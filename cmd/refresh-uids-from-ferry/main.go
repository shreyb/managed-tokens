package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
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

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	// Get config file
	// Check for override
	if viper.GetString("configfile") != "" {
		viper.SetConfigFile(viper.GetString("configfile"))
	} else {
		viper.SetConfigName(configFile)
	}

	viper.SetConfigName(configFile)
	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Panicf("Fatal error reading in config file: %w", err)
	}
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

	if !viper.GetBool("test") {
		db, err := sql.Open("sqlite3", dbLocation)
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
	}

	// FERRY vars
	ferryData := make([]*utils.UIDEntryFromFerry, 0)
	ferryClient := utils.InitializeHTTPSClient(viper.GetString("hostCert"), viper.GetString("hostKey"), viper.GetString("caPath"))
	ferryDataChan := make(chan *utils.UIDEntryFromFerry) // Channel to send FERRY data from GetFERRYData worker to AggregateFERRYData worker
	ferryDataWg := new(sync.WaitGroup)                   // WaitGroup to make sure we don't close ferryDataChan before all data is sent
	aggFERRYDataDone := make(chan struct{})              // Channel to close when FERRY data aggregation is done

	usernames := getAllAccountsFromConfig()

	// Start up worker to aggregate all FERRY data
	go func(ferryDataChan <-chan *utils.UIDEntryFromFerry, aggFERRYDataDone chan<- struct{}) {
		defer close(aggFERRYDataDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggFERRYDataDone)

	// Start workers to get data from FERRY
	func() {
		defer close(ferryDataChan)
		for _, username := range usernames {
			ferryDataWg.Add(1)
			go func(username string, ferryDataChan chan<- *utils.UIDEntryFromFerry) {
				defer ferryDataWg.Done()
				worker.GetFERRYUIDData(ferryClient, username, ferryDataChan)
			}(username, ferryDataChan)
		}
		ferryDataWg.Wait() // Don't close data channel until all workers have put their data in
	}()
	// Wait until FERRY data aggregation is done before we insert anything into DB
	<-aggFERRYDataDone

	if viper.GetBool("test") {
		log.Info("Finished gathering data from FERRY")

		ferryDataStringSlice := make([]string, 0, len(ferryData))
		for _, datum := range ferryData {
			ferryDataStringSlice = append(ferryDataStringSlice, fmt.Sprintf("%s", datum))
		}
		log.Infof(strings.Join(ferryDataStringSlice, "; "))

		log.Info("Test mode finished")
		os.Exit(0)
	}

	if err := utils.InsertUidsIntoTableFromFERRY(db, ferryData); err != nil {
		log.Fatal("Could not insert FERRY data into database")
	}

	// Confirm and verify that INSERT was successful
	count, err := utils.ConfirmUIDsInTable(db)
	if err != nil {
		log.Fatal("Error running verification of INSERT")
	}
	if count != len(ferryData) {
		log.Fatal("Verification of INSERT failed.  Expected %d total UID rows, got %d", len(ferryData), count)
	}
	//TODO Make this a debug
	log.Info("Verified INSERT. Done")
}
