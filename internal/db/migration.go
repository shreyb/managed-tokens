package db

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

// Create table statements
type migration struct {
	description string
	sqlText     string
}

var migrations = []migration{
	{
		description: "Initial Version",
		sqlText: `
PRAGMA user_version=1;
PRAGMA foreign_keys=on;

CREATE TABLE uids (
username STRING NOT NULL PRIMARY KEY,
uid INTEGER NOT NULL
);

CREATE TABLE services (
id INTEGER NOT NULL PRIMARY KEY,
name STRING NOT NULL
);

CREATE TABLE nodes (
id INTEGER NOT NULL PRIMARY KEY,
name STRING NOT NULL
);

CREATE TABLE setup_errors (
service_id INTEGER,
count INTEGER,
FOREIGN KEY (service_id)
	REFERENCES services (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION
);

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
); `,
	},
}

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

	// Create tables
	if err := m.migrate(0, schemaVersion); err != nil {
		log.WithField("filename", m.filename).Error("Could not create database tables")
		return err
	}
	return nil
}

func (m *ManagedTokensDatabase) migrate(from, to int) error {
	if to > len(migrations) {
		msg := "trying to migrate to a database version that does not exist"
		log.Error(msg)
		return errors.New(msg)
	}

	for i := from; i < to; i++ {
		log.WithField("migration", fmt.Sprintf("v%d-v%d", from, to)).Debug("Migrating database")
		if _, err := m.db.Exec(migrations[i].sqlText); err != nil {
			return err
		}
	}
	return nil
}

// // Funcs to create tables
// // createUidsTable creates a database table in the FERRYUIDDatabase that holds the username to UID mapping
// func (m *ManagedTokensDatabase) createUidsTable() error {
// 	if _, err := m.db.Exec(createUIDTableStatement); err != nil {
// 		log.Error(err)
// 		return err
// 	}
// 	log.Debug("Created uid table in ManagedTokensDatabase")
// 	return nil
// }

// // createServicesTable creates a database table in the NotificationsDatabse that stores all the configured services across runs
// func (m *ManagedTokensDatabase) createServicesTable() error {
// 	if _, err := m.db.Exec(createServicesTableStatement); err != nil {
// 		log.Error(err)
// 		return err
// 	}
// 	log.Debug("Created services table in ManagedTokensDatabase")
// 	return nil
// }

// // createNodesTable creates a database table in the ManagedTokensDatabase that holds the interactive nodes across runs
// func (m *ManagedTokensDatabase) createNodesTable() error {
// 	if _, err := m.db.Exec(createNodesTableStatement); err != nil {
// 		log.Error(err)
// 		return err
// 	}
// 	log.Debug("Created nodes table in ManagedTokensDatabase")
// 	return nil
// }

// // createSetupErrorsTable creates a database table in the ManagedTokensDatabase that holds the number of runs for which a given service
// // has encountered a SetupError
// func (m *ManagedTokensDatabase) createSetupErrorsTable() error {
// 	if _, err := m.db.Exec(createSetupErrorsTableStatement); err != nil {
// 		log.Error(err)
// 		return err
// 	}
// 	log.Debug("Created setup errors table in ManagedTokensDatabase")
// 	return nil
// }

// // createPushErrorsTable creates a database table in the ManagedTokensDatabase that holds the number of runs for which a given service
// // has encountered a PushError
// func (m *ManagedTokensDatabase) createPushErrorsTable() error {
// 	if _, err := m.db.Exec(createPushErrorsTableStatement); err != nil {
// 		log.Error(err)
// 		return err
// 	}
// 	log.Debug("Created push errors table in ManagedTokensDatabase")
// 	return nil
// }
