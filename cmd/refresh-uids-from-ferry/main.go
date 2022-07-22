package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
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
		sqlStmt := `
			CREATE TABLE uids (
				username STRING NOT NULL PRIMARY KEY, 
				uid INTEGER NOT NULL 
				);
			`

		_, err = db.Exec(sqlStmt)
		if err != nil {
			log.Error(err)
			log.Fatal("Could not create database table to store UIDs")
		}

		log.Info("Created new database and table")
	}

	// TODO Get data from FERRY.  Make this concurrent
	// ferryURL
	// ferryPort
	ferryData := make([]*entryFromFerry, 0)
	ferryClient := InitializeHTTPSClientForFerry()

	ferryURLUIDTemplate := template.Must(template.New("ferry").Parse("{{.URL}}:{{.Port}}/{{.API}}?username={{.Username}}"))

	// TODO get all usernames from config

	usernames := getAllAccountsFromConfig()

	for _, username := range usernames {
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
			continue
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

		entry := entryFromFerry{
			Username: username,
			Uid:      parsedResponse.FerryOutput.Uid,
		}

		ferryData = append(ferryData, &entry)

	}

	tx, err := db.Begin()
	if err != nil {
		log.Error(err)
		log.Fatal("Could not open transaction to database")
	}

	func() {
		insertStatement, err := tx.Prepare(`
	INSERT INTO uids(username, uid)
	VALUES
		(?, ?)
	ON CONFLICT(username) DO
		UPDATE SET uid = ?;
		`)
		if err != nil {
			log.Error(err)
			log.Fatal("Could not prepare INSERT statement to database")
		}
		defer insertStatement.Close()

		for _, datum := range ferryData {
			_, err := insertStatement.Exec(datum.Username, datum.Uid, datum.Uid)
			if err != nil {
				log.Error(err)
				log.Fatal("Could not insert FERRY data into database")
			}
		}

		err = tx.Commit()
		if err != nil {
			log.Error(err)
			log.Fatal("Could not commit transaction to database.  Rolling back.")
		}

		log.Infof("Inserted data into database")

	}()

	// Confirm that we did it.  Maybe this will become a debug output
	func() {
		var username string
		var uid int
		rowsOut := make([][]string, 0)
		rows, err := db.Query(`SELECT username, uid FROM uids;`)
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			err := rows.Scan(&username, &uid)
			if err != nil {
				log.Fatal(err)
			}
			rowsOut = append(rowsOut, []string{
				username,
				strconv.Itoa(uid),
			})
		}
		log.Info("UID output: ", rowsOut)
		err = rows.Err()
		if err != nil {
			log.Fatal(err)
		}
	}()

}

type ferryResponse struct {
	FerryStatus string   `json:"ferry_status"`
	FerryError  []string `json:"ferry_error"`
	FerryOutput struct {
		ExpirationDate string `json:"expirationdate"`
		FullName       string `json:"fullname"`
		GroupAccount   bool   `json:"groupaccount"`
		Status         bool   `json:"status"`
		Uid            int    `json:"uid"`
		VOPersonId     string `json:"vopersonid"`
	} `json:"ferry_output"`
}

type entryFromFerry struct {
	Username string
	Uid      int
}

// InitializeHTTPSClientForFerry sets up the HTTPS client to query the FERRY service
func InitializeHTTPSClientForFerry() *http.Client {
	log.Debug("Initializing client to query FERRY")
	caCertSlice := make([]string, 0)
	caCertPool := x509.NewCertPool()

	// Adapted from  https://gist.github.com/michaljemala/d6f4e01c4834bf47a9c4
	// Load host cert
	cert, err := tls.LoadX509KeyPair(viper.GetString("hostCert"), viper.GetString("hostKey"))
	if err != nil {
		log.Fatal(err)
	}

	// Load CA certs
	caFiles, err := ioutil.ReadDir(viper.GetString("caPath"))
	if err != nil {
		log.WithField("caPath", viper.GetString("caPath")).Fatal(err)

	}
	for _, f := range caFiles {
		if filepath.Ext(f.Name()) == ".pem" {
			filenameToAdd := path.Join(viper.GetString("caPath"), f.Name())
			caCertSlice = append(caCertSlice, filenameToAdd)
		}
	}
	for _, f := range caCertSlice {
		caCert, err := ioutil.ReadFile(f)
		if err != nil {
			log.WithField("filename", f).Warn(err)
		}
		caCertPool.AppendCertsFromPEM(caCert)
	}
	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates:  []tls.Certificate{cert},
		RootCAs:       caCertPool,
		Renegotiation: tls.RenegotiateFreelyAsClient,
	}

	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport}
}

func getAllAccountsFromConfig() []string {
	s := make([]string, 0)

	for experiment := range viper.GetStringMap("experiments") {
		roleConfigPath := "experiments." + experiment + ".roles"
		for role := range viper.GetStringMap(roleConfigPath) {
			accountConfigPath := roleConfigPath + "." + role + ".account"
			account := viper.GetString(accountConfigPath)
			log.WithField("account", account).Info("Found account")
			s = append(s, account)
		}
	}
	return s
}
