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
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fermitools/managed-tokens/internal/testUtils"
)

// TestOpenOrCreateDatabase checks that we can create and reopen a new ManagedTokensDatabase
func TestOpenOrCreateDatabase(t *testing.T) {
	tempDir := t.TempDir()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))

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

func TestCreateManagedTokensDatabase(t *testing.T) {
	tempDir := t.TempDir()
	goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))

	type testCase struct {
		description string
		dbLocation  string
		expectedErr error
	}

	testCases := []testCase{
		{
			"OK location",
			goodDbLocation,
			nil,
		},
		{
			"devnull - should fail",
			os.DevNull,
			&databaseCreateError{},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				m := &ManagedTokensDatabase{filename: test.dbLocation}
				if err := m.create(); err != nil {
					if test.expectedErr == nil {
						t.Errorf("Expected nil error from initializing in test %s.  Got %s", test.description, err)
					}
					var e1 *databaseOpenError
					if errors.As(test.expectedErr, &e1) && !errors.As(err, &e1) {
						t.Errorf("Got wrong error type.  Expected %T, got %T", e1, err)
					}
				} else {
					defer m.Close()

					if err := checkSchema(m); err != nil {
						t.Errorf("Schema check failed for test %s: %v", test.description, err)
					}
				}
			},
		)
	}
}

func TestOpenManagedTokensDatabase(t *testing.T) {
	tempDir := t.TempDir()
	goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))

	type testCase struct {
		description string
		dbLocation  string
		expectedErr error
	}

	testCases := []testCase{
		{
			"OK location",
			goodDbLocation,
			nil,
		},
		{
			"devnull - should fail",
			os.DevNull,
			&databaseCreateError{},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				m := &ManagedTokensDatabase{filename: test.dbLocation}
				if err := m.open(); err != nil {
					if test.expectedErr == nil {
						t.Errorf("Expected nil error from opening DB file in test %s.  Got %s", test.description, err)
					}
					var e1 *databaseOpenError
					if errors.As(test.expectedErr, &e1) && !errors.As(err, &e1) {
						t.Errorf("Got wrong error type.  Expected %T, got %T", e1, err)
					}
				}
				m.Close()
			},
		)
	}
}

func TestAddForeignKeyConstraintsToDB(t *testing.T) {
	tempDir := t.TempDir()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	m := &ManagedTokensDatabase{filename: dbLocation}

	var err error
	if m.db, err = sql.Open("sqlite3", dbLocation); err != nil {
		t.Error(err)
	}
	defer m.Close()

	if err = m.addForeignKeyConstraintsToDB(); err != nil {
		t.Error(err)
	}

	var foreignKeysSetting int
	if err := m.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeysSetting); err != nil {
		t.Error(err)
	}
	if foreignKeysSetting != 1 {
		t.Error("Expected PRAGMA foreign_keys to be 1.  Got 0 instead")
	}
}

// TestCheckDatabaseBadApplicationId checks that if we open a database with the wrong ApplicationId, we get the correct error type
func TestCheckDatabaseBadApplicationId(t *testing.T) {
	tempDir := t.TempDir()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
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

// TestGetValuesTransactionRunner inserts values into a test database table and makes sure that getValuesTransactionRunner properly
// returns those values
func TestGetValuesTransactionRunner(t *testing.T) {
	// Set up test data
	type expectedDatum struct {
		id       int
		fakeName string
	}
	type testCase struct {
		description     string
		insertFunc      func(id int, fakeName string, m *ManagedTokensDatabase) error
		expectedResults []expectedDatum
	}

	testCases := []testCase{
		{
			"No data inserted",
			func(id int, fakeName string, m *ManagedTokensDatabase) error { return nil },
			[]expectedDatum{},
		},
		{
			"One row of data inserted",
			func(id int, fakeName string, m *ManagedTokensDatabase) error {
				_, err := m.db.Exec("INSERT INTO test_table VALUES (?, ?)", id, fakeName)
				return err
			},
			[]expectedDatum{
				{
					id:       12345,
					fakeName: "foobar",
				},
			},
		},
		{
			"Multiple rows of data inserted",
			func(id int, fakeName string, m *ManagedTokensDatabase) error {
				_, err := m.db.Exec("INSERT INTO test_table VALUES (?, ?)", id, fakeName)
				return err
			},
			[]expectedDatum{
				{
					id:       12345,
					fakeName: "foobar",
				},
				{
					id:       23456,
					fakeName: "noway",
				},
				{
					id:       34567,
					fakeName: "barbaz",
				},
			},
		}}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				m, err := createAndOpenTestDatabaseWithApplicationId(tempDir)
				if err != nil {
					t.Error(err)
					return
				}
				defer m.db.Close()
				if _, err = m.db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, fake_name STRING NOT NULL)"); err != nil {
					t.Error("Could not create table for TestGetValuesTransactionRunner test")
					return
				}
				// Insert our test data
				for _, d := range test.expectedResults {
					if err = test.insertFunc(d.id, d.fakeName, m); err != nil {
						t.Errorf("Failed to run insert func for TestGetValuesTransactionRunner: %s: %s", test.description, err)
						return
					}
				}

				// The actual test
				ctx := context.Background()
				testQuery := "SELECT id, fake_name FROM test_table"
				data, err := getValuesTransactionRunner(ctx, m.db, testQuery)
				if err != nil {
					t.Errorf("Could not obtain values from database for TestGetValuesTransactionRunner: %s: %s", test.description, err)
					return
				}
				transformedData := make([]expectedDatum, 0, len(data))
				for _, datum := range data {
					idVal := datum[0].(int64)
					fakeNameVal := datum[1].(string)
					transformedData = append(transformedData, expectedDatum{int(idVal), fakeNameVal})
				}

				if !slices.Equal[[]expectedDatum, expectedDatum](transformedData, test.expectedResults) {
					t.Errorf("Data and expectedResults do not match.  Expected %v, got %v", test.expectedResults, transformedData)
				}
			},
		)
	}
}

func TestPrepareDataAndPointerSliceForDBRows(t *testing.T) {
	type expectedResultUnit struct {
		values   []any
		pointers []any
	}
	type testCase struct {
		length int
		expectedResultUnit
	}

	testCases := []testCase{
		{
			0,
			expectedResultUnit{
				[]any{},
				[]any{},
			},
		},
		{
			1,
			expectedResultUnit{
				[]any{nil},
				[]any{nil},
			},
		},
		{
			2,
			expectedResultUnit{
				[]any{nil, nil},
				[]any{nil, nil},
			},
		}}

	for _, test := range testCases {
		t.Run(
			strconv.Itoa(test.length),
			func(t *testing.T) {
				values, ptrs := prepareDataAndPointerSliceForDBRows(test.length)
				// Make sure that we get the right set of slices
				if !slices.Equal[[]any, any](values, test.expectedResultUnit.values) {
					t.Errorf("Got wrong values.  Expected %v, got %v", test.expectedResultUnit.values, values)
				}
				// Make sure that the ptrs slice is actually a slice of pointers to values elements
				if len(values) > 0 {
					testValue := any(42)
					ptrTest := ptrs[0].(*any)
					*ptrTest = testValue
					if values[0] != testValue {
						t.Errorf("Pointers slices does not properly point to values slice.  Expected the value %d to be stored.  Got %v instead", testValue, values[0])
					}
				}
			},
		)
	}
}

type fakeDatum struct {
	id       int
	fakeName string
}

func (f *fakeDatum) insertValues() []any { return []any{f.id, f.fakeName} }

// TestInsertValuesTransactionRunner checks that insertValuesTransactionRunner properly inserts values into a test database
func TestInsertValuesTransactionRunner(t *testing.T) {
	// Create fake DB, try to insert values, check that we get the right values back
	tempDir := t.TempDir()
	m, err := createAndOpenTestDatabaseWithApplicationId(tempDir)
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
	if !testUtils.SlicesHaveSameElementsOrderedType[string](schemaRows, migrationsSql) {
		return fmt.Errorf(
			"Schema for database does not match expected schema.  Expected %s, got %s.",
			migrationsSql,
			schemaRows,
		)
	}
	return nil
}

type testDataUnpacker struct {
	data any
}

func (t *testDataUnpacker) unpackDataRow(resultRow []any) (dataRowUnpacker, error) {
	return &testDataUnpacker{data: resultRow}, nil
}

func TestUnpackData(t *testing.T) {
	type testCase struct {
		description string
		data        [][]any
	}

	testCases := []testCase{
		{
			"3-tuple of data",
			[][]any{
				{
					"foo",
					"bar",
					"123",
				},
				{
					"foo",
					"bar",
					"345",
				},
			},
		},
		{
			"2-tuple of data",
			[][]any{
				{
					"foo",
					"123",
				},
			},
		},
		{
			"empty",
			[][]any{
				{},
			},
		},
		{
			"Shouldn't happen, but mixed n-tuples of data",
			[][]any{
				{
					"foo",
					"123",
				},
				{
					"foo",
					"bar",
					"345",
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				expectedData := make([]testDataUnpacker, 0, len(test.data))
				for _, datum := range test.data {
					expectedData = append(expectedData, testDataUnpacker{datum})
				}

				unpackedDataPtrs, err := unpackData[*testDataUnpacker](test.data)
				if err != nil {
					t.Errorf("Unexpected error, %v", err)
				}
				unpackedData := make([]testDataUnpacker, 0, len(unpackedDataPtrs))
				for _, elt := range unpackedDataPtrs {
					unpackedData = append(unpackedData, *elt)
				}

				if !slices.EqualFunc[[]testDataUnpacker, []testDataUnpacker, testDataUnpacker](
					expectedData,
					unpackedData,
					func(t1 testDataUnpacker, t2 testDataUnpacker) bool {
						t1Slice, ok1 := t1.data.([]any)
						t2Slice, ok2 := t2.data.([]any)
						ok := ok1 && ok2
						if !ok {
							t.Errorf("One of the testDataUnpackers does not contain a slice")
							return false
						}
						if !slices.Equal(t1Slice, t2Slice) {
							t.Errorf("Got wrong result.  Expected %v, got %v", t1Slice, t2Slice)
							return false
						}
						return true

					},
				) {
					t.Errorf("Got wrong result.  Expected %v, got %v", expectedData, unpackedData)
				}

			},
		)
	}
}

func TestCheckApplicationId(t *testing.T) {
	tempDir := t.TempDir()

	type testCase struct {
		description   string
		dbSetupFunc   func() *ManagedTokensDatabase
		expectedError error
	}

	testCases := []testCase{
		{
			"Valid case",
			func() *ManagedTokensDatabase {
				m, _ := createAndOpenTestDatabaseWithApplicationId(tempDir)
				return m
			},
			nil,
		},
		{
			"Database where we can't get application ID",
			func() *ManagedTokensDatabase {
				dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m := &ManagedTokensDatabase{filename: dbLocation}
				defer m.Close()

				var err error
				if m.db, err = sql.Open("sqlite3", dbLocation); err != nil {
					return nil
				}
				return m
			},
			&databaseCheckError{"foobar", nil}, // We don't care about the contents here - just that we get the right error type returned
		},
		{
			"Improper applicationId",
			func() *ManagedTokensDatabase {
				dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m := &ManagedTokensDatabase{filename: dbLocation}

				var err error
				if m.db, err = sql.Open("sqlite3", dbLocation); err != nil {
					return nil
				}

				// Set the application ID
				if _, err = m.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", 42)); err != nil {
					return nil
				}
				return m
			},
			&databaseCheckError{"foobar", nil}, // We don't care about the contents here - just that we get the right error type returned
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				m := test.dbSetupFunc()
				if m != nil {
					defer m.Close()
				}
				var e *databaseCheckError
				err := m.checkApplicationId()
				if err != nil && test.expectedError == nil {
					t.Errorf("Expected application ID check to pass.  Got error %s instead", err)
				} else if test.expectedError != nil {
					if err == nil {
						t.Errorf("Expected error %s, got nil instead", test.expectedError)
					} else if !errors.As(err, &e) {
						t.Errorf("Got wrong error type from application ID check.  Expected *databaseCheckError, got %T instead", err)
					}
				}
			},
		)
	}
}

func TestUnpackNamedDimensionData(t *testing.T) {
	type testCase struct {
		description     string
		data            [][]any
		expectedResults []string
		expectedErr     error
	}

	testCases := []testCase{
		{
			"Good data",
			[][]any{
				{
					"foo",
				},
				{
					"bar",
				},
				{
					"baz",
				},
			},
			[]string{"foo", "bar", "baz"},
			nil,
		},
		{
			"No data",
			[][]any{},
			[]string{},
			nil,
		},
		{
			"Dimension row has more than 1 elt errDatabaseDataWrongStructure",
			[][]any{
				{
					"foo",
					"bar",
				},
				{
					"bar",
					"baz",
				},
			},
			nil,
			errDatabaseDataWrongStructure,
		},
		{
			"Dimension row has no elts.  Like []{[]{ }} errDatabaseDataWrongStructure",
			[][]any{
				{},
				{},
			},
			nil,
			errDatabaseDataWrongStructure,
		},
		{
			"Dimension row is not a string value errDatabaseDataWrongType",
			[][]any{
				{42},
				{21},
			},
			nil,
			errDatabaseDataWrongType,
		},

		{
			"Mixed good, wrong structure rows (bad last) errDatabaseDataWrongStructure",
			[][]any{
				{"foo"},
				{"bar"},
				{"baz", "noooo"},
			},
			nil,
			errDatabaseDataWrongStructure,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				results, err := unpackNamedDimensionData(test.data)
				if !slices.Equal(results, test.expectedResults) {
					t.Errorf("Got wrong results.  Expected %v, got %v", test.expectedResults, results)
				}
				if !errors.Is(err, test.expectedErr) {
					t.Errorf("Got wrong error.  Expected %v, got %v", test.expectedErr, err)
				}
			},
		)
	}
}

type fakeInsertValuesString string

func (f *fakeInsertValuesString) insertValues() []any { return []any{string(*f)} }

func TestConvertStringSliceToInsertValuesSlice(t *testing.T) {
	// func convertStringSliceToInsertValuesSlice(converter func(string) insertValues, stringSlice []string) []insertValues {
	type testCase struct {
		description         string
		converter           func(string) insertValues
		stringSlice         []string
		expectedResultsFunc func() []insertValues
	}

	testCases := []testCase{
		{
			"non-empty string",
			func(s string) insertValues {
				val := fakeInsertValuesString(s)
				return insertValues(&val)
			},
			[]string{
				"foo",
				"bar",
				"baz",
			},
			func() []insertValues {
				val1 := fakeInsertValuesString("foo")
				val2 := fakeInsertValuesString("bar")
				val3 := fakeInsertValuesString("baz")
				return []insertValues{&val1, &val2, &val3}
			},
		},
		{
			"empty string",
			func(s string) insertValues {
				val := fakeInsertValuesString(s)
				return insertValues(&val)
			},
			[]string{},
			func() []insertValues {
				return []insertValues{}
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				results := convertStringSliceToInsertValuesSlice(test.converter, test.stringSlice)
				if !slices.EqualFunc(
					results,
					test.expectedResultsFunc(),
					func(elt1 insertValues, elt2 insertValues) bool {
						val1, ok := elt1.(*fakeInsertValuesString)
						if !ok {
							t.Errorf("Got wrong type in results.  Expected *fakeInsertValuesString, got %T", elt1)
							return false
						}
						val2, _ := elt2.(*fakeInsertValuesString)
						if *val1 != *val2 {
							t.Errorf("Got wrong value in results.  Expected %v, got %v", val2, val1)
							return false
						}
						return true
					},
				) {
					t.Errorf("Got wrong result.  Expected %v, got %v", test.expectedResultsFunc(), results)
				}
			},
		)
	}
}

func TestNewInsertValuesFromUnderlyingString(t *testing.T) {
	type testCase struct {
		description        string
		inputString        string
		expectedOutputFunc func() insertValues
	}

	testCases := []testCase{
		{
			"Normal string",
			"foobar",
			func() insertValues {
				s := fakeInsertValuesString("foobar")
				return insertValues(&s)
			},
		},
		{
			"Empty string",
			"",
			func() insertValues {
				s := fakeInsertValuesString("")
				return insertValues(&s)
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := newInsertValuesFromUnderlyingString[*fakeInsertValuesString](test.inputString)
				exp := test.expectedOutputFunc()
				resultVal, ok := result.(*fakeInsertValuesString)
				if !ok {
					t.Errorf("Got wrong underlying type in result.  Expected *fakeInsertValuesString, got %T", result)
				}
				expVal, _ := exp.(*fakeInsertValuesString)

				if *resultVal != *expVal {
					t.Errorf("Got wrong result.  Expected %v, got %v", expVal, resultVal)
				}
			},
		)
	}
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
func createAndOpenTestDatabaseWithApplicationId(tempDir string) (*ManagedTokensDatabase, error) {
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
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
func TestManagedTokensDatabase_Location(t *testing.T) {
	tempDir := t.TempDir()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	m := &ManagedTokensDatabase{filename: dbLocation}

	location := m.Location()

	if location != dbLocation {
		t.Errorf("Expected location to be %s, but got %s", dbLocation, location)
	}
}
func TestDbArgsToStringSlice(t *testing.T) {
	testCases := []struct {
		args         []any
		expectedArgs []string
		expectedErr  error
	}{
		{
			args:         []any{"hello", 42, 3.14, 2.718},
			expectedArgs: []string{"hello", "42", "3.14", "2.718"},
			expectedErr:  nil,
		},
		{
			args:         []any{"world", 123, 1.23, 4.56},
			expectedArgs: []string{"world", "123", "1.23", "4.56"},
			expectedErr:  nil,
		},
		{
			args:         []any{"foo", "bar", "baz"},
			expectedArgs: []string{"foo", "bar", "baz"},
			expectedErr:  nil,
		},
		{
			args:         []any{true, false},
			expectedArgs: []string{},
			expectedErr:  fmt.Errorf("unsupported type bool"),
		},
	}

	for _, tc := range testCases {
		argsStr, err := dbArgsToStringSlice(tc.args)

		if err != nil {
			if tc.expectedErr == nil {
				t.Errorf("Expected nil error for args %v, got %v", tc.args, err)
			} else if err.Error() != tc.expectedErr.Error() {
				t.Errorf("Expected error %v for args %v, got %v", tc.expectedErr, tc.args, err)
			}
		} else if !slices.Equal(argsStr, tc.expectedArgs) {
			t.Errorf("Expected args %v, got %v", tc.expectedArgs, argsStr)
		}
	}
}
