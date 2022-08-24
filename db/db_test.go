package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestOpenOrCreateDatabase checks that we can create and reopen a new FERRYUIDDatabase
func TestOpenOrCreateDatabase(t *testing.T) {
	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(dbLocation)

	// Test that we can create a new db at a new location
	func() {
		goodTestDb, err := OpenOrCreateDatabase(dbLocation)
		if err != nil {
			t.Errorf("Could not create new database, %s", err)
		}
		defer goodTestDb.Close()

		if err = checkSchema(goodTestDb); err != nil {
			t.Error("Schema check failed")

		}
	}()

	// Test that we can reopen the db
	goodTestDb, err := OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Errorf("Could not open previously-created database, %s", err)
	}
	goodTestDb.Close()
}

// TestOpenOrCreateDatabaseCheckError creates a file that will fail the
// FERRYUIDDatabase check.  We want to make sure we get the proper returned *ferryUIDDatabaseCheckError
// This call to OpenOrCreateDatabase should return an error because we create a tempfile, and because it exists,
// OpenOrCreateDatabase never runs initialize() and assigns the ApplicationId
func TestOpenOrCreateDatabaseCheckError(t *testing.T) {
	var checkError *ferryUIDDatabaseCheckError
	dbLocation, err := os.CreateTemp(os.TempDir(), "managed-tokens")
	if err != nil {
		t.Error("Could not create temp file for test database")
	}
	defer os.Remove(dbLocation.Name())

	badTestDb, err := OpenOrCreateDatabase(dbLocation.Name())
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

// TestInitialize makes sure that the *(FERRYUIDDatabase).initialize func returns a *FERRYUIDDatabase object with the
// correct ApplicationId and schema
func TestInitialize(t *testing.T) {
	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(dbLocation)

	f := &FERRYUIDDatabase{filename: dbLocation}
	if err := f.initialize(); err != nil {
		t.Error("Could not initialize FERRYUIDDatabase")
	}
	defer f.Close()
	if err := f.check(); err != nil {
		t.Error("initialized FERRYUIDDatabase has the wrong ApplicationId")
	}
	if err := checkSchema(f); err != nil {
		t.Error("initialized FERRYUIDDatabase failed schema check")
	}
}

// TestCheckGood makes sure that the *(FERRYUIDDatabase).check() method performs the proper check, and when given a proper database, returns
// a nil error
func TestCheckGood(t *testing.T) {
	goodDbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(goodDbLocation)

	goodTestDb := &FERRYUIDDatabase{filename: goodDbLocation}
	if err := goodTestDb.initialize(); err != nil {
		t.Error("Could not initialize FERRYUIDDatabase")
	}
	defer goodTestDb.Close()

	if err := goodTestDb.check(); err != nil {
		t.Errorf("FERRYUIDDatabase failed check. %s", err)
	}
}

// TestCheckBad makes sure that the *(FERRYUIDDatabase).check() method performs the proper check, and when given an improper database, returns
// an error indicating that the ApplicationIds do not match
func TestCheckBad(t *testing.T) {
	var checkError = fmt.Errorf("Application IDs do not match.  Got 0, expected %d", ApplicationId)
	badDbLocation, err := os.CreateTemp(os.TempDir(), "managed-tokens")
	if err != nil {
		t.Error("Could not create temp file for test database")
	}
	defer os.Remove(badDbLocation.Name())

	badTestDb := &FERRYUIDDatabase{filename: badDbLocation.Name()}
	badTestDb.db, err = sql.Open("sqlite3", badDbLocation.Name())
	if err != nil {
		t.Error("Could not open the UID database file")
	}
	defer badTestDb.Close()

	if err := badTestDb.check(); err.Error() != checkError.Error() {
		t.Errorf(
			"Got unexpected error from check.  Expected %s, got %s",
			checkError,
			err,
		)
	}
}

// TestCreateUidTables checks that *(FERRYUIDDatabase).createUidsTable creates the proper UID table in the database by checking the schema
func TestCreateUidsTable(t *testing.T) {
	var err error
	goodDbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(goodDbLocation)

	goodTestDb := &FERRYUIDDatabase{filename: goodDbLocation}
	goodTestDb.db, err = sql.Open("sqlite3", goodDbLocation)
	if err != nil {
		t.Error("Could not open the UID database file")
	}
	defer goodTestDb.Close()

	if err := goodTestDb.createUidsTable(); err != nil {
		t.Error("Could not create UIDs table in FERRYUIDDatabase")
	}
	if err := checkSchema(goodTestDb); err != nil {
		t.Error("Test FERRYUIDDatabase has wrong schema")
	}
}

// Checks needed:
// InsertUidsIntoTableFromFERRY(make some fake data, insert it into dummy db, make sure it's properly there)
// ConfirmUIDsInTable(Give it an empty Db, make sure we have nothing; then give it a good DB, and make sure we have something)
// GetUIDsByUsername(Give us a good DB, retrieve by username)

// checkSchema is a testing utility function to make sure that a test FERRYUIDDatabase has the right schema
func checkSchema(f *FERRYUIDDatabase) error {
	var schema string
	if err := f.db.QueryRow("SELECT sql FROM sqlite_master;").Scan(&schema); err != nil {
		return err
	}
	expectedSchema := strings.TrimRight(strings.TrimSpace(createUIDTableStatement), ";")
	if schema != expectedSchema {
		return fmt.Errorf(
			"Schema for database does not match expected schema.  Expected %s, got %s",
			expectedSchema,
			schema,
		)
	}
	return nil
}
