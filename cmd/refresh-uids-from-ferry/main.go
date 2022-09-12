package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/utils"
)

var (
	currentExecutable string
	buildTimestamp    string
	version           string
)

const globalTimeoutDefaultStr string = "300s"

var (
	timeouts          = make(map[string]time.Duration)
	supportedTimeouts = map[string]struct{}{
		"globaltimeout":       {},
		"ferryrequesttimeout": {},
		"dbtimeout":           {},
	}
)

var (
	adminNotifications = make([]notifications.SendMessager, 0)
	notificationsChan  notifications.EmailManager
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
	ferryRefreshTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "last_ferry_refresh",
			Help:      "The timestamp of the last successful refresh of the username --> UID table from FERRY for the Managed Tokens Service",
		},
	)
)

var (
	startSetup      time.Time
	startProcessing time.Time
	prometheusUp    = true
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
	pflag.BoolP("test", "t", false, "Test mode.  Query FERRY, but do not make any database changes")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")
	pflag.String("authmethod", "tls", "Choose method for authentication to FERRY.  Currently-supported choices are \"tls\" and \"jwt\"")
	pflag.BoolP("verbose", "v", false, "Turn on verbose mode")

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
	debugLogConfigLookup := "logs.refresh-uids-from-ferry.debugfile"
	logConfigLookup := "logs.refresh-uids-from-ferry.logfile"
	// Debug log
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.DebugLevel: viper.GetString(debugLogConfigLookup),
		log.InfoLevel:  viper.GetString(debugLogConfigLookup),
		log.WarnLevel:  viper.GetString(debugLogConfigLookup),
		log.ErrorLevel: viper.GetString(debugLogConfigLookup),
		log.FatalLevel: viper.GetString(debugLogConfigLookup),
		log.PanicLevel: viper.GetString(debugLogConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	// Info log file
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.InfoLevel:  viper.GetString(logConfigLookup),
		log.WarnLevel:  viper.GetString(logConfigLookup),
		log.ErrorLevel: viper.GetString(logConfigLookup),
		log.FatalLevel: viper.GetString(logConfigLookup),
		log.PanicLevel: viper.GetString(logConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	log.WithField("executable", currentExecutable).Debugf("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		log.WithField("executable", currentExecutable).Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func init() {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		if _, ok := supportedTimeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				log.WithFields(log.Fields{
					"timeoutKey": timeoutKey,
					"executable": currentExecutable,
				}).Warn("Configured timeout not supported by this utility")
			}
			log.WithFields(log.Fields{
				"executable": currentExecutable,
				timeoutKey:   timeoutString,
			}).Debug("Configured timeout")
			timeouts[timeoutKey] = timeout
		}
	}

	// Verify that individual timeouts don't add to more than total timeout
	now := time.Now()
	timeForComponentCheck := now

	for timeoutKey, timeout := range timeouts {
		if timeoutKey != "globaltimeout" {
			timeForComponentCheck = timeForComponentCheck.Add(timeout)
		}
	}

	timeForGlobalCheck := now.Add(timeouts["globaltimeout"])
	if timeForComponentCheck.After(timeForGlobalCheck) {
		log.WithField("executable", currentExecutable).Fatal("Configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts: ", timeouts)
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
	metrics.MetricsRegistry.MustRegister(ferryRefreshTime)
}

// Order of operations:
// 0.  Setup (global context, set up admin notification emails)
// 1. Open database to record FERRY data
// 2. a. Choose authentication method to FERRY
// 2. b. Query FERRY for data
// 3. Insert data into database
// 4. Verify that INSERTed data matches response data from FERRY
// 5. Push metrics and send necessary notifications
func main() {
	var dbLocation string

	// Global Context
	var globalTimeout time.Duration
	var ok bool
	var err error

	if globalTimeout, ok = timeouts["globaltimeout"]; !ok {
		log.WithField("executable", currentExecutable).Infof(
			"Global timeout not configured in config file.  Using default global timeout of %s",
			globalTimeoutDefaultStr,
		)
		if globalTimeout, err = time.ParseDuration(globalTimeoutDefaultStr); err != nil {
			log.WithField("executable", currentExecutable).Fatal("Could not parse default global timeout.")
		}
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

	now := time.Now().Format(time.RFC822)
	email := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice(prefix+"admin_email"),
		"Managed Tokens Errors "+now,
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
		"",
	)
	slackMessage := notifications.NewSlackMessage(
		viper.GetString(prefix + "slack_alerts_url"),
	)
	adminNotifications = append(adminNotifications, email, slackMessage)
	notificationsChan = notifications.NewAdminEmailManager(ctx, email) // Listen for messages from run
	defer func() {
		close(notificationsChan)
		if err := notifications.SendAdminNotifications(
			ctx,
			currentExecutable,
			viper.GetString("templates.adminerrors"),
			viper.GetBool("test"),
			adminNotifications...,
		); err != nil {
			log.WithField("executable", currentExecutable).Error("Error sending admin notifications")
		}
	}()

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

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = utils.ContextWithVerbose(ctx)
	}

	// Open connection to the SQLite database where UID info will be stored
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}
	log.WithField("executable", currentExecutable).Debugf("Using db file at %s", dbLocation)

	ferryUidDb, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		msg := "Could not open or create FERRYUIDDatabase"
		notificationsChan <- notifications.NewSetupError(msg, currentExecutable)
		log.WithField("executable", currentExecutable).Fatal(msg)
	}
	defer ferryUidDb.Close()

	// Start up worker to aggregate all FERRY data
	ferryData := make([]db.FerryUIDDatum, 0)
	ferryDataChan := make(chan db.FerryUIDDatum) // Channel to send FERRY data from GetFERRYData worker to AggregateFERRYData worker

	aggFERRYDataDone := make(chan struct{}) // Channel to close when FERRY data aggregation is done
	go func(ferryDataChan <-chan db.FerryUIDDatum, aggFERRYDataDone chan<- struct{}) {
		defer close(aggFERRYDataDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggFERRYDataDone)

	usernames := getAllAccountsFromConfig()

	// Start workers to get data from FERRY
	ferryDataWg := new(sync.WaitGroup) // WaitGroup to make sure we don't close ferryDataChan before all data is sent
	func() {
		defer close(ferryDataChan)

		// Pick our authentication method
		var authFunc func() func(context.Context, string, string) (*http.Response, error)
		switch viper.GetString("authmethod") {
		case "tls":
			authFunc = withTLSAuth
			log.WithField("executable", currentExecutable).Debug("Using TLS to authenticate to FERRY")
		case "jwt":
			sc, err := newFERRYServiceConfigWithKerberosAuth(ctx)
			if err != nil {
				msg := "Could not create service config to authenticate to FERRY with a JWT. Exiting"
				notificationsChan <- notifications.NewSetupError(msg, currentExecutable)
				log.WithField("executable", currentExecutable).Error(msg)
				os.Exit(1)
			}
			defer func() {
				os.RemoveAll(sc.Krb5ccname)
				log.WithField("executable", currentExecutable).Info("Cleared kerberos cache")
			}()
			authFunc = withKerberosJWTAuth(sc)
			log.WithField("executable", currentExecutable).Debug("Using JWT to authenticate to FERRY")
		}

		// For each username, query FERRY for UID info
		for _, username := range usernames {
			ferryDataWg.Add(1)

			go func(username string) {
				defer ferryDataWg.Done()
				getAndAggregateFERRYData(ctx, username, authFunc, ferryDataChan)
			}(username)
		}
		ferryDataWg.Wait() // Don't close data channel until all workers have put their data in
	}()

	<-aggFERRYDataDone // Wait until FERRY data aggregation is done before we insert anything into DB

	if len(ferryData) == 0 {
		msg := "No data collected from FERRY"
		notificationsChan <- notifications.NewSetupError(msg, currentExecutable)
		log.Error(msg + ". Exiting")
		return
	}

	if viper.GetBool("test") {
		log.Info("Finished gathering data from FERRY")

		ferryDataStringSlice := make([]string, 0, len(ferryData))
		for _, datum := range ferryData {
			ferryDataStringSlice = append(ferryDataStringSlice, datum.String())
		}
		log.Infof(strings.Join(ferryDataStringSlice, "; "))

		log.Info("Test mode finished")
		return
	}

	// INSERT all collected FERRY data into FERRYUIDDatabase
	var dbContext context.Context
	if timeout, ok := timeouts["dbtimeout"]; ok {
		dbContext = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		dbContext = ctx
	}
	if err := ferryUidDb.InsertUidsIntoTableFromFERRY(dbContext, ferryData); err != nil {
		msg := "Could not insert FERRY data into database"
		notificationsChan <- notifications.NewSetupError(msg, currentExecutable)
		log.Error(msg)
		return
	}

	// Confirm and verify that INSERT was successful
	dbData, err := ferryUidDb.ConfirmUIDsInTable(ctx)
	if err != nil {
		msg := "Error running verification of INSERT"
		notificationsChan <- notifications.NewSetupError(msg, currentExecutable)
		log.Error(msg)
		return
	}

	if !checkFerryDataInDB(ferryData, dbData) {
		msg := "Verification of INSERT failed.  Please check the logs"
		log.Error(msg)
		notificationsChan <- notifications.NewSetupError(
			"Verification of INSERT failed.  Please check the logs",
			currentExecutable,
		)
		return
	}
	log.Debug("Verified INSERT")
	log.Info("Successfully refreshed uid DB.")
	ferryRefreshTime.SetToCurrentTime()
}
