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
	log "github.com/sirupsen/logrus"

	"github.com/fermitools/managed-tokens/internal/utils"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

const (
	// ApplicationId is used to uniquely identify a sqlite database as belonging to an application, rather than being a simple DB
	ApplicationId              = 0x5da82553
	dbDefaultTimeoutStr string = "10s"
	schemaVersion              = 1
)

// ManagedTokensDatabase is a database in which FERRY username to uid mappings are stored.  It is the main type that external packages
// will use to interact with the database.
type ManagedTokensDatabase struct {
	filename string
	db       *sql.DB
}

// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateDatabase(filename string) (*ManagedTokensDatabase, error) {
	funcLog := log.WithField("dbLocation", filename)

	m := ManagedTokensDatabase{filename: filename}
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		if err = m.create(); err != nil {
			return nil, err
		}
		funcLog.Debug("Created new ManagedTokensDatabase")
	} else {
		if err = m.open(); err != nil {
			return nil, err
		}
		funcLog.Debug("ManagedTokensDatabase file already exists.  Will try to use it")
	}

	if err := m.addForeignKeyConstraintsToDB(); err != nil {
		return nil, err
	}

	if err := m.check(); err != nil {
		funcLog.Error("ManagedTokensDatabase failed check")
		return nil, err
	}

	funcLog.Debug("ManagedTokensDatabase connection ready")
	return &m, nil
}

func (m *ManagedTokensDatabase) create() error {
	funcLog := log.WithField("dbLocation", m.filename)
	if err := m.initialize(); err != nil {
		funcLog.Error("Could not initialize database")
		if err2 := os.Remove(m.filename); errors.Is(err2, os.ErrNotExist) {
			funcLog.Error("Could not remove corrupt database file.  Please do so manually")
			return err2
		}
		return err
	}
	return nil
}

func (m *ManagedTokensDatabase) open() error {
	var err error
	m.db, err = sql.Open("sqlite3", m.filename)
	if err != nil {
		msg := "Could not open the managed tokens database file"
		log.WithField("dbLocation", m.filename).Errorf("%s: %s", msg, err)
		return &databaseOpenError{m.filename, err}
	}
	return nil
}

func (m *ManagedTokensDatabase) addForeignKeyConstraintsToDB() error {
	if _, err := m.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		log.WithField("dbLocation", m.filename).Error(err)
		return err
	}
	return nil
}

// Close closes the FERRYUIDDatabase
func (m *ManagedTokensDatabase) Close() error {
	return m.db.Close()
}

// check makes sure that an object claiming to be a ManagedTokensDatabase actually is, by checking the ApplicationID
func (m *ManagedTokensDatabase) check() error {
	funcLog := log.WithField("dbLocation", m.filename)

	err := m.checkApplicationId()
	if err != nil {
		funcLog.Error("ApplicationId check failed")
		return err
	}

	// Migrate to the right userVersion of the database
	var userVersion int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		msg := "Could not get user_version from ManagedTokensDatabase"
		funcLog.Error(msg)
		return &databaseCheckError{msg, err}
	}
	if userVersion < schemaVersion {
		if err := m.migrate(userVersion, schemaVersion); err != nil {
			return &databaseCheckError{"Error migrating database schema versions", err}
		}
	} else if userVersion > schemaVersion {
		funcLog.Warn("Database is from a newer version of the Managed Tokens library.  There may have been breaking changes in newer migrations")
	}
	return nil
}

func (m *ManagedTokensDatabase) checkApplicationId() error {
	var dbApplicationId int
	funcLog := log.WithField("dbLocation", m.filename)
	if err := m.db.QueryRow("PRAGMA application_id").Scan(&dbApplicationId); err != nil {
		msg := "Could not get application_id from ManagedTokensDatabase"
		funcLog.Error(msg)
		return &databaseCheckError{msg, err}
	}
	// Make sure our application IDs match
	if dbApplicationId != ApplicationId {
		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
		funcLog.Errorf(errMsg)
		return &databaseCheckError{errMsg, nil}
	}
	return nil
}

// Main funcs to use internally for interacting with the database

// getValuesTransactionRunner queries a database table and returns a [][]any of the row values requested.  This is the main func to run
// from this library for retrieving data from the database
func getValuesTransactionRunner(ctx context.Context, db *sql.DB, getStatementString string, args ...any) ([][]any, error) {
	data := make([][]any, 0)

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return data, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	// Query the DB
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

	// Get column names so we can get the length of each row
	cols, err := rows.Columns()
	if err != nil {
		log.Error("Error getting columns from query results")
		return data, err
	}

	// Scan our rows and add them to data
	for rows.Next() {
		resultRow, resultRowPtrs := prepareDataAndPointerSliceForDBRows(len(cols))
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

// getNamedDimensionStringValues queries a table as given in the sqlGetStatement provided that each row returned by the query
// in sqlGetStatement is a single string (one column of string type). An example of a valid query for sqlGetStatement would be
// "SELECT name FROM table".  An invalid query would be "SELECT id, name FROM table".  This is the main func to use to get
// dimension-like data in this library
func getNamedDimensionStringValues(ctx context.Context, db *sql.DB, sqlGetStatement string) ([]string, error) {
	data, err := getValuesTransactionRunner(ctx, db, sqlGetStatement)
	if err != nil {
		log.Error("Could not get values from database")
		return nil, err
	}

	if len(data) == 0 {
		log.Debug("No values in database")
		return nil, nil
	}

	unpackedData, err := unpackNamedDimensionData(data)
	if err != nil {
		log.Error("Error unpacking named dimension data")
		return nil, err
	}

	return unpackedData, nil
}

// insertTransactionRunner inserts data into a database.  Besides the context to be used and the database
// itself, it also takes an insertStatementString string that contains the SQL query to be prepared and
// filled in with the values given by each element of insertData.  This is the main func to use in this library to insert
// values into the database
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

	// Run the passed-in insert statement on insertData
	for _, datum := range insertData {
		datumValues := datum.insertValues()
		_, err := insertStatement.ExecContext(dbContext, datumValues...)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return dbContext.Err()
			}
			log.Errorf("Could not insert data into database: %s", err)
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

// Helper interfaces

// insertValues is a bridge interface that contains a values() method.  This values method should
// return a []any, where each element is any(value).  For each element, the type of value should
// match the intended database column type.  For example, to use this interface to insert to columns
// of type (string, int), we would do something like the following:
//
//	type myType struct {
//		stringField string
//		intField    int
//	}
//
//	func (m *myType) values() []any { return []any{any(m.stringField), any(m.intField)} }
//
// And then pass in a []*myType as the insertData parameter in insertTransactionRunner
type insertValues interface {
	insertValues() []any
}

// dataRowUnpacker is an interface to wrap types that support unpacking slices of data.
// Generally, the unpackDataRow method for each implementing type should return an instance of dataRowUnpacker whose underlying type is the pointer to
// the implementing type, and for which the underlying value is populated from the slice of data.  For example, if we have a type myType that
// implements dataRowUnpacker, it should be structured as follows:
//
//	type myType struct{ // fields }
//	func (*m myType) unpackDataRow(data []any) (dataRowUnpacker, error) {
//	  var m2 myType
//	  m2 = doSomethingWithData()
//	  return dataRowUnpacker(&m2)
//	}
type dataRowUnpacker interface {
	unpackDataRow([]any) (dataRowUnpacker, error)
}

// Helper funcs

// prepareDataAndPointerSliceForDBRows gives us a slice of pointers that point back to the row values themselves
func prepareDataAndPointerSliceForDBRows(length int) ([]any, []any) {
	resultRow := make([]any, length)
	resultRowPtrs := make([]any, length)
	for idx := range resultRow {
		resultRowPtrs[idx] = &resultRow[idx]
	}
	return resultRow, resultRowPtrs
}

// unpackNamedDimensionData takes a slice of data, and makes sure it's legitimate dimension data, meaning after type checks, it should
// be [][]string, where len([]string) is 1.  It then extracts the single element in each []string, combines them,
// and returns those values as a []string
func unpackNamedDimensionData(data [][]any) ([]string, error) {
	dataConverted := make([]string, 0, len(data))
	for _, resultRow := range data {
		if len(resultRow) != 1 {
			msg := "dimension name data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return nil, errDatabaseDataWrongStructure
		}
		if val, ok := resultRow[0].(string); !ok {
			msg := "dimension name query result has wrong type.  Expected string"
			log.Errorf("%s: got %T", msg, val)
			return nil, errDatabaseDataWrongType
		} else {
			log.Debugf("Got dimension row: %s", val)
			dataConverted = append(dataConverted, val)
		}
	}
	return dataConverted, nil
}

// unpackData takes row data and applies the unpackDataRow method of the type T to return a slice of type T
func unpackData[T dataRowUnpacker](data [][]any) ([]T, error) {
	unpackedData := make([]T, 0, len(data))
	for _, row := range data {
		var datum T
		unpackedDatum, err := datum.unpackDataRow(row)
		if err != nil {
			log.Error("Error unpacking data")
			return nil, err
		}
		datumVal, ok := unpackedDatum.(T)
		if !ok {
			msg := "unpackDataRow returned wrong type"
			log.Error(msg)
			return nil, errors.New(msg)
		}
		unpackedData = append(unpackedData, datumVal)
	}
	return unpackedData, nil
}

// convertStringSliceToInsertValuesSlice takes a string slice and converts it to a slice of types
func convertStringSliceToInsertValuesSlice(converter func(string) insertValues, stringSlice []string) []insertValues {
	sl := make([]insertValues, 0, len(stringSlice))
	for _, elt := range stringSlice {
		valToAdd := converter(elt)
		sl = append(sl, valToAdd)
	}
	return sl
}

// newInsertValuesFromUnderlyingString takes a string and wraps it in the insertValues interface.  The underlying type of the return
// value is given with the type parameters, for example:
//
//	myVal := newInsertValuesFromUnderlyingString[*myType, myType]("mystring")
//
// The underlying type myType in the above example must have underlying type string and its pointer must implement insertValues.
func newInsertValuesFromUnderlyingString[T1 interface {
	*T2
	insertValues
}, T2 ~string](s string) insertValues {
	underlyingTypeValue := T2(s)
	pointerToTypeValue := T1(&underlyingTypeValue)
	return insertValues(pointerToTypeValue)
}

// Errors

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
