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
	"strconv"

	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/tracing"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

const (
	// ApplicationId is used to uniquely identify a sqlite database as belonging to an application, rather than being a simple DB
	ApplicationId              = 0x5da82553
	dbDefaultTimeoutStr string = "10s"
	schemaVersion              = 1
)

var tracer = otel.Tracer("db")

// ManagedTokensDatabase is a database in which FERRY username to uid mappings are stored.  It is the main type that external packages
// will use to interact with the database.
type ManagedTokensDatabase struct {
	filename string
	db       *sql.DB
}

// Location returns the location of the ManagedTokensDatabase file
func (m *ManagedTokensDatabase) Location() string {
	return m.filename
}

// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateDatabase(filename string) (*ManagedTokensDatabase, error) {
	m := ManagedTokensDatabase{filename: filename}
	_, err := os.Stat(filename)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = m.create(); err != nil {
			return nil, fmt.Errorf("could not create database: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("could not stat database file: %w", err)
	default:
		if err = m.open(); err != nil {
			return nil, fmt.Errorf("could not open existing database: %w", err)
		}
	}

	if err := m.addForeignKeyConstraintsToDB(); err != nil {
		return nil, fmt.Errorf("could not add foreign key constraints to database: %w", err)
	}

	if err := m.check(); err != nil {
		return nil, fmt.Errorf("ManagedTokensDatabase failed check: %w", err)
	}

	return &m, nil
}

func (m *ManagedTokensDatabase) create() error {
	if err := m.initialize(); err != nil {
		errMsg := "could not initialize database"
		if err2 := os.Remove(m.filename); !errors.Is(err2, os.ErrNotExist) {
			errMsg = errMsg + ": could not remove corrupt database file.  Please do so manually"
			return fmt.Errorf("%s: %w", errMsg, err2)
		}
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	return nil
}

func (m *ManagedTokensDatabase) open() error {
	var err error
	m.db, err = sql.Open("sqlite3", m.filename)
	if err != nil {
		return fmt.Errorf("could not open database file: %w", &databaseOpenError{m.filename, err})
	}
	return nil
}

func (m *ManagedTokensDatabase) addForeignKeyConstraintsToDB() error {
	pragmaStatement := "PRAGMA foreign_keys = ON;"
	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Adding foreign key constraints to database: %s", pragmaStatement))
	}
	if _, err := m.db.Exec(pragmaStatement); err != nil {
		return fmt.Errorf("could not enable foreign key constraints: %w", err)
	}
	return nil
}

// Close closes the FERRYUIDDatabase
func (m *ManagedTokensDatabase) Close() error {
	return m.db.Close()
}

// check makes sure that an object claiming to be a ManagedTokensDatabase actually is, by checking the ApplicationID
func (m *ManagedTokensDatabase) check() error {
	err := m.checkApplicationId()
	if err != nil {
		return fmt.Errorf("ApplicationId check failed: %w", err)
	}

	// Migrate to the right userVersion of the database
	var userVersion int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return &databaseCheckError{"could not get user_version from ManagedTokensDatabase", err}
	}
	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Supported schema version: %d, user version: %d", schemaVersion, userVersion))
	}
	if userVersion < schemaVersion {
		if err := m.migrate(userVersion, schemaVersion); err != nil {
			return &databaseCheckError{"Error migrating database schema versions", err}
		}
	} else if userVersion > schemaVersion {
		fmt.Println("Database is from a newer version of the Managed Tokens library.  There may have been breaking changes in newer migrations")
	}
	return nil
}

func (m *ManagedTokensDatabase) checkApplicationId() error {
	var dbApplicationId int
	appIDStatement := "PRAGMA application_id"
	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Checking application_id of database: %s", appIDStatement))
	}
	if err := m.db.QueryRow(appIDStatement).Scan(&dbApplicationId); err != nil {
		return &databaseCheckError{"Could not get application_id from ManagedTokensDatabase", err}
	}
	// Make sure our application IDs match
	if dbApplicationId != ApplicationId {
		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
		return &databaseCheckError{errMsg, nil}
	}
	return nil
}

// Main funcs to use internally for interacting with the database

// getValuesTransactionRunner queries a database table and returns a [][]any of the row values requested.  This is the main func to run
// from this library for retrieving data from the database
func getValuesTransactionRunner(ctx context.Context, db *sql.DB, getStatementString string, args ...any) ([][]any, error) {
	ctx, span := tracer.Start(ctx, "getValuesTransactionRunner")
	span.SetAttributes(attribute.String("getStatementString", getStatementString))
	if argSlice, err := dbArgsToStringSlice(args); err == nil {
		span.SetAttributes(attribute.StringSlice("args", argSlice))
	}
	defer span.End()

	data := make([][]any, 0)

	dbTimeout, _, err := contextStore.GetProperTimeout(ctx, dbDefaultTimeoutStr)
	if err != nil {
		err = fmt.Errorf("could not parse db timeout duration: %w", err)
		tracing.LogErrorWithTrace(
			span,
			err,
			tracing.KeyValueForLog{Key: "dbDefaultTimeoutStr", Value: dbDefaultTimeoutStr},
		)
		return data, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	// Query the DB
	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Querying database: %s, %v", getStatementString, args))
	}
	rows, err := db.QueryContext(dbContext, getStatementString, args...)
	if err != nil {
		err = fmt.Errorf("could not query the database: %w", err)
		tracing.LogErrorWithTrace(
			span,
			err,
			tracing.KeyValueForLog{Key: "dbStatement", Value: getStatementString},
		)
		return data, err
	}
	defer rows.Close()

	// Get column names so we can get the length of each row
	cols, err := rows.Columns()
	if err != nil {
		err = fmt.Errorf("could not get columns from query results: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return data, err
	}

	// Scan our rows and add them to data
	for rows.Next() {
		resultRow, resultRowPtrs := prepareDataAndPointerSliceForDBRows(len(cols))
		err := rows.Scan(resultRowPtrs...)
		if err != nil {
			err = fmt.Errorf("could not scan row from database results: %w", err)
			tracing.LogErrorWithTrace(span, err)
			return data, err
		}
		if debugEnabled {
			debugLogger.Debug(fmt.Sprintf("Got row from database: %v", resultRow))
		}
		data = append(data, resultRow)
	}
	if err = rows.Err(); err != nil {
		err = fmt.Errorf("error reading rows from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	tracing.LogSuccessWithTrace(span, "Successfully retrieved data from database")
	return data, nil
}

// getNamedDimensionStringValues queries a table as given in the sqlGetStatement provided that each row returned by the query
// in sqlGetStatement is a single string (one column of string type). An example of a valid query for sqlGetStatement would be
// "SELECT name FROM table".  An invalid query would be "SELECT id, name FROM table".  This is the main func to use to get
// dimension-like data in this library
func getNamedDimensionStringValues(ctx context.Context, db *sql.DB, sqlGetStatement string) ([]string, error) {
	ctx, span := tracer.Start(ctx, "getNamedDimensionStringValues")
	span.SetAttributes(attribute.String("sqlGetStatement", sqlGetStatement))
	defer span.End()

	data, err := getValuesTransactionRunner(ctx, db, sqlGetStatement)
	if err != nil {
		err = fmt.Errorf("could not get values from database transaction: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No values in database")
		return nil, nil
	}

	unpackedData, err := unpackNamedDimensionData(data)
	if err != nil {
		err = fmt.Errorf("could not unpack named dimension data: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	tracing.LogSuccessWithTrace(span, "Successfully retrieved named dimension data from database")
	return unpackedData, nil
}

// insertTransactionRunner inserts data into a database.  Besides the context to be used and the database
// itself, it also takes an insertStatementString string that contains the SQL query to be prepared and
// filled in with the values given by each element of insertData.  This is the main func to use in this library to insert
// values into the database
func insertValuesTransactionRunner(ctx context.Context, db *sql.DB, insertStatementString string, insertData []insertValues) error {
	ctx, span := tracer.Start(ctx, "insertValuesTransactionRunner")
	span.SetAttributes(attribute.String("insertStatementString", insertStatementString))
	defer span.End()

	dbTimeout, _, err := contextStore.GetProperTimeout(ctx, dbDefaultTimeoutStr)
	if err != nil {
		err = fmt.Errorf("could not parse db timeout duration: %w", err)
		tracing.LogErrorWithTrace(
			span,
			err,
			tracing.KeyValueForLog{Key: "dbDefaultTimeoutStr", Value: dbDefaultTimeoutStr},
		)
		return err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	tx, err := db.Begin()
	if err != nil {
		err = fmt.Errorf("could not open transaction to database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	insertStatement, err := tx.Prepare(insertStatementString)
	if err != nil {
		err = fmt.Errorf("could not prepare INSERT statement to database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	defer insertStatement.Close()

	argSlice := make([]string, 0)
	// Run the passed-in insert statement on insertData
	for _, datum := range insertData {
		datumValues := datum.insertValues()

		// This bit is only for tracing
		if datumValueSlice, err := dbArgsToStringSlice(datumValues); err == nil {
			argSlice = append(argSlice, datumValueSlice...)
		}

		if debugEnabled {
			debugLogger.Debug(fmt.Sprintf("Inserting data into database: %s, %v", insertStatementString, datumValues))
		}
		if _, err := insertStatement.ExecContext(dbContext, datumValues...); err != nil {
			err = fmt.Errorf("could not insert data into database: %w", err)
			span.SetAttributes(attribute.StringSlice("args", argSlice))
			tracing.LogErrorWithTrace(span, err)
			return err
		}
	}
	span.SetAttributes(attribute.StringSlice("args", argSlice))

	err = tx.Commit()
	if err != nil {
		err = fmt.Errorf("could not commit transaction to database. Rolling back: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	tracing.LogSuccessWithTrace(span, "Successfully inserted data into database")
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
			return nil, &errDatabaseDataWrongStructure{resultRow, "expected 1 element"}
		}
		val, ok := resultRow[0].(string)
		if !ok {
			return nil, &errDatabaseDataWrongType{resultRow[0], "expected string"}
		}
		dataConverted = append(dataConverted, val)
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
			return nil, fmt.Errorf("could not unpack data row: %v: %w", row, err)
		}
		datumVal, ok := unpackedDatum.(T)
		if !ok {
			return nil, fmt.Errorf("unpackDataRow returned wrong type.  Expected %T", datumVal)
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

// dbArgsToStringSlice converts a slice of any type to a slice of strings.
// Supported types are string, int, float32, and float64.
// If an unsupported type is encountered, it returns an empty string slice and an error.
func dbArgsToStringSlice(args []any) ([]string, error) {
	argsStr := make([]string, len(args))
	for i, arg := range args {
		switch val := arg.(type) {
		case string:
			argsStr[i] = val
		case int:
			argsStr[i] = strconv.Itoa(val)
		case float32:
			argsStr[i] = strconv.FormatFloat(float64(val), 'f', -1, 32)
		case float64:
			argsStr[i] = strconv.FormatFloat(val, 'f', -1, 64)
		default:
			return []string{}, fmt.Errorf("unsupported type %T", arg)
		}
	}
	return argsStr, nil
}

type errDatabaseDataWrongStructure struct {
	value any
	msg   string
}

func (e *errDatabaseDataWrongStructure) Error() string {
	msg := "row has wrong structure"
	if e.msg != "" {
		msg += ": " + e.msg
	}
	msg += fmt.Sprintf(": %v", e.value)
	return msg
}

type errDatabaseDataWrongType struct {
	value       any
	expectedMsg string
}

func (e *errDatabaseDataWrongType) Error() string {
	msg := "datum has wrong type"
	if e.expectedMsg != "" {
		msg += ": " + e.expectedMsg
	}
	msg += fmt.Sprintf(", got %T", e.value)
	return msg
}
