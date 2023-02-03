package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rifflock/lfshook"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
)

var (
	currentExecutable string
	buildTimestamp    string
	version           string
)

// Metrics checkpoints
var (
	startSetup      time.Time
	startProcessing time.Time
	prometheusUp    = true
)

// Metrics
var (
	promDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "stage_duration_seconds",
		Help:      "The amount of time it took to run a stage (setup|processing|cleanup) of a Managed Tokens Service executable",
	},
		[]string{
			"executable",
			"stage",
		},
	)
)

// Notifications
var (
	adminNotifications = make([]notifications.SendMessager, 0)
)

// Initial setup.  Read flags, find config file
func init() {
	const configFile string = "managedTokens"
	startSetup = time.Now()

	// Get current executable name
	if exePath, err := os.Executable(); err != nil {
		log.Error("Could not get path of current executable")
	} else {
		currentExecutable = path.Base(exePath)
	}

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Current user is root.  Please run this executable as a non-root user")
	}

	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Flags
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.BoolP("test", "t", false, "Test mode.  Check certs, but do not send emails")
	pflag.Bool("version", false, "Version of Managed Tokens library")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	if viper.GetBool("version") {
		fmt.Printf("Managed tokens library version %s, build %s\n", version, buildTimestamp)
		os.Exit(0)
	}

	// Get config file
	// Check for override
	if config := viper.GetString("configfile"); config != "" {
		viper.SetConfigFile(config)
	} else {
		viper.SetConfigName(configFile)
	}

	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.WithField("executable", currentExecutable).Panicf("Fatal error reading in config file: %v", err)
	}
}

// Set up logs
func init() {
	log.SetLevel(log.DebugLevel)
	logConfigLookup := "logs.check-service-cert.logfile"

	// Info log file
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.InfoLevel:  viper.GetString(logConfigLookup),
		log.WarnLevel:  viper.GetString(logConfigLookup),
		log.ErrorLevel: viper.GetString(logConfigLookup),
		log.FatalLevel: viper.GetString(logConfigLookup),
		log.PanicLevel: viper.GetString(logConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	log.WithField("executable", currentExecutable).Infof("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		log.WithField("executable", currentExecutable).Info("Running in test mode")
	}
}

// Set up prometheus metrics
func init() {
	// Set up prometheus metrics
	if _, err := http.Get(viper.GetString("prometheus.host")); err != nil {
		log.WithField("executable", currentExecutable).Errorf("Error contacting prometheus pushgateway %s: %s.  The rest of prometheus operations will fail. "+
			"To limit error noise, "+
			"these failures at the experiment level will be registered as warnings in the log, "+
			"and not be sent in any notifications.", viper.GetString("prometheus.host"), err.Error())
		prometheusUp = false
	}
	metrics.MetricsRegistry.MustRegister(promDuration)
}

// Order of operations
// 0.  Setup (global context, set up admin notification emails)
// 1.  Ingest service cert
// 2.  Check if time left is less than configured time
// 3.  If so, send notification.  If not, log and exit
// 4. Push metrics and send necessary notifications
func main() {
	//0.
	// Global Context
	var globalTimeout time.Duration
	globalTimeoutDefault, _ := time.ParseDuration("2m")
	var err error

	globalTimeout, err = time.ParseDuration(viper.GetString("timeouts.globaltimeout"))
	if err != nil {
		log.WithField("executable", currentExecutable).Errorf("Could not parse global timeout.  Using default of %v", globalTimeoutDefault)
		globalTimeout = globalTimeoutDefault
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Send admin notifications at end of run
	var prefix string
	if viper.GetBool("test") {
		prefix = "notifications_test."
	} else {
		prefix = "notifications."
	}

	now := time.Now()
	nowString := now.Format(time.RFC822)
	email := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice(prefix+"admin_email"),
		"Managed Tokens Certificates Check "+nowString,
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
		"",
	)
	slackMessage := notifications.NewSlackMessage(
		viper.GetString(prefix + "slack_alerts_url"),
	)
	adminNotifications = append(adminNotifications, email, slackMessage)

	// Setup complete
	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "setup").Set(time.Since(startSetup).Seconds())
	}

	// Begin processing
	startProcessing = time.Now()
	defer func() {
		if prometheusUp {
			promDuration.WithLabelValues(currentExecutable, "processing").Set(time.Since(startProcessing).Seconds())
			if err := metrics.PushToPrometheus(); err != nil {
				log.WithField("executable", currentExecutable).Error("Could not push metrics to prometheus pushgateway")
			} else {
				log.WithField("executable", currentExecutable).Info("Finished pushing metrics to prometheus pushgateway")
			}
		}
	}()

	// 1.  Ingest service certificate
	// Borrowed this heavily from own code at
	// https://cdcvs.fnal.gov/redmine/projects/discompsupp/repository/ken_proxy_push/revisions/master/entry/proxy/serviceCert.go

	serviceCertLookupPath := "ferry.hostcert"
	serviceCertPath := viper.GetString(serviceCertLookupPath)

	certFile, err := os.Open(serviceCertPath)
	if err != nil {
		if os.IsNotExist(err) {
			errText := "certPath does not exist"
			log.WithFields(
				log.Fields{
					"executable": currentExecutable,
					"certPath":   serviceCertPath,
				}).Fatal(errText)
		}
		errText := "Could not open service certificate file"
		log.WithFields(
			log.Fields{
				"executable": currentExecutable,
				"certPath":   serviceCertPath,
			}).Fatal(errText)
	}

	defer certFile.Close()

	certContent, err := io.ReadAll(certFile)
	if err != nil {
		err := fmt.Sprintf("Could not read cert file: %s", err.Error())
		log.WithFields(
			log.Fields{
				"executable": currentExecutable,
				"certPath":   serviceCertPath,
			}).Fatal(err)
	}

	certDER, _ := pem.Decode(certContent)
	if certDER == nil {
		log.WithFields(
			log.Fields{
				"executable": currentExecutable,
				"certPath":   serviceCertPath,
			}).Fatal("Could not decode PEM block containing cert data")
	}

	cert, err := x509.ParseCertificate(certDER.Bytes)
	if err != nil {
		err := fmt.Sprintf("Could not parse certificate from DER data: %s", err.Error())
		log.WithFields(
			log.Fields{
				"executable": currentExecutable,
				"certPath":   serviceCertPath,
			}).Fatal(err)
	}

	log.WithField("certPath", serviceCertPath).Debug("Read in and decoded cert file, getting expiration")

	expiration := cert.NotAfter
	log.WithFields(log.Fields{
		"certPath":   serviceCertPath,
		"expiration": expiration,
	}).Debug("Successfully ingested service certificate")

	// 2.  Check if time left is less than configured time
	defaultHostCertLifetime := "720h"
	expirationAlertWindow, err := time.ParseDuration(viper.GetString("minHostCertLifetime"))
	if err != nil {
		log.WithField("executable", currentExecutable).Error(err)
		log.WithField("executable", currentExecutable).Errorf("Could not parse minimum host certificate lifetime.  Using default of %s", defaultHostCertLifetime)
		expirationAlertWindow, _ = time.ParseDuration(defaultHostCertLifetime)
	}

	if now.Add(expirationAlertWindow).After(expiration) {
		log.WithFields(log.Fields{
			"executable":             currentExecutable,
			"now":                    now,
			"expiration":             expiration,
			"serviceCertificatePath": serviceCertPath,
		}).Warnf("Service certificate will expire within %s", viper.GetString("minHostCertLifetime"))
	} else {
		log.WithFields(log.Fields{
			"executable":             currentExecutable,
			"now":                    now,
			"expiration":             expiration,
			"serviceCertificatePath": serviceCertPath,
		}).Infof("Service certificate will not expire within %s. Exiting now", viper.GetString("minHostCertLifetime"))
		return
	}

	// 3.  If so, send notification.
	templateData, err := os.ReadFile(viper.GetString("templates.expiringCertificate"))
	if err != nil {
		log.WithFields(log.Fields{
			"executable":   currentExecutable,
			"templatePath": viper.GetString("templates.expiringCertificate"),
		}).Fatalf("Could not read expiring certificate template file: %s", err)
	}

	var b strings.Builder
	numDays := int(math.Floor(expiration.Sub(now).Hours() / 24.0))
	tmpl := template.Must(template.New("expiringCert").Parse(string(templateData)))
	messageArgs := struct{ NumDays int }{NumDays: numDays}

	if err = tmpl.Execute(&b, messageArgs); err != nil {
		log.WithField("executable", currentExecutable).Fatalf("Failed to execute expiring certificate template: %s", err)
	}

	for _, n := range adminNotifications {
		if err := notifications.SendMessage(ctx, n, b.String()); err != nil {
			log.WithFields(log.Fields{
				"executable":       currentExecutable,
				"notificationType": fmt.Sprintf("%T", n),
			}).Errorf("Error sending notification: %s", err)
		}
	}
}
