package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// NotificationsDatabase is a database in which information about how many notifications have been sent about a particular failure have
// been generated
type NotificationsDatabase struct {
	filename string
	db       *sql.DB
}

// TODO Need queries to actually create tables

// OpenOrCreateNotificationsDatabase opens a sqlite3 database for reading or writing, and returns a *NotificationsDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateNotificationsDatabase(filename string) (*NotificationsDatabase, error) {
	var err error
	n := NotificationsDatabase{filename: filename}
	err = openOrCreateDatabase(&n)
	if err != nil {
		log.WithField("filename", n.filename).Error("Could not create or open NotificationsDatabase")
	}
	return &n, err
}

// Filename returns the path to the file holding the database
func (n *NotificationsDatabase) Filename() string {
	return n.filename
}

// Database returns the underlying SQL database of the NotificationsDatabase
func (n *NotificationsDatabase) Database() *sql.DB {
	return n.db
}

// Open opens the database located at NotificationsDatabase.filename and stores the opened *sql.DB object in the NotificationsDatabase
func (n *NotificationsDatabase) Open() error {
	var err error
	n.db, err = sql.Open("sqlite3", n.filename)
	if err != nil {
		msg := "Could not open the database file"
		log.WithField("filename", n.filename).Errorf("%s: %s", msg, err)
		return &databaseOpenError{msg}
	}
	return nil
}

// Close closes the NotificationsDatabase
func (n *NotificationsDatabase) Close() error {
	return n.db.Close()
}

// initialize prepares a new NotificationsDatabase for use and returns a pointer to the underlying sql.DB
func (n *NotificationsDatabase) initialize() error {
	var err error
	if n.db, err = sql.Open("sqlite3", n.filename); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return err
	}

	// Set our application ID
	if _, err := n.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return err
	}

	// TODO Create the correct tables

	return nil
}
