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

// getValuesTransactionRunner queries a database table and returns a [][]any of the row values requested.
func getValuesTransactionRunner(ctx context.Context, db *sql.DB, getStatementString string) ([][]any, error) {
	data := make([][]any, 0)

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return data, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	rows, err := db.QueryContext(dbContext, getStatementString)
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
		log.Error("Error getting columns list from query results")
		return data, err
	}

	for rows.Next() {
		rowValues := make([]any, 0, len(cols))
		err := rows.Scan(rowValues...)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return data, dbContext.Err()
			}
			log.Errorf("Error retrieving results of SELECT query: %s", err)
			return data, err
		}
		data = append(data, rowValues)
		log.Debugf("Got row values: %s", rowValues...)
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
func insertTransactionRunner(ctx context.Context, db *sql.DB, insertStatementString string, insertData []insertValues) error {
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

// databaseCreateError is returned when the database cannot be created
type databaseCreateError struct{ msg string }

func (d *databaseCreateError) Error() string { return d.msg }

// databaseOpenError is returned when the database cannot be opened
type databaseOpenError struct{ msg string }

func (d *databaseOpenError) Error() string { return d.msg }

// databaseCheckError is returned when the database fails the verification check
type databaseCheckError struct{ msg string }

func (d *databaseCheckError) Error() string { return d.msg }
