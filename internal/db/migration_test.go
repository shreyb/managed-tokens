package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TODO Parametrize theses tests with t.Run() calls.  Also, remove databases using t.TempDir()

// TestCantOpen makes sure that an invalid DB file location fails to get opened
func TestCantOpen(t *testing.T) {
	goodDbLocation := path.Join(os.DevNull, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(goodDbLocation)

	m := &ManagedTokensDatabase{filename: goodDbLocation}
	defer m.Close()

	if err := m.initialize(); err == nil {
		t.Error("File should have not been able to be opened")
	}
}

// TestInitialize checks that if we give valid and invalid filenames, that initialize() displays the correct behavior or
// returns the correct error
func TestInitialize(t *testing.T) {
	type testCase struct {
		description string
		filename    string
		expectedErr error
	}

	testCases := []testCase{
		{
			"Valid file location",
			path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000))),
			nil,
		},
		{
			"Invalid file location",
			path.Join(os.DevNull, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000))),
			&databaseCreateError{},
		},
	}

	for _, test := range testCases {
		defer os.Remove(test.filename)
		m := &ManagedTokensDatabase{filename: test.filename}
		defer m.Close()

		err := m.initialize()
		if test.expectedErr == nil {
			if err != nil {
				t.Errorf("Expected nil error from initializing in test %s.  Got %s", test.description, err)
			}
		} else {
			var e1 *databaseOpenError
			var e2 *databaseCreateError
			var e3 *databaseMigrateError
			switch {
			case errors.As(test.expectedErr, &e1):
				if !errors.As(err, &e1) {
					t.Errorf("Got wrong error type.  Expected %T, got %T", e1, err)
				}
			case errors.As(test.expectedErr, &e2):
				if !errors.As(err, &e2) {
					t.Errorf("Got wrong error type.  Expected %T, got %T", e2, err)
				}
			case errors.As(test.expectedErr, &e3):
				if !errors.As(err, &e3) {
					t.Errorf("Got wrong error type.  Expected %T, got %T", e3, err)
				}
			}
		}
	}
}

// TestMigrationFromHigherSchemaVersions checks that if we open a database with a higher schema version than schemaVersion, we
// leave the database alone
func TestMigrationFromHigherSchemaVersions(t *testing.T) {
	// Check user version by opening a DB, seeing if we can mock that it's on lower, higher, and correct versions
	m, err := createAndOpenTestDatabaseWithApplicationId()
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		m.db.Close()
		os.Remove(m.filename)
	}()

	// Higher version than schemaVersion - so the schema should remain empty
	higherVersion := schemaVersion + 1
	msg := "Error running test where the database version number is higher than the schemaVersion"
	if _, err = m.db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", higherVersion)); err != nil {
		t.Errorf("%s: %s", msg, err)
	}
	if err := m.migrate(higherVersion, schemaVersion); err != nil {
		t.Errorf("%s: %s", msg, err)
	}

	var s string
	if err := m.db.QueryRow("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL;").Scan(&s); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Schema should be empty in the test where our database version number for an empty database is higher than the schemaVersion.  Got %s", s)
	}
}

// TestMigrationFromLowerSchemaVersions checks that if we open a database with a lower schema version than schemaVersion, we
// migrate the database to the current schemaVersion
func TestMigrationFromLowerSchemaVersion(t *testing.T) {
	// Check user version by opening a DB, seeing if we can mock that it's on lower, higher, and correct versions
	m, err := createAndOpenTestDatabaseWithApplicationId()
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		m.db.Close()
		os.Remove(m.filename)
	}()
	// Lower version than schemaVersion - so our test DB should match the migrations
	lowerVersion := schemaVersion - 1
	msg := "error running test where the database version number is lower than the schemaVersion"
	if _, err = m.db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", lowerVersion)); err != nil {
		t.Errorf("%s: %s", msg, err)
	}
	if err := m.migrate(lowerVersion, schemaVersion); err != nil {
		t.Errorf("%s: %s", msg, err)
	}
	if err := checkSchema(m); err != nil {
		t.Errorf("Schema check failed: %s", err)
	}
}

// TestMigrate checks that migrate properly handles the various migration possibilities that are not covered in
// TestMigrationFromLowerSchemaVersion and TestMigrationFromHigherSchemaVersion.
func TestMigrate(t *testing.T) {
	lenMigrations := len(migrations)
	type testCase struct {
		description string
		from        int
		to          int
		expectedErr error
	}
	testCases := []testCase{
		{
			"from and to are the same, but not too high - nothing should happen here",
			0,
			0,
			nil,
		},
		{
			"from and to are the same, but too high.  Should get databaseMigrateError",
			lenMigrations + 1,
			lenMigrations + 1,
			&databaseMigrateError{},
		},
		{
			"from and to are different, but to is too high.  Should get databaseMigrateError",
			0,
			lenMigrations + 1,
			&databaseMigrateError{},
		},
	}

	for _, test := range testCases {
		m, err := createAndOpenTestDatabaseWithApplicationId()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			m.db.Close()
			os.Remove(m.filename)
		}()

		err = m.migrate(test.from, test.to)
		if test.expectedErr == nil {
			if err != nil {
				t.Errorf("Expected nil error for test %s.  Got %s", test.description, err)
			}
		} else {
			var e1 *databaseMigrateError
			if errors.As(test.expectedErr, &e1) {
				if !errors.As(err, &e1) {
					t.Errorf("Got wrong type of error for test %s.  Expected %T, got %T", test.description, e1, err)
				}
			}
		}
	}
}
