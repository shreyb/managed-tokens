package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

// NotificationsDatabaseCountDatum is a piece of data that can be used to hold information about
// notifications counts for a given service and optionally a node
type NotificationsDatabaseCountDatum interface {
	ServiceName() string
	NodeName() string
	Count() uint16
	String() string
}

// NotificationsDatabase is a database in which information about how many notifications have been sent about a particular failure have
// been generated
type NotificationsDatabase struct {
	filename string
	db       *sql.DB
}

// SQL statements to be used by API
// Create table statements
var (
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

// Query db actions
var (
	getSetupErrorsCountsStatement = `
	SELECT
		services.name,
		setup_errors.count
	FROM
		setup_errors
		INNER JOIN services ON services.id = setup_errors.service_id
	;
	`
	getPushErrorsCountsStatement = `
	SELECT
		services.name,
		nodes.name,
		push_errors.count
	FROM
		push_errors
		INNER JOIN services ON services.id = push_errors.service_id
		INNER JOIN nodes on nodes.id = push_errors.node_id
	;
	`
	getAllServicesFromTableStatement = `
	SELECT name FROM services;
	`
	getAllNodesFromTableStatement = `
	SELECT name FROM nodes;
	`
)

// INSERT/UPDATE actions
var (
	insertIntoServicesTableStatement = `
	INSERT INTO services(name)
	VALUES
		(?)
	ON CONFLICT(name) DO NOTHING;
	`
	insertIntoNodesTableStatement = `
	INSERT INTO nodes(name)
	VALUES
		(?)
	ON CONFLICT(name) DO NOTHING;
	`
	insertOrUpdateSetupErrorsStatement = `
	INSERT INTO setup_errors(service_id, count)
		SELECT
			services.id,
			? as new_count
		FROM
			services
		WHERE
			services.name = ?
	ON CONFLICT(service_id) DO
		UPDATE SET count = ?
	;
	`
	insertOrUpdatePushErrorsStatement = `
	INSERT INTO push_errors(service_id, node_id, count)
		SELECT
			services.id,
			nodes.id,
			? as new_count
		FROM
			push_errors
			INNER JOIN services ON services.id = push_errors.service_id
			INNER JOIN nodes ON nodes.id = push_errors.node_id
		WHERE
			services.name = ?
			AND nodes.name = ?
	ON CONFLICT(service_id, node_id) DO
		UPDATE SET count = ?
	;
	`
)

// TODO Need queries to actually create tables

// Pragma:  PRAGMA foreign_keys = ON;
// Schema:
// sqlite> .schema
// CREATE TABLE services (
// id INTEGER NOT NULL PRIMARY KEY,
// name STRING NOT NULL
// );
// CREATE TABLE nodes (
// id INTEGER NOT NULL PRIMARY KEY,
// name STRING NOT NULL
// );
// CREATE TABLE push_errors (
// service_id INTEGER,
// node_id INTEGER,
// count INTEGER,
// FOREIGN KEY (service_id)
//   REFERENCES services (id)
//     ON DELETE CASCADE
//     ON UPDATE NO ACTION,
// FOREIGN KEY (node_id)
//   REFERENCES nodes (id)
//     ON DELETE CASCADE
//     ON UPDATE NO ACTION
// );
// CREATE TABLE setup_errors (
// service_id INTEGER,
// count INTEGER,
// FOREIGN KEY (service_id)
//   REFERENCES services (id)
//     ON DELETE CASCADE
//     ON UPDATE NO ACTION
// );

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
	// Enforce foreign key constraints
	if _, err := n.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return err
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
	if err = n.Open(); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return err
	}

	// Set our application ID
	if _, err := n.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return err
	}

	// Create the tables in the database
	if err := n.createServicesTable(); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := n.createNodesTable(); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := n.createSetupErrorsTable(); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}
	if err := n.createPushErrorsTable(); err != nil {
		log.WithField("filename", n.filename).Error(err)
		return &databaseCreateError{err.Error()}
	}

	return nil
}

// Funcs to create tables
// createServicesTable creates a database table in the NotificationsDatabse that stores all the configured services across runs
func (n *NotificationsDatabase) createServicesTable() error {
	if _, err := n.db.Exec(createServicesTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created services table in NotificationsDatabase")
	return nil
}

// createNodesTable creates a database table in the NotificationsDatabase that holds the interactive nodes across runs
func (n *NotificationsDatabase) createNodesTable() error {
	if _, err := n.db.Exec(createNodesTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created nodes table in NotificationsDatabase")
	return nil
}

// createSetupErrorsTable creates a database table in the NotificationsDatabase that holds the number of runs for which a given service
// has encountered a SetupError
func (n *NotificationsDatabase) createSetupErrorsTable() error {
	if _, err := n.db.Exec(createSetupErrorsTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created setup errors table in NotificationsDatabase")
	return nil
}

// createPushErrorsTable creates a database table in the NotificationsDatabase that holds the number of runs for which a given service
// has encountered a PushError
func (n *NotificationsDatabase) createPushErrorsTable() error {
	if _, err := n.db.Exec(createPushErrorsTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created push errors table in NotificationsDatabase")
	return nil
}

// External-facing functions to modify db
// TODO Figure out signatures.  Make sure all return errors

type serviceDatum struct{ value string }

func (s *serviceDatum) values() []any { return []any{s.value} }

func (n *NotificationsDatabase) UpdateServices(ctx context.Context, serviceNames []string) error {
	serviceDatumSlice := make([]insertValues, len(serviceNames))
	for _, s := range serviceNames {
		serviceDatumSlice = append(serviceDatumSlice, &serviceDatum{s})
	}

	if err := insertTransactionRunner(ctx, n.db, insertIntoServicesTableStatement, serviceDatumSlice); err != nil {
		log.Error("Could not update services in notifications database")
		return err
	}
	log.Info("Updated services in notifications database")
	return nil
}
func (n *NotificationsDatabase) UpdateNodes()            {}
func (n *NotificationsDatabase) GetAllServices()         {}
func (n *NotificationsDatabase) GetAllNodes()            {}
func (n *NotificationsDatabase) GetSetupErrorsInfo()     {}
func (n *NotificationsDatabase) GetPushErrorsInfo()      {}
func (n *NotificationsDatabase) UpdateSetupErrorsTable() {}
func (n *NotificationsDatabase) UpdatePushErrorsTable()  {}
