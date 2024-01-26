// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
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
	"github.com/yukitsune/lokirus"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/utils"
)

var (
	currentExecutable string
	buildTimestamp    string // Should be injected at build time with something like go build -ldflags="-X main.buildTimeStamp=$BUILDTIMESTAMP"
	version           string // Should be injected at build time with something like go build -ldflags="-X main.version=$VERSION"
	exeLogger         *log.Entry
)

var devEnvironmentLabel string

const devEnvironmentLabelDefault string = "production"

// Supported Timeouts and their defaults
var timeouts = map[string]time.Duration{
	"global":       time.Duration(300 * time.Second),
	"ferryrequest": time.Duration(30 * time.Second),
	"db":           time.Duration(10 * time.Second),
}

// Metrics
var (
	promDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "stage_duration_seconds",
		Help:      "The amount of time it took to run a stage of a Managed Tokens Service executable",
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
	startSetup   time.Time
	prometheusUp = true
)

func init() {
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

	initFlags() // Parse our flags
	if viper.GetBool("version") {
		fmt.Printf("Managed tokens library version %s, build %s\n", version, buildTimestamp)
		os.Exit(0)
	}

	if err := initConfig(); err != nil {
		fmt.Println("Fatal error setting up configuration.  Exiting now")
		os.Exit(1)
	}

	devEnvironmentLabel = getDevEnvironmentLabel()

	initLogs()
	if err := initTimeouts(); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Fatal error setting up timeouts")
	}
	if err := initMetrics(); err != nil {
		log.WithField("executable", currentExecutable).Error("Error setting up metrics")
	}

}

func initFlags() {
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
}

func initConfig() error {
	// Get config file
	configFileName := "managedTokens"
	// Check for override
	if config := viper.GetString("configfile"); config != "" {
		viper.SetConfigFile(config)
	} else {
		viper.SetConfigName(configFileName)
	}

	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.WithField("executable", currentExecutable).Errorf("Error reading in config file: %v", err)
		return err
	}

	return nil
}

// Set up logs
func initLogs() {
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

	// Loki.  Example here taken from README: https://github.com/YuKitsune/lokirus/blob/main/README.md
	lokiOpts := lokirus.NewLokiHookOptions().
		// Grafana doesn't have a "panic" level, but it does have a "critical" level
		// https://grafana.com/docs/grafana/latest/explore/logs-integration/
		WithLevelMap(lokirus.LevelMap{log.PanicLevel: "critical"}).
		WithFormatter(&log.JSONFormatter{}).
		WithStaticLabels(lokirus.Labels{
			"app":         "managed-tokens",
			"command":     currentExecutable,
			"environment": devEnvironmentLabel,
		})
	lokiHook := lokirus.NewLokiHookWithOpts(
		viper.GetString("loki.host"),
		lokiOpts,
		log.InfoLevel,
		log.WarnLevel,
		log.ErrorLevel,
		log.FatalLevel)

	log.AddHook(lokiHook)

	exeLogger = log.WithField("executable", currentExecutable)
	exeLogger.Debugf("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		exeLogger.Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func initTimeouts() error {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		timeoutKey := strings.TrimSuffix(timeoutKey, "timeout")
		// Only save the timeout if it's supported, otherwise ignore it
		if _, ok := timeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				exeLogger.WithField(timeoutKey, timeoutString).Warn("Could not parse configured timeout duration.  Using default")
				continue
			}
			timeouts[timeoutKey] = timeout
			exeLogger.WithField(timeoutKey, timeoutString).Debug("Configured timeout")
		}
	}

	// Verify that individual timeouts don't add to more than total timeout
	now := time.Now()
	timeForComponentCheck := now

	for timeoutKey, timeout := range timeouts {
		if timeoutKey != "global" {
			timeForComponentCheck = timeForComponentCheck.Add(timeout)
		}
	}

	timeForGlobalCheck := now.Add(timeouts["global"])
	if timeForComponentCheck.After(timeForGlobalCheck) {
		msg := "configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts"
		exeLogger.Error(msg)
		return errors.New(msg)
	}
	return nil
}

// Set up prometheus metrics
func initMetrics() error {
	// Set up prometheus metrics
	if _, err := http.Get(viper.GetString("prometheus.host")); err != nil {
		exeLogger.Errorf("Error contacting prometheus pushgateway %s: %s.  The rest of prometheus operations will fail. "+
			"To limit error noise, "+
			"these failures at the experiment level will be registered as warnings in the log, "+
			"and not be sent in any notifications.", viper.GetString("prometheus.host"), err.Error())
		prometheusUp = false
		return err
	}

	metrics.MetricsRegistry.MustRegister(promDuration)
	metrics.MetricsRegistry.MustRegister(ferryRefreshTime)
	return nil
}

func main() {
	// Global Context
	var globalTimeout time.Duration
	var ok bool

	if globalTimeout, ok = timeouts["global"]; !ok {
		exeLogger.Fatal("Could not obtain global timeout.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Run our actual operation
	if err := run(ctx); err != nil {
		exeLogger.Fatal("Error running operations to update database from FERRY.  Exiting")
	}
	log.Debug("Finished run")
}

func run(ctx context.Context) error {
	// Order of operations:
	// 1. Open database to record FERRY data
	// 2. Set up admin notification emails
	// 3. a. Choose authentication method to FERRY
	// 4. b. Query FERRY for data
	// 5. Insert data into database
	// 6. Verify that INSERTed data matches response data from FERRY
	// 7. Push metrics and send necessary notifications

	// We need to pass the pointer in here since we need to pick up the
	// changes made to adminNotifications, as explained here:
	// https://stackoverflow.com/a/52070387
	// and mocked out here:
	// https://go.dev/play/p/rww0ORt94pU

	var dbLocation string
	// Open connection to the SQLite database where UID info will be stored
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}
	exeLogger.Debugf("Using db file at %s", dbLocation)

	database, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		msg := "Could not open or create ManagedTokensDatabase"
		exeLogger.Error(msg)
		// Start up a notification manager JUST for the purpose of sending the email that we couldn't open the DB.
		// In the case of this executable, that's a fatal error and we should stop execution.
		admNotMgr, adminNotifications := setupAdminNotifications(ctx, nil)
		admNotMgr.ReceiveChan <- notifications.NewSetupError(msg, currentExecutable)

		if err2 := sendAdminNotifications(ctx, admNotMgr.ReceiveChan, &adminNotifications); err2 != nil {
			exeLogger.Error("Error sending admin notifications")
			err := fmt.Errorf("error sending admin notifications regarding %w: %w", err, err2)
			return err
		}
		return fmt.Errorf("%s: %w", msg, err)
	}
	defer database.Close()

	// Send admin notifications at end of run
	admNotMgr, adminNotifications := setupAdminNotifications(ctx, database)

	// Send metrics anytime run() returns
	defer func() {
		if prometheusUp {
			if err := metrics.PushToPrometheus(viper.GetString("prometheus.host"), getPrometheusJobName()); err != nil {
				// Non-essential - don't halt execution here
				exeLogger.Error("Could not push metrics to prometheus pushgateway")
			} else {
				exeLogger.Info("Finished pushing metrics to prometheus pushgateway")
			}
		}
		// We don't check the error here, because we don't want to halt execution if the admin message can't be sent.  Just log it and move on
		sendAdminNotifications(ctx, admNotMgr.ReceiveChan, &adminNotifications)
	}()

	// Setup complete
	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "setup").Set(time.Since(startSetup).Seconds())
	}

	// Begin processing
	startRequest := time.Now()

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = utils.ContextWithVerbose(ctx)
	}
	// Start up worker to aggregate all FERRY data
	ferryData := make([]db.FerryUIDDatum, 0)
	ferryDataChan := make(chan db.FerryUIDDatum) // Channel to send FERRY data from GetFERRYData worker to AggregateFERRYData worker
	aggFERRYDataDone := make(chan struct{})      // Channel to close when FERRY data aggregation is done
	go func(ferryDataChan <-chan db.FerryUIDDatum, aggFERRYDataDone chan<- struct{}) {
		defer close(aggFERRYDataDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggFERRYDataDone)

	usernames := getAllAccountsFromConfig()

	// Pick our authentication method
	var authFunc func() func(context.Context, string, string) (*http.Response, error)
	switch supportedFERRYAuthMethod(viper.GetString("authmethod")) {
	case tlsAuth:
		authFunc = withTLSAuth
		exeLogger.Debug("Using TLS to authenticate to FERRY")
	case jwtAuth:
		sc, err := newFERRYServiceConfigWithKerberosAuth(ctx)
		if err != nil {
			msg := "Could not create service config to authenticate to FERRY with a JWT. Exiting"
			admNotMgr.ReceiveChan <- notifications.NewSetupError(msg, currentExecutable)
			exeLogger.Error(msg)
			os.Exit(1)
		}
		defer func() {
			prefix := environment.FILE.String()
			os.RemoveAll(strings.TrimPrefix(sc.GetValue(environment.Krb5ccname), prefix))
			exeLogger.Info("Cleared kerberos cache")
		}()
		authFunc = withKerberosJWTAuth(sc)
		exeLogger.Debug("Using JWT to authenticate to FERRY")
	default:
		return errors.New("unsupported authentication method to communicate with FERRY")
	}

	// Start workers to get data from FERRY
	func() {
		var ferryDataWg sync.WaitGroup // WaitGroup to make sure we don't close ferryDataChan before all data is sent
		defer close(ferryDataChan)

		var ferryContext context.Context
		if timeout, ok := timeouts["ferryrequest"]; ok {
			ferryContext = utils.ContextWithOverrideTimeout(ctx, timeout)
		} else {
			ferryContext = ctx
		}
		// For each username, query FERRY for UID info
		for _, username := range usernames {
			ferryDataWg.Add(1)

			go func(username string) {
				defer ferryDataWg.Done()
				getAndAggregateFERRYData(ferryContext, username, authFunc, ferryDataChan, admNotMgr.ReceiveChan)
			}(username)
		}
		ferryDataWg.Wait() // Don't close data channel until all workers have put their data in
	}()

	<-aggFERRYDataDone // Wait until FERRY data aggregation is done before we insert anything into DB
	promDuration.WithLabelValues(currentExecutable, "getFERRYData").Set(time.Since(startRequest).Seconds())

	// If we got no data, that's a bad thing, since we always expect to be able to
	if len(ferryData) == 0 {
		msg := "no data collected from FERRY"
		admNotMgr.ReceiveChan <- notifications.NewSetupError(msg, currentExecutable)
		exeLogger.Error(msg + ". Exiting")
		return errors.New(msg)
	}

	// Stop here if we're in test mode
	if viper.GetBool("test") {
		exeLogger.Info("Finished gathering data from FERRY")

		ferryDataStringSlice := make([]string, 0, len(ferryData))
		for _, datum := range ferryData {
			ferryDataStringSlice = append(ferryDataStringSlice, datum.String())
		}
		exeLogger.Infof(strings.Join(ferryDataStringSlice, "; "))

		exeLogger.Info("Test mode finished")
		return nil
	}

	// INSERT all collected FERRY data into FERRYUIDDatabase
	startDBInsert := time.Now()
	var dbContext context.Context
	if timeout, ok := timeouts["db"]; ok {
		dbContext = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		dbContext = ctx
	}
	if err := database.InsertUidsIntoTableFromFERRY(dbContext, ferryData); err != nil {
		msg := "Could not insert FERRY data into database"
		admNotMgr.ReceiveChan <- notifications.NewSetupError(msg, currentExecutable)
		exeLogger.Error(msg)
		return err
	}

	// Confirm and verify that INSERT was successful
	dbData, err := database.ConfirmUIDsInTable(ctx)
	if err != nil {
		msg := "Error running verification of INSERT"
		admNotMgr.ReceiveChan <- notifications.NewSetupError(msg, currentExecutable)
		exeLogger.Error(msg)
		return err
	}

	if !checkFerryDataInDB(ferryData, dbData) {
		msg := "verification of INSERT failed.  Please check the logs"
		exeLogger.Error(msg)
		admNotMgr.ReceiveChan <- notifications.NewSetupError(
			"Verification of INSERT failed.  Please check the logs",
			currentExecutable,
		)
		return errors.New(msg)
	}
	exeLogger.Debug("Verified INSERT")
	exeLogger.Info("Successfully refreshed Managed Tokens DB.")
	promDuration.WithLabelValues(currentExecutable, "refreshManagedTokensDB").Set(time.Since(startDBInsert).Seconds())
	ferryRefreshTime.SetToCurrentTime()
	return nil
}
