package db

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"
)

var goodTestDb *FERRYUIDDatabase
var badTestDb *FERRYUIDDatabase

// TestOpenOrCreateDatabase checks that we can create and reopen a new FERRYUIDDatabase
func TestOpenOrCreateDatabase(t *testing.T) {
	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(dbLocation)

	// Test that we can create a new db at a new location
	goodTestDb, err := OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Errorf("Could not create new database, %s", err)
	}

	// Test that the new db has the right schema
	var schema string
	if err = goodTestDb.db.QueryRow("SELECT sql FROM sqlite_master;").Scan(&schema); err != nil {
		t.Errorf("Could not query schema, %s", err)
	}
	expectedSchema := strings.TrimRight(strings.TrimSpace(createUIDTableStatement), ";")
	if schema != expectedSchema {
		t.Errorf(
			"Schema for database does not match expected schema.  Expected %s, got %s",
			expectedSchema,
			schema,
		)
	}

	goodTestDb.Close()

	// Test that we can reopen the db
	goodTestDb, err = OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Errorf("Could not open previously-created database, %s", err)
	}
	goodTestDb.Close()
}

// Test that if we purposefully try to create a db where we can't write a file, we get a ferryUIDDatabaseCreateError
// TODO See if we can make the DB fail a check error

// TestOpenOrCreateDatabaseCheckError creates a file that will fail the
// FERRYUIDDatabase check.  We want to make sure we get the proper returned *ferryUIDDatabaseCheckError
func TestOpenOrCreateDatabaseCheckError(t *testing.T) {
	var checkError *ferryUIDDatabaseCheckError
	dbLocation, err := os.CreateTemp(os.TempDir(), "managed-tokens")
	if err != nil {
		t.Error("Could not create temp file for test database")
	}
	defer os.Remove(dbLocation.Name())

	badTestDb, err = OpenOrCreateDatabase(dbLocation.Name())
	if !errors.As(err, &checkError) {
		t.Errorf(
			"Returned error from OpenOrCreateDatabase is of wrong type.  Expected %T, got %T",
			checkError,
			err,
		)
	} else {
		badTestDb.Close()
	}

}

// Checks needed:
// initialize (use dummy file, make sure we get good app ID with check, good schema)
// check (use dummy file, make one pass, one fail on purpose)
// createUidTablesInDB (create the tables, check schema)
// InsertUidsIntoTableFromFERRY(make some fake data, insert it into dummy db, make sure it's properly there)
// ConfirmUIDsInTable(Give it an empty Db, make sure we have nothing; then give it a good DB, and make sure we have something)
// GetUIDsByUsername(Give us a good DB, retrieve by username)
