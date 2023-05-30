package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"testing"

	"github.com/shreyb/managed-tokens/internal/testutils"
)

// TestInsertUidsIntoTableFromFERRY checks that we can insert FERRY data into the ManagedTokensDatabase correctly
func TestInsertUidsIntoTableFromFERRY(t *testing.T) {
	type testCase struct {
		description string
		fakeData    []FerryUIDDatum
	}

	testCases := []testCase{
		{
			"Use an empty database with no data stored",
			[]FerryUIDDatum{},
		},
		{
			"Use a database with fake data",
			[]FerryUIDDatum{
				&ferryUidDatum{
					username: "testuser",
					uid:      12345,
				},
				&ferryUidDatum{
					username: "anothertestuser",
					uid:      67890,
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				defer os.Remove(goodDbLocation)

				goodTestDb, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
				}
				defer goodTestDb.Close()

				if err := goodTestDb.InsertUidsIntoTableFromFERRY(context.Background(), test.fakeData); err != nil {
					t.Error("Could not insert fake data into test database")
				}

				retrievedData := make([]FerryUIDDatum, 0, len(test.fakeData))
				rows, err := goodTestDb.db.Query("SELECT username, uid FROM uids")
				if err != nil {
					t.Error("Could not retrieve fake data from test database")
				}
				for rows.Next() {
					var dataUsername string
					var dataUid int
					if err := rows.Scan(&dataUsername, &dataUid); err != nil {
						t.Error("Could not store retrieved test database data locally")
					}
					retrievedData = append(retrievedData, &ferryUidDatum{dataUsername, dataUid})
				}
				if !testutils.SlicesHaveSameElements(
					ferryUIDDatumInterfaceSlicetoStructSlice(test.fakeData),
					ferryUIDDatumInterfaceSlicetoStructSlice(retrievedData),
				) {
					t.Errorf("Expected data and retrieved data do not match.  Expected %v, got %v", test.fakeData, retrievedData)
				}
			},
		)
	}
}

// TestConfirmUIDsInTable checks that ConfirmUIDsInTable returns values from the ManagedTokensDatabase correctly
func TestConfirmUIDsInTable(t *testing.T) {
	type testCase struct {
		description string
		fakeData    []ferryUidDatum
	}

	testCases := []testCase{
		{
			"Use an empty database with no data stored",
			[]ferryUidDatum{},
		},
		{
			"Use a database with fake data",
			[]ferryUidDatum{
				{
					username: "testuser",
					uid:      12345,
				},
				{
					username: "anothertestuser",
					uid:      67890,
				},
			},
		},
	}

	for _, test := range testCases {

		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

		goodTestDb, err := OpenOrCreateDatabase(goodDbLocation)
		if err != nil {
			t.Errorf("Could not create new database, %s", err)
		}
		defer goodTestDb.Close()

		for _, datum := range test.fakeData {
			if _, err := goodTestDb.db.Exec("INSERT into uids (username, uid) VALUES (?,?)", datum.username, datum.uid); err != nil {
				t.Errorf("Could not insert test data into database")
			}
		}

		retrievedData, err := goodTestDb.ConfirmUIDsInTable(context.Background())
		if err != nil {
			t.Error("Could not retrieve fake data from test database")
		}

		if !testutils.SlicesHaveSameElements(
			ferryUIDDatumInterfaceSlicetoStructSlice(retrievedData),
			test.fakeData,
		) {
			t.Errorf("Expected data and retrieved data do not match.  Expected %v, got %v", test.fakeData, retrievedData)
		}
	}
}

// TestGetUIDsByUsername checks that we can retrieve the proper UID given a particular username.
// If we give a user that does not exist, we should get 0 as the result, and an error
func TestGetUIDsByUsername(t *testing.T) {

	type testCase struct {
		description   string
		testUsername  string
		expectedUid   int
		expectedError error
	}

	testCases := []testCase{
		{
			"Get UID for user we know is in the database",
			"testuser",
			12345,
			nil,
		},
		{
			"Attempt to get UID for user who is not in the database",
			"nonexistentuser",
			0,
			sql.ErrNoRows,
		},
	}

	fakeData := []FerryUIDDatum{
		&ferryUidDatum{
			username: "testuser",
			uid:      12345,
		},
		&ferryUidDatum{
			username: "anothertestuser",
			uid:      67890,
		},
	}

	goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	defer os.Remove(goodDbLocation)

	goodTestDb, err := OpenOrCreateDatabase(goodDbLocation)
	if err != nil {
		t.Errorf("Could not create new database, %s", err)
	}
	defer goodTestDb.Close()

	if err := goodTestDb.InsertUidsIntoTableFromFERRY(context.Background(), fakeData); err != nil {
		t.Error("Could not insert fake data into test database")
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				ctx := context.Background()
				retrievedUid, err := goodTestDb.GetUIDByUsername(ctx, test.testUsername)
				if err != nil {
					if test.expectedError != nil {
						if !errors.Is(err, test.expectedError) {
							t.Errorf(
								"Got wrong error.  Expected %v, got %v",
								test.expectedError,
								err,
							)
						}
					} else {
						t.Error("Could not get UID by username")
					}
				}
				if retrievedUid != test.expectedUid {
					t.Errorf(
						"Retrieved UID and expected UID do not match.  Expected %d, got %d",
						test.expectedUid,
						retrievedUid,
					)
				}
			},
		)
	}

}

// ferryUIDDatumInterfaceSlicetoStructSlice is a helper function that converts a slice of
// FerryUIDDatum to a slice of the concrete type used in this package, ferryUidDatum
func ferryUIDDatumInterfaceSlicetoStructSlice(f []FerryUIDDatum) []ferryUidDatum {
	retVal := make([]ferryUidDatum, 0, len(f))
	for _, elt := range f {
		eltVal, ok := elt.(*ferryUidDatum)
		if ok {
			if eltVal != nil {
				retVal = append(retVal, *eltVal)
			}
		}
	}
	return retVal
}
