// Package db provides the FERRYUIDDatabase struct which provides an interface to a SQLite3 database that is used by the managed tokens
// utilities to store username-UID mappings, as provided from FERRY.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

const (
	// ApplicationId is used to uniquely identify a sqlite database as belonging to an application, rather than being a simple DB
	ApplicationId              = 0x5da82553
	dbDefaultTimeoutStr string = "10s"
	schemaVersion              = 1
)

// ManagedTokensDatabase is a database in which FERRY username to uid mappings are stored
type ManagedTokensDatabase struct {
	filename string
	db       *sql.DB
}

// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateDatabase(filename string) (*ManagedTokensDatabase, error) {
	m := ManagedTokensDatabase{filename: filename}
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		err = m.initialize()
		if err != nil {
			msg := "Could not initialize database"
			log.Error(msg)
			if err := os.Remove(filename); errors.Is(err, os.ErrNotExist) {
				log.Error("Could not remove corrupt database file.  Please do so manually")
				return &ManagedTokensDatabase{}, err
			}
			return nil, err
		}
		log.WithField("filename", filename).Debug("Created new ManagedTokensDatabase")
	} else {
		m.db, err = sql.Open("sqlite3", filename)
		if err != nil {
			msg := "Could not open the UID database file"
			log.WithField("filename", filename).Errorf("%s: %s", msg, err)
			return nil, &databaseOpenError{filename, err}
		}
		log.WithField("filename", filename).Debug("ManagedTokensDatabase file already exists.  Will try to use it")
	}
	// Enforce foreign key constraints
	if _, err := m.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		log.WithField("filename", m.filename).Error(err)
		return nil, err
	}
	if err := m.check(); err != nil {
		msg := "ManagedTokensDatabase failed check"
		log.WithField("filename", filename).Error(msg)
		return nil, err
	}
	log.WithField("filename", filename).Debug("ManagedTokensDatabase connection ready")
	return &m, nil
}

// Close closes the FERRYUIDDatabase
func (m *ManagedTokensDatabase) Close() error {
	return m.db.Close()
}

// check makes sure that an object claiming to be a ManagedTokensDatabase actually is, by checking the ApplicationID
func (m *ManagedTokensDatabase) check() error {
	var dbApplicationId int
	if err := m.db.QueryRow("PRAGMA application_id").Scan(&dbApplicationId); err != nil {
		msg := "Could not get application_id from ManagedTokensDatabase"
		log.WithField("filename", m.filename).Error(msg)
		return &databaseCheckError{msg, err}
	}
	// Make sure our application IDs match
	if dbApplicationId != ApplicationId {
		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
		log.WithField("filename", m.filename).Errorf(errMsg)
		return &databaseCheckError{errMsg, nil}
	}
	// Migrate to the right userVersion of the database
	var userVersion int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		msg := "Could not get user_version from ManagedTokensDatabase"
		log.WithField("filename", m.filename).Error(msg)
		return &databaseCheckError{msg, err}
	}
	if userVersion < schemaVersion {
		if err := m.migrate(userVersion, schemaVersion); err != nil {
			return &databaseCheckError{"Error migrating database schema versions", err}
		}
	} else if userVersion > schemaVersion {
		log.Warn("Database is from a newer version of the Managed Tokens library.  There may have been breaking changes in newer migrations")
	}
	return nil
}

// getValuesTransactionRunner queries a database table and returns a [][]any of the row values requested.
func getValuesTransactionRunner(ctx context.Context, db *sql.DB, getStatementString string, args ...any) ([][]any, error) {
	data := make([][]any, 0)

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return data, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	rows, err := db.QueryContext(dbContext, getStatementString, args...)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return data, dbContext.Err()
		}
		log.Errorf("Error running SELECT query against database: %s", err)
		return data, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Error("Error getting columns from query results")
		return data, err
	}

	for rows.Next() {
		resultRow := make([]any, len(cols))
		resultRowPtrs := make([]any, len(cols))
		for idx := range resultRow {
			resultRowPtrs[idx] = &resultRow[idx]
		}
		err := rows.Scan(resultRowPtrs...)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return data, dbContext.Err()
			}
			log.Errorf("Error retrieving results of SELECT query: %s", err)
			return data, err
		}
		data = append(data, resultRow)
		log.Debugf("Got row values from database: %s", resultRow...)
	}
	err = rows.Err()
	if err != nil {
		log.Error(err)
		return data, err
	}
	return data, nil
}

// insertValues is a bridge interface that contains a values() method.  This values method should
// return a []any, where each element is any(value).  For each element, the type of value should
// match the intended database column type.  For example, to use this interface to insert to columns
// of type (string, int), we would do something like the following:
//
//	type myType struct {
//			stringField string
//			intField    int
//	}
//
// func (m *myType) values() []any { return []any{any(m.stringField), any(m.intField)} }
//
// And then pass in a []*myType as the insertData parameter in insertTransactionRunner
type insertValues interface {
	values() []any
}

// insertTransactionRunner inserts data into a database.  Besides the context to be used and the databse
// itself, it also takes an insertStatementString string that contains the SQL query to be prepared and
// filled in with the values given by each element of insertData.
func insertValuesTransactionRunner(ctx context.Context, db *sql.DB, insertStatementString string, insertData []insertValues) error {
	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	tx, err := db.Begin()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not open transaction to database: %s", err)
		return err
	}

	insertStatement, err := tx.Prepare(insertStatementString)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not prepare INSERT statement to database: %s", err)
		return err
	}
	defer insertStatement.Close()

	// Run the passed-in insertFunc on insertData
	for _, datum := range insertData {
		datumValues := datum.values()
		_, err := insertStatement.ExecContext(dbContext, datumValues...)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return dbContext.Err()
			}
			log.Errorf("Could not insert FERRY data into database: %s", err)
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not commit transaction to database.  Rolling back.  Error: %s", err)
		return err
	}

	log.Debug("Inserted data into database")
	return nil
}

// databaseCheckError is returned when the database fails the verification check
type databaseCheckError struct {
	msg string
	err error
}

func (d *databaseCheckError) Error() string {
	msg := d.msg
	if d.err != nil {
		return fmt.Sprintf("%s: %s", msg, d.err)
	}
	return msg
}
func (d *databaseCheckError) Unwrap() error { return d.err }

var (
	errDatabaseDataWrongStructure error = errors.New("returned data has wrong structure")
	errDatabaseDataWrongType      error = errors.New("returned data has wrong type")
)

// type databaseConnector interface {
// 	Filename() string
// 	Database() *sql.DB
// 	Open() error
// 	Close() error
// 	initialize() error
// }

// initialize prepares a new FERRYUIDDatabase for use
// func (m *ManagedTokensDatabase) initialize() error {
// 	var err error
// 	if m.db, err = sql.Open("sqlite3", m.filename); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return err
// 	}

// 	// Set our application ID
// 	if _, err := m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return err
// 	}

// 	// Create the UID table
// 	if err = m.createUidsTable(); err != nil {
// 		log.Error("Could not create the UID table in the ManagedTokensDatabase")
// 		return err
// 	}
// 	// Create the tables in the database
// 	if err := m.createServicesTable(); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return &databaseCreateError{err.Error()}
// 	}
// 	if err := m.createNodesTable(); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return &databaseCreateError{err.Error()}
// 	}
// 	if err := m.createSetupErrorsTable(); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return &databaseCreateError{err.Error()}
// 	}
// 	if err := m.createPushErrorsTable(); err != nil {
// 		log.WithField("filename", m.filename).Error(err)
// 		return &databaseCreateError{err.Error()}
// 	}
// 	return nil
// }

// func openOrCreateDatabase(db databaseConnector) error {
// 	filename := db.Filename()
// 	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
// 		// DB file doesn't exist: Create a new database and initialize it
// 		err = db.initialize()
// 		if err != nil {
// 			msg := "Could not create new database"
// 			log.Error(msg)
// 			if err := os.Remove(filename); errors.Is(err, os.ErrNotExist) {
// 				log.Error("Could not remove corrupt database file.  Please do so manually")
// 				return err
// 			}
// 			return &databaseCreateError{msg}
// 		}
// 		log.WithField("filename", filename).Debug("Created new database")
// 	} else {
// 		// Database file exists, so try to open the DB and use it
// 		err = db.Open()
// 		if err != nil {
// 			msg := "Could not open the database file"
// 			log.WithField("filename", filename).Errorf("%s: %s", msg, err)
// 			return &databaseOpenError{msg}
// 		}
// 		log.WithField("filename", filename).Debug("Database file already exists.  Will try to use it")
// 	}
// 	if err := checkDatabaseApplicationId(db); err != nil {
// 		// Check the database
// 		msg := "database failed check"
// 		log.WithField("filename", filename).Error(msg)
// 		return &databaseCheckError{msg}
// 	}
// 	log.WithField("filename", filename).Debug("database connection ready")
// 	return nil
// }

// // check makes sure that an object claiming to be a NotificationsDatabase actually is, by checking the ApplicationID
// func checkDatabaseApplicationId(db databaseConnector) error {
// 	var dbApplicationId int
// 	err := db.Database().QueryRow("PRAGMA application_id").Scan(&dbApplicationId)
// 	if err != nil {
// 		log.WithField("filename", db.Filename()).Error("Could not get application_id from database")
// 		return err
// 	}
// 	if dbApplicationId != ApplicationId {
// 		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
// 		log.WithField("filename", db.Filename()).Errorf(errMsg)
// 		return errors.New(errMsg)
// 	}
// 	return nil
// }

// // getDimensionValuesTransactionRunner queries a database dimensions table (one with only id/name column structures)
// // and returns a []string of the dimension values
// func getDimensionValuesTransactionRunner(ctx context.Context, db *sql.DB, getStatementString string) ([]string, error) {
// 	data := make([]string, 0)

// 	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
// 	if err != nil {
// 		log.Error("Could not parse db timeout duration")
// 		return data, err
// 	}
// 	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
// 	defer dbCancel()

// 	rows, err := db.QueryContext(dbContext, getStatementString)
// 	if err != nil {
// 		if dbContext.Err() == context.DeadlineExceeded {
// 			log.Error("Context timeout")
// 			return data, dbContext.Err()
// 		}
// 		log.Errorf("Error running SELECT query against database: %s", err)
// 		return data, err
// 	}
// 	defer rows.Close()
// 	for rows.Next() {
// 		var value string
// 		err := rows.Scan(&value)
// 		if err != nil {
// 			if dbContext.Err() == context.DeadlineExceeded {
// 				log.Error("Context timeout")
// 				return data, dbContext.Err()
// 			}
// 			log.Errorf("Error retrieving results of SELECT query: %s", err)
// 			return data, err
// 		}
// 		data = append(data, value)
// 		log.Debugf("Got dimension value: %s", value)
// 	}
// 	err = rows.Err()
// 	if err != nil {
// 		log.Error(err)
// 		return data, err
// 	}
// 	return data, nil
// }

// getValuesTransactio
