// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

// migration describes the SQL statements needed to migrate from the immediately previous DB schema to the desired schema.  The description should
// be a helpful indicator of which version the schema is at, like "version 2".
type migration struct {
	description string
	sqlText     string
}

// Create table statements
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

// initialize  runs the initial steps needed to bring a database to the proper schema
func (m *ManagedTokensDatabase) initialize() error {
	var err error
	funcLogger := log.WithField("dbLocation", m.filename)
	// Set up the database
	if m.db, err = sql.Open("sqlite3", m.filename); err != nil {
		funcLogger.Error(err)
		return &databaseOpenError{
			m.filename,
			err,
		}
	}

	// Set our application ID
	if _, err := m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		funcLogger.Error(err)
		return &databaseCreateError{err}
	}

	// Create tables
	if err := m.migrate(0, schemaVersion); err != nil {
		funcLogger.Error("Could not create database tables")
		return err
	}
	return nil
}

// migrate runs the various migrations to bring a database from a certain schema to the desired schema.
func (m *ManagedTokensDatabase) migrate(from, to int) error {
	funcLogger := log.WithField("dbLocation", m.filename)
	if to > len(migrations) {
		msg := "trying to migrate to a database version that does not exist"
		funcLogger.Error(msg)
		return &databaseMigrateError{
			msg,
			from,
			to,
			nil,
		}
	}

	for i := from; i < to; i++ {
		funcLogger.WithField("migration", fmt.Sprintf("v%d-v%d", from, to)).Debug("Migrating database")
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
