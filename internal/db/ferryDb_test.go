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

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"slices"
	"testing"

	"github.com/shreyb/managed-tokens/internal/testUtils"
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

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))

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
				if !testUtils.SlicesHaveSameElements(
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

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
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

				if !testUtils.SlicesHaveSameElements(
					ferryUIDDatumInterfaceSlicetoStructSlice(retrievedData),
					test.fakeData,
				) {
					t.Errorf("Expected data and retrieved data do not match.  Expected %v, got %v", test.fakeData, retrievedData)
				}
			},
		)
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

	goodDbLocation := path.Join(t.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
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

func TestUnpackUIDDataRow(t *testing.T) {
	type testCase struct {
		description    string
		resultRow      []any
		expectedResult *ferryUidDatum
		expectedErr    error
	}

	testCases := []testCase{
		{
			"Valid data",
			[]any{
				"string",
				int64(42),
			},
			&ferryUidDatum{
				"string",
				42,
			},
			nil,
		},
		{
			"Invalid data - wrong structure",
			[]any{
				"string",
				int64(42),
				int64(43),
			},
			nil,
			errDatabaseDataWrongStructure,
		},
		{
			"Invalid data - wrong types",
			[]any{
				"string",
				"string2",
			},
			nil,
			errDatabaseDataWrongType,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				var u ferryUidDatum
				datum, err := u.unpackDataRow(test.resultRow)
				if test.expectedErr == nil && err != nil {
					t.Errorf("Expected nil error.  Got %s instead", err)
					return
				}
				testErrors := []error{errDatabaseDataWrongStructure, errDatabaseDataWrongType}
				for _, testError := range testErrors {
					if errors.Is(test.expectedErr, testError) {
						if !errors.Is(err, testError) {
							t.Errorf("Got wrong error.  Expected %v, got %v", test.expectedErr, err)
							return
						}
						break
					}
				}

				if (test.expectedResult == nil && datum != nil) ||
					(test.expectedResult != nil && datum == nil) {
					t.Errorf("Got wrong result.  Expected %v, got %v", test.expectedResult, datum)
				}

				if test.expectedResult != nil && datum != nil {
					datumValue, ok := datum.(*ferryUidDatum)
					if !ok {
						t.Errorf("Got wrong type in result.  Expected %T, got %T", *test.expectedResult, datum)
					}
					if *datumValue != *test.expectedResult {
						t.Errorf("Got wrong result.  Expected %v, got %v", test.expectedResult, datumValue)
					}
				}
			},
		)
	}
}

func TestFerryUIDDatumInterfaceSlicetoInsertValuesSlice(t *testing.T) {
	type testCase struct {
		description  string
		inputData    []FerryUIDDatum
		expectedData []insertValues
	}

	testCases := []testCase{
		{
			"non-zero length slice",
			[]FerryUIDDatum{
				&ferryUidDatum{"foo", 1},
				&ferryUidDatum{"bar", 2},
			},
			[]insertValues{
				&ferryUidDatum{"foo", 1},
				&ferryUidDatum{"bar", 2},
			},
		},
		{
			"zero length slice",
			[]FerryUIDDatum{},
			[]insertValues{},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := ferryUIDDatumInterfaceSlicetoInsertValuesSlice(test.inputData)
				if !slices.EqualFunc[[]insertValues, []insertValues, insertValues, insertValues](
					result,
					test.expectedData,
					func(resultElt insertValues, expectedElt insertValues) bool {
						if resultElt == nil && expectedElt == nil {
							return true
						}
						if expectedElt != nil {
							if resultElt == nil {
								t.Errorf("Got nil for result, but expected %v", expectedElt)
								return false
							}

							expectedEltVal, _ := expectedElt.(*ferryUidDatum)
							resultEltVal, ok := resultElt.(*ferryUidDatum)
							if !ok {
								t.Errorf("Got wrong type in result.  Expected *ferryUidDatum, got %T", resultElt)
							}

							if *expectedEltVal != *resultEltVal {
								t.Errorf("Got wrong result.  Expected %v, got %v", *expectedEltVal, *resultEltVal)
								return false
							}
						}
						return true
					},
				) {
					t.Errorf("Got wrong result.  Expected %v, got %v", test.expectedData, result)
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
