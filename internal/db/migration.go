package db

import (
	"database/sql"
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
username STRING UNIQUE NOT NULL PRIMARY KEY,
uid INTEGER UNIQUE NOT NULL
);

CREATE TABLE services (
id INTEGER NOT NULL PRIMARY KEY,
name STRING UNIQUE NOT NULL
);

CREATE TABLE nodes (
id INTEGER NOT NULL PRIMARY KEY,
name STRING UNIQUE NOT NULL
);

CREATE TABLE setup_errors (
service_id INTEGER UNIQUE,
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
UNIQUE(service_id, node_id),
FOREIGN KEY (service_id)
	REFERENCES services (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION,
FOREIGN KEY (node_id)
	REFERENCES nodes (id)
		ON DELETE CASCADE
		ON UPDATE NO ACTION
);`,
	},
}

// Set up the database
func (m *ManagedTokensDatabase) initialize() error {
	var err error
	if m.db, err = sql.Open("sqlite3", m.filename); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseOpenError{
			m.filename,
			err,
		}
	}

	// Set our application ID
	if _, err := m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return &databaseCreateError{err}
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
		return &databaseMigrateError{
			msg,
			from,
			to,
			nil,
		}
	}

	for i := from; i < to; i++ {
		log.WithField("migration", fmt.Sprintf("v%d-v%d", from, to)).Debug("Migrating database")
		if _, err := m.db.Exec(migrations[i].sqlText); err != nil {
			return &databaseMigrateError{
				"",
				from,
				to,
				err,
			}
		}
	}
	return nil
}

// databaseCreateError is returned when the database cannot be created
type databaseCreateError struct {
	err error
}

func (d *databaseCreateError) Error() string {
	msg := "Could not create new ManagedTokensDatabase"
	if d.err != nil {
		return fmt.Sprintf("%s: %s", msg, d.err)
	}
	return msg
}
func (d *databaseCreateError) Unwrap() error { return d.err }

// databaseOpenError is returned when the database file cannot be opened
type databaseOpenError struct {
	filename string
	err      error
}

func (d *databaseOpenError) Error() string {
	msg := fmt.Sprintf("Could not open database file at %s", d.filename)
	if d.err != nil {
		return fmt.Sprintf("%s: %s", msg, d.err)
	}
	return msg
}
func (d *databaseOpenError) Unwrap() error { return d.err }

// databaseMigrateError is returned when migration between schema versions fails
type databaseMigrateError struct {
	msg      string
	from, to int
	err      error
}

func (d *databaseMigrateError) Error() string {
	msg := fmt.Sprintf("Could not migrate between schemaVersions %d and %d", d.from, d.to)
	if d.msg != "" {
		msg = fmt.Sprintf("%s: %s", msg, d.msg)
	}
	if d.err != nil {
		return fmt.Sprintf("%s: %s", msg, d.err)
	}
	return msg
}
func (d *databaseMigrateError) Unwrap() error { return d.err }

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
