package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"text/template"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/utils"
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

	// TODO Get data from FERRY.  Make this concurrent
	// ferryURL
	// ferryPort
	ferryData := make([]*utils.UIDEntryFromFerry, 0)
	ferryClient := utils.InitializeHTTPSClient(viper.GetString("hostCert"), viper.GetString("hostKey"), viper.GetString("caPath"))

	ferryURLUIDTemplate := template.Must(template.New("ferry").Parse("{{.URL}}:{{.Port}}/{{.API}}?username={{.Username}}"))

	// TODO get all usernames from config

	usernames := getAllAccountsFromConfig()
	ferryDataChan := make(chan *utils.UIDEntryFromFerry)
	ferryDataWg := new(sync.WaitGroup)
	aggDone := make(chan struct{})

	// Start up worker to aggregate all ferry data
	// TODO Move this to package worker and give it a name
	go func(ferryDataChan <-chan *utils.UIDEntryFromFerry, aggDone chan<- struct{}) {
		defer close(aggDone)
		for ferryDatum := range ferryDataChan {
			ferryData = append(ferryData, ferryDatum)
		}
	}(ferryDataChan, aggDone)

	func() {
		defer close(ferryDataChan)
		for _, username := range usernames {
			ferryDataWg.Add(1)
			go func(username string, ferryDataChan chan<- *utils.UIDEntryFromFerry) {
				defer ferryDataWg.Done()
				ferryAPIConfig := struct{ URL, Port, API, Username string }{
					URL:      viper.GetString("ferryURL"),
					Port:     viper.GetString("ferryPort"),
					API:      "getUserInfo",
					Username: username,
				}

				var b strings.Builder
				if err := ferryURLUIDTemplate.Execute(&b, ferryAPIConfig); err != nil {
					log.Errorf("Could not execute ferryURLUID template")
					log.Fatal(err)
				}

				resp, err := ferryClient.Get(b.String())
				if err != nil {
					log.WithField("account", username).Error("Attempt to get UID from FERRY failed")
					log.WithField("account", username).Error(err)
					return
				}
				defer resp.Body.Close()
				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.WithField("account", username).Error("Could not read body from HTTP response")
				}

				parsedResponse := ferryResponse{}
				if err := json.Unmarshal(body, &parsedResponse); err != nil {
					log.WithField("account", username).Error("Could not unmarshal FERRY response")
					log.WithField("account", username).Error(err)
				}

				entry := utils.UIDEntryFromFerry{
					Username: username,
					Uid:      parsedResponse.FerryOutput.Uid,
				}

				ferryDataChan <- &entry
			}(username, ferryDataChan)
		}
		ferryDataWg.Wait()
	}()
	// Wait until FERRY data aggregation is done before we insert anything into DB
	<-aggDone

	if err = utils.InsertUidsIntoTableFromFERRY(db, ferryData); err != nil {
		log.Fatal("Could not insert FERRY data into database")
	}

	// Confirm that we did it.  Maybe this will become a debug output
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
