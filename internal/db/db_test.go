package db

import (
	"fmt"
	"math/rand"
	"os"
	"path"
	"testing"
)

func TestCheckDatabaseApplicationId(t *testing.T) {}

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

		// TODO Uncomment this when we implement migrations for schema
		// if err = checkSchema(goodTestDb); err != nil {
		// 	t.Error("Schema check failed")

		// }
	}()

	// Test that we can reopen the db
	goodTestDb, err := OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Errorf("Could not open previously-created database, %s", err)
	}
	goodTestDb.Close()
}

// TODO Implement this when migration stuff is done
// checkSchema is a testing utility function to make sure that a test ManagedTokensDatabase has the right schema
func checkSchema(f *ManagedTokensDatabase) error {
	// var schema string
	// if err := f.db.QueryRow("SELECT sql FROM sqlite_master;").Scan(&schema); err != nil {
	// 	return err
	// }
	// // TODO:  Pull this as a concatenation of all the migrations when that's implemented
	// expectedSchema := strings.TrimRight(strings.TrimSpace(createUIDTableStatement), ";")
	// if schema != expectedSchema {
	// 	return fmt.Errorf(
	// 		"Schema for database does not match expected schema.  Expected %s, got %s",
	// 		expectedSchema,
	// 		schema,
	// 	)
	// }
	return nil
}
