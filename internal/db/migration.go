package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// TODO Do a migration slice like fifemail does and apply that migration when we create the database up to user version
// See https://cdcvs.fnal.gov/redmine/projects/fifemon/repository/fifemail/revisions/master/entry/db/migration.go
// Create table statements
var (
	createUIDTableStatement = `
	CREATE TABLE uids (
	username STRING NOT NULL PRIMARY KEY,
	uid INTEGER NOT NULL
	);
	`
	createServicesTableStatement = `
	CREATE TABLE services (
	id INTEGER NOT NULL PRIMARY KEY,
	name STRING NOT NULL
	);
	`
	createNodesTableStatement = `
	CREATE TABLE nodes (
	id INTEGER NOT NULL PRIMARY KEY,
	name STRING NOT NULL
	);
	`
	createSetupErrorsTableStatement = `
	CREATE TABLE setup_errors (
	service_id INTEGER,
	count INTEGER,
	FOREIGN KEY (service_id)
	REFERENCES services (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION
	);
	`
	createPushErrorsTableStatement = `
	CREATE TABLE push_errors (
	service_id INTEGER,
	node_id INTEGER,
	count INTEGER,
	FOREIGN KEY (service_id)
	REFERENCES services (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION,
	FOREIGN KEY (node_id)
	REFERENCES nodes (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION
	);
	`
)

// Set up the database
func (m *ManagedTokensDatabase) initialize() error {
	var err error
	if m.db, err = sql.Open("sqlite3", m.filename); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return err
	}

	// Set our application ID
	if _, err := m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return err
	}

	// Create the UID table
	if err = m.createUidsTable(); err != nil {
		log.Error("Could not create the UID table in the ManagedTokensDatabase")
		return err
	}
	// Create the tables in the database
	if err := m.createServicesTable(); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := m.createNodesTable(); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := m.createSetupErrorsTable(); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := m.createPushErrorsTable(); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	return nil
}

// Funcs to create tables
// createUidsTable creates a database table in the FERRYUIDDatabase that holds the username to UID mapping
func (m *ManagedTokensDatabase) createUidsTable() error {
	if _, err := m.db.Exec(createUIDTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created uid table in ManagedTokensDatabase")
	return nil
}

// createServicesTable creates a database table in the NotificationsDatabse that stores all the configured services across runs
func (m *ManagedTokensDatabase) createServicesTable() error {
	if _, err := m.db.Exec(createServicesTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created services table in ManagedTokensDatabase")
	return nil
}

// createNodesTable creates a database table in the ManagedTokensDatabase that holds the interactive nodes across runs
func (m *ManagedTokensDatabase) createNodesTable() error {
	if _, err := m.db.Exec(createNodesTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created nodes table in ManagedTokensDatabase")
	return nil
}

// createSetupErrorsTable creates a database table in the ManagedTokensDatabase that holds the number of runs for which a given service
// has encountered a SetupError
func (m *ManagedTokensDatabase) createSetupErrorsTable() error {
	if _, err := m.db.Exec(createSetupErrorsTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created setup errors table in ManagedTokensDatabase")
	return nil
}

// createPushErrorsTable creates a database table in the ManagedTokensDatabase that holds the number of runs for which a given service
// has encountered a PushError
func (m *ManagedTokensDatabase) createPushErrorsTable() error {
	if _, err := m.db.Exec(createPushErrorsTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created push errors table in ManagedTokensDatabase")
	return nil
}
