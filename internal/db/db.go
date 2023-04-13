// Package db provides the FERRYUIDDatabase struct which provides an interface to a SQLite3 database that is used by the managed tokens
// utilities to store username-UID mappings, as provided from FERRY.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

const (
	// ApplicationId is used to uniquely identify a sqlite database as belonging to an application, rather than being a simple DB
	ApplicationId              = 0x5da82553
	dbDefaultTimeoutStr string = "10s"
)

type databaseConnector interface {
	Filename() string
	Database() *sql.DB
	Open() error
	Close() error
	initialize() error
}

// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func openOrCreateDatabase(db databaseConnector) error {
	filename := db.Filename()
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		// DB file doesn't exist: Create a new database and initialize it
		err = db.initialize()
		if err != nil {
			msg := "Could not create new database"
			log.Error(msg)
			if err := os.Remove(filename); errors.Is(err, os.ErrNotExist) {
				log.Error("Could not remove corrupt database file.  Please do so manually")
				return err
			}
			return &databaseCreateError{msg}
		}
		log.WithField("filename", filename).Debug("Created new database")
	} else {
		// Database file exists, so try to open the DB and use it
		err = db.Open()
		if err != nil {
			msg := "Could not open the database file"
			log.WithField("filename", filename).Errorf("%s: %s", msg, err)
			return &databaseOpenError{msg}
		}
		log.WithField("filename", filename).Debug("Database file already exists.  Will try to use it")
	}
	if err := checkDatabaseApplicationId(db); err != nil {
		// Check the database
		msg := "database failed check"
		log.WithField("filename", filename).Error(msg)
		return &databaseCheckError{msg}
	}
	log.WithField("filename", filename).Debug("database connection ready")
	return nil
}

// check makes sure that an object claiming to be a NotificationsDatabase actually is, by checking the ApplicationID
func checkDatabaseApplicationId(db databaseConnector) error {
	var dbApplicationId int
	err := db.Database().QueryRow("PRAGMA application_id").Scan(&dbApplicationId)
	if err != nil {
		log.WithField("filename", db.Filename()).Error("Could not get application_id from database")
		return err
	}
	if dbApplicationId != ApplicationId {
		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
		log.WithField("filename", db.Filename()).Errorf(errMsg)
		return errors.New(errMsg)
	}
	return nil
}

// databaseCreateError is returned when the database cannot be created
type databaseCreateError struct{ msg string }

func (d *databaseCreateError) Error() string { return d.msg }

// databaseOpenError is returned when the database cannot be opened
type databaseOpenError struct{ msg string }

func (d *databaseOpenError) Error() string { return d.msg }

// databaseCheckError is returned when the database fails the verification check
type databaseCheckError struct{ msg string }

func (d *databaseCheckError) Error() string { return d.msg }
