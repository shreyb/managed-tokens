package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/notifications"
	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
)

const globalTimeoutDefaultStr string = "300s"

var (
	timeouts          = make(map[string]time.Duration)
	supportedTimeouts = map[string]struct{}{
		"globaltimeout":       {},
		"ferryrequesttimeout": {},
		"dbtimeout":           {},
	}
	adminNotifications = make([]notifications.SendMessager, 0)
	notificationsChan  notifications.EmailManager
)

func init() {
	const configFile string = "managedTokens"

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.Fatal("Current user is root.  Please run this executable as a non-root user")
	}

	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Flags
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.BoolP("test", "t", false, "Test mode.  Query FERRY, but do not make any database changes")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")
	pflag.String("authmethod", "tls", "Choose method for authentication to FERRY.  Currently-supported choices are \"tls\" and \"jwt\"")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	// Get config file
	// Check for override
	log.Info(viper.GetString("configfile"))
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
		log.Panicf("Fatal error reading in config file: %w", err)
	}

	// Set up logs
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

	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		log.Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func init() {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		if _, ok := supportedTimeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				log.WithField("timeoutKey", timeoutKey).Warn("Configured timeout not supported by this utility")
			}
			log.WithField(timeoutKey, timeoutString).Info("Configured timeout") // TODO Make a debug
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
		log.Fatal("Configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts: ", timeouts)
	}
}

func main() {
	var dbLocation string
	var newDB bool
	var db *sql.DB
	service := "refresh-uids-from-ferry"

	// Global Context
	var globalTimeout time.Duration
	var ok bool
	var err error

	if globalTimeout, ok = timeouts["globaltimeout"]; !ok {
		log.Debugf("Global timeout not configured in config file.  Using default global timeout of %s", globalTimeoutDefaultStr)
		if globalTimeout, err = time.ParseDuration(globalTimeoutDefaultStr); err != nil {
			log.Fatal("Could not parse default global timeout.")
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
	notificationsChan = notifications.NewAdminEmailManager(ctx, email)
	defer func() {
		close(notificationsChan)
		if err := notifications.SendAdminNotifications(
			ctx,
			"refresh-uids-from-ferry",
			viper.GetString("templates.adminerrors"),
			viper.GetBool("test"),
			adminNotifications...,
		); err != nil {
			log.Error("Error sending admin notifications")
		}
	}()

	// Open connection to the SQLite database where UID info will be stored
	// Look for DB file.  If not there, create it and DB.  If there, don't make it, just update it
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}
	log.Debugf("Using db file at %s", dbLocation)

	if _, err := os.Stat(dbLocation); errors.Is(err, os.ErrNotExist) {
		newDB = true
	}

	db, err = sql.Open("sqlite3", dbLocation)
	if err != nil {
		msg := "Could not open the UID database file"
		log.Errorf("%s: %s", msg, err)
		notificationsChan <- notifications.NewSetupError(msg, service)
		return
	}
	defer db.Close()

	if newDB {
		if err = utils.CreateUidsTableInDB(ctx, db); err != nil {
			msg := "Could not open the UID database file"
			notificationsChan <- notifications.NewSetupError(msg, service)
			return
		}
	}

	// FERRY vars
	ferryData := make([]utils.FerryUIDDatum, 0)
	ferryDataChan := make(chan utils.FerryUIDDatum) // Channel to send FERRY data from GetFERRYData worker to AggregateFERRYData worker
	ferryDataWg := new(sync.WaitGroup)              // WaitGroup to make sure we don't close ferryDataChan before all data is sent
	aggFERRYDataDone := make(chan struct{})         // Channel to close when FERRY data aggregation is done

	usernames := getAllAccountsFromConfig()

	// Start up worker to aggregate all FERRY data
	go func(ferryDataChan <-chan utils.FerryUIDDatum, aggFERRYDataDone chan<- struct{}) {
		defer close(aggFERRYDataDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggFERRYDataDone)

	// Start workers to get data from FERRY
	func() {
		defer close(ferryDataChan)

		// Pick our authentication method
		var authFunc func() func(context.Context, string, string) (*http.Response, error)
		switch viper.GetString("authmethod") {
		case "tls":
			authFunc = withTLSAuth
			log.Debug("Using TLS to authenticate to FERRY")
		case "jwt":
			sc, err := newFERRYServiceConfigWithKerberosAuth(ctx)
			if err != nil {
				msg := "Could not create service config to authenticate to FERRY with a JWT. Exiting"
				log.Error(msg)
				notificationsChan <- notifications.NewSetupError(msg, service)
				os.Exit(1)
			}
			defer func() {
				os.RemoveAll(sc.Krb5ccname)
				log.Info("Cleared kerberos cache")
			}()
			authFunc = withKerberosJWTAuth(sc)
			log.Debug("Using JWT to authenticate to FERRY")
		}

		// For each username, query FERRY for UID info
		for _, username := range usernames {
			ferryDataWg.Add(1)

			go func(username string, ferryDataChan chan<- utils.FerryUIDDatum) {
				defer ferryDataWg.Done()
				var ferryRequestContext context.Context
				if timeout, ok := timeouts["ferryrequesttimeout"]; ok {
					ferryRequestContext = utils.ContextWithOverrideTimeout(ctx, timeout)
				} else {
					ferryRequestContext = ctx
				}
				entry, err := worker.GetFERRYUIDData(
					ferryRequestContext,
					username,
					viper.GetString("ferry.host"),
					viper.GetInt("ferry.port"),
					authFunc(),
					ferryDataChan,
				)
				if err != nil {
					msg := "Could not get FERRY UID data"
					log.WithField("username", username).Error(msg)
					notificationsChan <- notifications.NewSetupError(msg+" for user "+username, service)
				} else {
					ferryDataChan <- entry
				}
			}(username, ferryDataChan)
		}
		ferryDataWg.Wait() // Don't close data channel until all workers have put their data in
	}()

	<-aggFERRYDataDone // Wait until FERRY data aggregation is done before we insert anything into DB

	if len(ferryData) == 0 {
		msg := "No data collected from FERRY"
		notificationsChan <- notifications.NewSetupError(msg, service)
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
		os.Exit(0)
	}

	var dbContext context.Context
	if timeout, ok := timeouts["dbtimeout"]; ok {
		dbContext = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		dbContext = ctx
	}
	if err := utils.InsertUidsIntoTableFromFERRY(dbContext, db, ferryData); err != nil {
		msg := "Could not insert FERRY data into database"
		notificationsChan <- notifications.NewSetupError(msg, service)
		log.Error(msg)
		return
	}

	// Confirm and verify that INSERT was successful
	dbData, err := utils.ConfirmUIDsInTable(ctx, db)
	if err != nil {
		msg := "Error running verification of INSERT"
		notificationsChan <- notifications.NewSetupError(msg, service)
		log.Error(msg)
		return
	}

	if !checkFerryDataInDB(ferryData, dbData) {
		notificationsChan <- notifications.NewSetupError(
			"Verification of INSERT failed.  Please check the logs",
			service,
		)
		return
	}
	log.Debug("Verified INSERT")
	log.Info("Successfully refreshed uid DB.")
}
