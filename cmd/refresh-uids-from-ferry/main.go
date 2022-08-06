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

	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
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

	// TODO Take care of overrides:  keytabPath, desiredUid, condorCreddHost, condorCollectorHost, userPrincipalOverride

	if viper.GetBool("test") {
		log.Info("Running in test mode")
	}
	// TODO Flags to override config

	// TODO Logfile setup

}

func main() {
	var dbLocation string
	var newDB bool
	var db *sql.DB
	var globalTimeout time.Duration
	globalTimeout, err := time.ParseDuration(viper.GetString("timeouts.globalTimeout"))
	if err != nil {
		log.Error("Could not parse global timeout.  Using default value of 300s")
		globalTimeout = time.Duration(300 * time.Second)
	}

	// Global context
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Open connection to the SQLite database where UID info will be stored
	// Look for DB file.  If not there, create it and DB.  If there, don't make it, just update it
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}

	if _, err := os.Stat(dbLocation); errors.Is(err, os.ErrNotExist) {
		newDB = true
	}

	db, err = sql.Open("sqlite3", dbLocation)
	if err != nil {
		log.Error("Could not open the UID database file")
		log.Fatal(err)
	}
	defer db.Close()

	if newDB {
		if err = utils.CreateUidsTableInDB(db); err != nil {
			log.Fatal("Error creating UID table in database")
		}
	}

	// FERRY vars
	ferryData := make([]*worker.UIDEntryFromFerry, 0)
	ferryDataChan := make(chan *worker.UIDEntryFromFerry) // Channel to send FERRY data from GetFERRYData worker to AggregateFERRYData worker
	ferryDataWg := new(sync.WaitGroup)                    // WaitGroup to make sure we don't close ferryDataChan before all data is sent
	aggFERRYDataDone := make(chan struct{})               // Channel to close when FERRY data aggregation is done

	usernames := getAllAccountsFromConfig()

	// Start up worker to aggregate all FERRY data
	go func(ferryDataChan <-chan *worker.UIDEntryFromFerry, aggFERRYDataDone chan<- struct{}) {
		defer close(aggFERRYDataDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggFERRYDataDone)

	// Start workers to get data from FERRY
	func() {
		defer close(ferryDataChan)

		// Pick our authentication method
		var authFunc func() func(string, string) (*http.Response, error)
		switch viper.GetString("authmethod") {
		case "tls":
			authFunc = withTLSAuth
			log.Debug("Using TLS to authenticate to FERRY")
		case "jwt":
			sc, err := newFERRYServiceConfigWithKerberosAuth(ctx)
			if err != nil {
				log.Error("Could not create service config to authenticate to FERRY with a JWT. Exiting")
				os.Exit(1)
			}
			defer func() {
				os.RemoveAll(sc.Krb5ccname)
				log.Info("Cleared kerberos cache")
			}()
			authFunc = withKerberosJWTAuth(ctx, sc)
			log.Debug("Using JWT to authenticate to FERRY")
		}

		for _, username := range usernames {
			ferryDataWg.Add(1)
			func(username string, ferryDataChan chan<- *worker.UIDEntryFromFerry) {
				// go func(username string, ferryDataChan chan<- *worker.UIDEntryFromFerry) {
				defer ferryDataWg.Done()
				entry, err := worker.GetFERRYUIDData(
					username,
					viper.GetString("ferry.host"),
					viper.GetInt("ferry.port"),
					ferryDataChan,
					authFunc(),
				)
				if err != nil {
					log.WithField("username", username).Error("Could not get FERRY UID data")
				} else {
					ferryDataChan <- entry
				}
			}(username, ferryDataChan)
		}
		ferryDataWg.Wait() // Don't close data channel until all workers have put their data in
	}()
	// Wait until FERRY data aggregation is done before we insert anything into DB
	<-aggFERRYDataDone

	if len(ferryData) == 0 {
		log.Fatal("No data collected from FERRY.  Exiting")
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

	// Convert type of ferryData to []FerryDatum
	ferryDataConverted := make([]utils.FerryUIDDatum, 0, len(ferryData))
	for _, entry := range ferryData {
		ferryDataConverted = append(ferryDataConverted, entry)
	}

	if err := utils.InsertUidsIntoTableFromFERRY(db, ferryDataConverted); err != nil {
		log.Fatal("Could not insert FERRY data into database")
	}

	// Confirm and verify that INSERT was successful
	count, err := utils.ConfirmUIDsInTable(db)
	if err != nil {
		log.Fatal("Error running verification of INSERT")
	}
	if count != len(ferryData) {
		log.Fatalf("Verification of INSERT failed.  Expected %d total UID rows, got %d", len(ferryData), count)
	}
	//TODO Make this a debug
	log.Info("Verified INSERT. Done")
}
