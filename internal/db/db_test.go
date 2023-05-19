package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/shreyb/managed-tokens/internal/testutils"
)

// TestOpenOrCreateDatabase checks that we can create and reopen a new ManagedTokensDatabase
func TestOpenOrCreateDatabase(t *testing.T) {
	dbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(dbLocation)

	// Test that we can create a new db at a new location
	func() {
		goodTestDb, err := OpenOrCreateDatabase(dbLocation)
		if err != nil {
			t.Errorf("Could not create new database, %s", err)
		}
		defer goodTestDb.Close()

		if err = checkSchema(goodTestDb); err != nil {
			t.Errorf("Schema check failed: %v", err)
		}
	}()

	// Test that we can reopen the db
	goodTestDb, err := OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Errorf("Could not open previously-created database, %s", err)
	}
	goodTestDb.Close()
}

// TestCheckDatabaseBadApplicationId checks that if we open a database with the wrong ApplicationId, we get the correct error type
func TestCheckDatabaseBadApplicationId(t *testing.T) {
	dbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(dbLocation)
	m := &ManagedTokensDatabase{
		filename: dbLocation,
	}

	var err error
	if m.db, err = sql.Open("sqlite3", dbLocation); err != nil {
		t.Error(err)
	}
	defer m.Close()

	// Set a fake application ID
	if _, err = m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", 42)); err != nil {
		t.Error(err)
	}

	var e *databaseCheckError
	if err = m.check(); err == nil {
		t.Error("Expected application ID check to fail.  Got nil error instead")
	} else if !errors.As(err, &e) {
		t.Errorf("Got wrong error type from application ID check.  Expected *databaseCheckError, got %T instead", err)
	}

}

// TODO Expand this test so we try prepared statements
// TestGetValuesTransactionRunner inserts values into a test database table and makes sure that getValuesTransactionRunner properly
// returns those values
func TestGetValuesTransactionRunner(t *testing.T) {
	// Create fake DB, check that this func works
	m, err := createAndOpenTestDatabaseWithApplicationId()
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		m.db.Close()
		os.Remove(m.filename)
	}()

	// Set up test data
	type expectedData struct {
		id       int
		fakeName string
	}
	type testCase struct {
		description string
		insertFunc  func(id int, fakeName string) error
		expectedData
	}

	testCases := []testCase{
		{
			"No data inserted",
			func(id int, fakeName string) error { return nil },
			expectedData{},
		},
		{
			"One row of data inserted",
			func(id int, fakeName string) error {
				_, err = m.db.Exec("INSERT INTO test_table VALUES (?, ?)", id, fakeName)
				return err
			},
			expectedData{
				id:       12345,
				fakeName: "foobar",
			},
		},
	}
	for _, test := range testCases {
		if _, err = m.db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, fake_name STRING NOT NULL)"); err != nil {
			t.Error("Could not create table for TestGetValuesTransactionRunner test")
			return
		}
		// Insert our test data
		if err = test.insertFunc(test.expectedData.id, test.expectedData.fakeName); err != nil {
			t.Errorf("Failed to run insert func for TestGetValuesTransactionRunner: %s: %s", test.description, err)
			return
		}

		// The actual test
		ctx := context.Background()
		testQuery := "SELECT id, fake_name FROM test_table"
		data, err := getValuesTransactionRunner(ctx, m.db, testQuery)
		if err != nil {
			t.Errorf("Could not obtain values from database for TestGetValuesTransactionRunner: %s: %s", test.description, err)
			return
		}
		var noExpectedData expectedData
		if test.expectedData == noExpectedData {
			if len(data) != 0 {
				t.Errorf("Should not have gotten any data back.  Got %v", data)
			}
			return
		}
		if dataId, ok := data[0][0].(int64); !ok {
			t.Errorf("Got wrong data type for id value.  Expected int, got %T", dataId)
		} else {
			if int(dataId) != test.expectedData.id {
				t.Errorf("Returned id value does not match expected id value.  Expected %d, got %d", test.expectedData.id, dataId)
			}
		}
		if dataFakeName, ok := data[0][1].(string); !ok {
			t.Errorf("Got wrong data type for fake_name value.  Expected string, got %T", dataFakeName)
		} else {
			if dataFakeName != test.expectedData.fakeName {
				t.Errorf("Returned fake_name value does not match expected fake_name value.  Expected %s, got %s", test.expectedData.fakeName, dataFakeName)
			}
		}
	}

}

type fakeDatum struct {
	id       int
	fakeName string
}

func (f *fakeDatum) values() []any { return []any{f.id, f.fakeName} }

// TestInsertValuesTransactionRunner checks that insertValuesTransactionRunner properly inserts values into a test database
func TestInsertValuesTransactionRunner(t *testing.T) {
	// Create fake DB, try to insert values, check that we get the right values back
	m, err := createAndOpenTestDatabaseWithApplicationId()
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		m.db.Close()
		os.Remove(m.filename)
	}()

	// Set up test data
	expectedId := 12345
	expectedFakeName := "foobar"
	if _, err = m.db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, fake_name STRING NOT NULL)"); err != nil {
		t.Error("Could not create table for TestInsertTransactionRunner test")
		return
	}

	// The actual test
	ctx := context.Background()
	insertStatementString := "INSERT INTO test_table VALUES (?, ?)"
	if err = insertValuesTransactionRunner(ctx, m.db, insertStatementString, []insertValues{&fakeDatum{expectedId, expectedFakeName}}); err != nil {
		t.Errorf("Could not insert values into database for TestInsertTransactionRunner test")
		return
	}
	getDataBackQuery := "SELECT id, fake_name FROM test_table"
	var dataId int
	var dataFakeName string
	if err := m.db.QueryRowContext(ctx, getDataBackQuery).Scan(&dataId, &dataFakeName); err != nil {
		t.Errorf("Could not retrieve test data from database for TestInsertTransactionRunner test")
	}
	if dataId != expectedId {
		t.Errorf("Returned id value does not match expected id value.  Expected %d, got %d", expectedId, dataId)
	}
	if dataFakeName != expectedFakeName {
		t.Errorf("Returned fake_name value does not match expected fake_name value.  Expected %s, got %s", expectedFakeName, dataFakeName)
	}
}

// checkSchema is a testing utility function to make sure that a test ManagedTokensDatabase that has been initialized has the right schema
func checkSchema(m *ManagedTokensDatabase) error {
	schemaRows := make([]string, 0)
	rows, err := m.db.Query("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL;")
	if err != nil {
		return errors.New("could not get schema from database")
	}
	defer rows.Close()

	for rows.Next() {
		var schemaRow string
		err = rows.Scan(&schemaRow)
		if err != nil {
			return fmt.Errorf("could not scan schema rows from database: %w", err)
		}
		schemaRows = append(schemaRows, standardizeSpaces(strings.TrimSpace(schemaRow)))
	}
	migrationsSql := make([]string, 0, len(migrations))
	for _, migrationPiece := range migrations {
		sql := migrationPiece.sqlText
		sqlSlice := strings.Split(sql, ";")

		for _, sqlElt := range sqlSlice {
			if !strings.Contains(sqlElt, "PRAGMA") && len(sqlElt) != 0 {
				migrationsSql = append(migrationsSql, standardizeSpaces(strings.TrimSpace(sqlElt)))
			}
		}
	}
	if !testutils.SlicesHaveSameElements(schemaRows, migrationsSql) {
		return fmt.Errorf(
			"Schema for database does not match expected schema.  Expected %s, got %s.",
			migrationsSql,
			schemaRows,
		)
	}
	return nil
}

// standardizeSpaces is a simple utility function to reprint any string with only a single space character separating.  This
// helps to compare two strings that have the same text, but different spacing schemes/indents/newlines, etc.
// For example:
// standardizeSpaces("blah  blah blahblah") = standardizeSpaces("blah blah      blahblah ")
// Thanks to https://stackoverflow.com/a/42251527
func standardizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// createAndOpenTestDatabaseWithApplicationId creates a test ManagedTokensDatabase object at a random location in os.TempDir,
// sets the application ID, and returns a pointer to the ManagedTokensDatabase, along with any error in creating that object
func createAndOpenTestDatabaseWithApplicationId() (*ManagedTokensDatabase, error) {
	dbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	m := &ManagedTokensDatabase{
		filename: dbLocation,
	}

	var err error
	if m.db, err = sql.Open("sqlite3", dbLocation); err != nil {
		return nil, err
	}

	// Set the application ID
	if _, err = m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		return nil, err
	}
	return m, nil
}
