package main

import (
	"database/sql"
	"errors"
	"os"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
)

func init() {
	// Get config file
	viper.SetConfigName("managedTokens")
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

	// TODO Flags to override config

	// TODO Logfile setup

}

func main() {
	var dbLocation string
	var newDB bool

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

	if err = utils.InsertUidsIntoTableFromFERRY(db, ferryData); err != nil {
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
