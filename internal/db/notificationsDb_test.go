package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/shreyb/managed-tokens/internal/testUtils"
)

// TestGetAllServices checks that GetAllServices correctly retrieves services from the ManagedTokensDatabase
func TestGetAllServices(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []string
	}
	testCases := []testCase{
		{
			"No services in database",
			[]string{},
		},
		{
			"Some services in database",
			[]string{"foo", "bar", "baz"},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				for _, datum := range test.expectedData {
					if _, err := m.db.Exec("INSERT INTO services (name) VALUES (?);", datum); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				services, err := m.GetAllServices(ctx)
				if err != nil {
					t.Errorf("Failure to obtain services for test %s: %s", test.description, err)
				}
				if !testUtils.SlicesHaveSameElements(services, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, services)
				}
			},
		)
	}
}

// TestGetAllNodes checks that GetAllNodes correctly retrieves nodes from the ManagedTokensDatabase
func TestGetAllNodes(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []string
	}
	testCases := []testCase{
		{
			"No nodes in database",
			[]string{},
		},
		{
			"Some nodes in database",
			[]string{"foo", "bar", "baz"},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				for _, datum := range test.expectedData {
					if _, err := m.db.Exec("INSERT INTO nodes (name) VALUES (?);", datum); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				nodes, err := m.GetAllNodes(ctx)
				if err != nil {
					t.Errorf("Failure to obtain nodes for test %s: %s", test.description, err)
				}
				if !testUtils.SlicesHaveSameElements(nodes, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, nodes)
				}
			},
		)
	}
}

// TestGetNamedDimensionStringValues checks that getNamedDimentionStringValues returns the expected result or error depending on
// the given SQL to run
func TestGetNamedDimensionStringValues(t *testing.T) {
	type testCase struct {
		description     string
		sqlGetStatement string
		expectedData    []string
		expectedErr     error
	}
	testCases := []testCase{
		{
			"No data in table",
			"SELECT name FROM nodes",
			[]string{},
			nil,
		},
		{
			"Valid data in table",
			"SELECT name FROM nodes",
			[]string{"foo", "bar", "baz"},
			nil,
		},
		{
			"Valid data in table, bad query result structure",
			"SELECT id, name FROM nodes",
			[]string{"foo", "bar", "baz"},
			errDatabaseDataWrongStructure,
		},
		{
			"Valid data in table, but the type of the column we picked is wrong",
			"SELECT id FROM nodes",
			[]string{"foo", "bar", "baz"},
			errDatabaseDataWrongType,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				for _, datum := range test.expectedData {
					if _, err := m.db.Exec("INSERT INTO nodes (name) VALUES (?);", datum); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				data, err := m.getNamedDimensionStringValues(ctx, test.sqlGetStatement)
				if !errors.Is(err, test.expectedErr) {
					t.Errorf("Got wrong error.  Expected %s, got %s", test.expectedErr, err)
				}
				if err == nil && !testUtils.SlicesHaveSameElements(data, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, data)
				}
			},
		)
	}
}

// TestGetSetupErrorsInfo checks that GetSetupErrorsInfo properly retrieves setupError counts from the database
func TestGetSetupErrorsInfo(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []setupErrorCount
		expectedErr  error
	}
	testCases := []testCase{
		{
			"No information in database",
			[]setupErrorCount{},
			sql.ErrNoRows,
		},
		{
			"Some setup error counts in database",
			[]setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
			nil,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertSetupErrors := `
				INSERT INTO setup_errors(service_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					? AS count
				;
				`

				for _, datum := range test.expectedData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertSetupErrors, datum.service, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				data, err := m.GetSetupErrorsInfo(ctx)
				if err != nil {
					if !errors.Is(err, test.expectedErr) {
						t.Errorf("Got wrong error from test %s.  Expected %s, got %s", test.description, test.expectedErr, err)
					} else {
						return
					}
					t.Errorf("Failure to obtain setup errors data for test %s: %s", test.description, err)
				}

				retrievedData := make([]setupErrorCount, 0, len(data))
				for _, datum := range data {
					val, ok := datum.(*setupErrorCount)
					if ok {
						if val != nil {
							retrievedData = append(retrievedData, *val)
						}
					}
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestGetSetupErrorsInfoByService checks that GetSetupErrorsInfoByService properly retrieves the setupError count for a single service
func TestGetSetupErrorsInfoByService(t *testing.T) {
	type testCase struct {
		description    string
		testData       []setupErrorCount
		serviceToQuery string
		expectedCount  int
		expectedErr    error
	}
	testCases := []testCase{
		{
			"No information in database",
			[]setupErrorCount{},
			"foo",
			0,
			sql.ErrNoRows,
		},
		{
			"Some setup error counts in database",
			[]setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
			"foo",
			1,
			nil,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertSetupErrors := `
				INSERT INTO setup_errors(service_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					? AS count
				;
				`

				for _, datum := range test.testData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertSetupErrors, datum.service, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				datum, err := m.GetSetupErrorsInfoByService(ctx, test.serviceToQuery)
				if err != nil {
					if !errors.Is(err, test.expectedErr) {
						t.Errorf("Got wrong error from test %s.  Expected %s, got %s", test.description, test.expectedErr, err)
					} else {
						return
					}
					t.Errorf("Failure to obtain setup errors data for test %s: %s", test.description, err)
				}

				val, ok := datum.(*setupErrorCount)
				if ok {
					if val != nil {
						if val.count != test.expectedCount {
							t.Errorf("Got wrong count from test %s.  Expected %d, got %d", test.description, test.expectedCount, val.count)
						}
					}
				} else {
					t.Errorf("Got wrong type from test %s.  Expected *setupErrorCount, got %T", test.description, val)
				}
			},
		)
	}
}

// TestGetPushErrorsInfo checks that GetPushErrorsInfo properly retrieves pushError counts from the database
func TestGetPushErrorsInfo(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []pushErrorCount
		expectedErr  error
	}
	testCases := []testCase{
		{
			"No information in database",
			[]pushErrorCount{},
			sql.ErrNoRows,
		},
		{
			"Some push error counts in database",
			[]pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"baz", "baznode2", 1},
			},
			nil,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertNodes := "INSERT INTO nodes(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertPushErrors := `
				INSERT INTO push_errors(service_id, node_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					(SELECT id FROM nodes WHERE nodes.name = ?) AS node_id,
					? AS count
				;
				`

				for _, datum := range test.expectedData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertNodes, datum.node); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertPushErrors, datum.service, datum.node, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				data, err := m.GetPushErrorsInfo(ctx)
				if err != nil {
					if !errors.Is(err, test.expectedErr) {
						t.Errorf("Got wrong error from test %s.  Expected %s, got %s", test.description, test.expectedErr, err)
					} else {
						return
					}
					t.Errorf("Failure to obtain push errors data for test %s: %s", test.description, err)
				}
				retrievedData := make([]pushErrorCount, 0, len(data))
				for _, datum := range data {
					val, ok := datum.(*pushErrorCount)
					if ok {
						if val != nil {
							retrievedData = append(retrievedData, *val)
						}
					}
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestGetPushErrorsInfo checks that GetPushErrorsInfo properly retrieves pushError counts from the database
func TestGetPushErrorsByServiceInfo(t *testing.T) {
	type testCase struct {
		description    string
		testData       []pushErrorCount
		serviceToQuery string
		expectedData   []pushErrorCount
		expectedErr    error
	}
	testCases := []testCase{
		{
			"No information in database",
			[]pushErrorCount{},
			"foo",
			[]pushErrorCount{},
			sql.ErrNoRows,
		},
		{
			"Some push error counts in database, multiple results",
			[]pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"baz", "baznode2", 1},
			},
			"baz",
			[]pushErrorCount{
				{"baz", "baznode", 0},
				{"baz", "baznode2", 1},
			},
			nil,
		},
		{
			"Some push error counts in database, single result",
			[]pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"baz", "baznode2", 1},
			},
			"foo",
			[]pushErrorCount{
				{"foo", "foonode", 1},
			},
			nil,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertNodes := "INSERT INTO nodes(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertPushErrors := `
				INSERT INTO push_errors(service_id, node_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					(SELECT id FROM nodes WHERE nodes.name = ?) AS node_id,
					? AS count
				;
				`

				for _, datum := range test.testData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertNodes, datum.node); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertPushErrors, datum.service, datum.node, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				data, err := m.GetPushErrorsInfoByService(ctx, test.serviceToQuery)
				if err != nil {
					if !errors.Is(err, test.expectedErr) {
						t.Errorf("Got wrong error from test %s.  Expected %s, got %s", test.description, test.expectedErr, err)
					} else {
						return
					}
					t.Errorf("Failure to obtain push errors data for test %s: %s", test.description, err)
				}
				retrievedData := make([]pushErrorCount, 0, len(data))
				for _, datum := range data {
					val, ok := datum.(*pushErrorCount)
					if ok {
						if val != nil {
							retrievedData = append(retrievedData, *val)
						}
					}
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestUpdateServices ensures that UpdateServices properly updates the services database table
func TestUpdateServices(t *testing.T) {
	type testCase struct {
		description  string
		originalData []string
		newData      []string
		expectedData []string
	}

	testCases := []testCase{
		{
			description:  "No data exists, none inserted",
			originalData: nil,
			newData:      nil,
			expectedData: nil,
		},
		{
			description:  "First insert of data",
			originalData: nil,
			newData:      []string{"foo", "bar", "baz"},
			expectedData: []string{"foo", "bar", "baz"},
		},
		{
			description:  "Add more data",
			originalData: []string{"foo", "bar", "baz"},
			newData:      []string{"gopher"},
			expectedData: []string{"foo", "bar", "baz", "gopher"},
		},
		{
			description:  "A subset of existing data is added.  Should retain everything",
			originalData: []string{"foo", "bar", "baz"},
			newData:      []string{"foo"},
			expectedData: []string{"foo", "bar", "baz"},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				for _, datum := range test.originalData {
					if _, err := m.db.Exec("INSERT INTO services (name) VALUES (?);", datum); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				if err = m.UpdateServices(ctx, test.newData); err != nil {
					t.Errorf("Could not update services for test %s: %s", test.description, err)
				}

				rows, err := m.db.Query("SELECT name FROM services;")
				if err != nil {
					t.Errorf("Could not retrieve rows from database for test %s: %s", test.description, err)
				}

				retrievedData := make([]string, 0, len(test.expectedData))
				for rows.Next() {
					var datum string
					if err := rows.Scan(&datum); err != nil {
						t.Errorf("Error scanning row for test %s: %s", test.description, err)
					}
					retrievedData = append(retrievedData, datum)
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestUpdateNodes ensures that UpdateNodes properly updates the nodes database table.  Note that this test is pretty much identical to
// TestUpdateServices since both behave identically on different database tables that have the same overall structure
func TestUpdateNodes(t *testing.T) {
	type testCase struct {
		description  string
		originalData []string
		newData      []string
		expectedData []string
	}

	testCases := []testCase{
		{
			description:  "No data exists, none inserted",
			originalData: nil,
			newData:      nil,
			expectedData: nil,
		},
		{
			description:  "First insert of data",
			originalData: nil,
			newData:      []string{"foo", "bar", "baz"},
			expectedData: []string{"foo", "bar", "baz"},
		},
		{
			description:  "Add more data",
			originalData: []string{"foo", "bar", "baz"},
			newData:      []string{"gopher"},
			expectedData: []string{"foo", "bar", "baz", "gopher"},
		},
		{
			description:  "A subset of existing data is added.  Should retain everything",
			originalData: []string{"foo", "bar", "baz"},
			newData:      []string{"foo"},
			expectedData: []string{"foo", "bar", "baz"},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				for _, datum := range test.originalData {
					if _, err := m.db.Exec("INSERT INTO nodes (name) VALUES (?);", datum); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				// The test
				ctx := context.Background()
				if err = m.UpdateNodes(ctx, test.newData); err != nil {
					t.Errorf("Could not update nodes for test %s: %s", test.description, err)
				}

				rows, err := m.db.Query("SELECT name FROM nodes;")
				if err != nil {
					t.Errorf("Could not retrieve rows from database for test %s: %s", test.description, err)
				}

				retrievedData := make([]string, 0, len(test.expectedData))
				for rows.Next() {
					var datum string
					if err := rows.Scan(&datum); err != nil {
						t.Errorf("Error scanning row for test %s: %s", test.description, err)
					}
					retrievedData = append(retrievedData, datum)
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestUpdateSetupErrorsTable checks that running UpdateSetupErrorsTable properly updates the setup_errors table in the ManagedTokenDatabase
func TestUpdateSetupErrorsTable(t *testing.T) {
	type testCase struct {
		description  string
		originalData []setupErrorCount
		newData      []setupErrorCount
		expectedData []setupErrorCount
	}

	testCases := []testCase{
		{
			description:  "No data exists, none inserted",
			originalData: nil,
			newData:      nil,
			expectedData: nil,
		},
		{
			description:  "First insert of data",
			originalData: nil,
			newData: []setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
			expectedData: []setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
		},
		{
			description: "Add more data",
			originalData: []setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
			newData: []setupErrorCount{{"gopher", 1}},
			expectedData: []setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
				{"gopher", 1},
			},
		},
		{
			description: "A subset of existing data is modified.  Should retain everything else",
			originalData: []setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
				{"gopher", 1},
			},
			newData: []setupErrorCount{
				{"foo", 1},
				{"bar", 0},
			},
			expectedData: []setupErrorCount{
				{"foo", 1},
				{"bar", 0},
				{"baz", 0},
				{"gopher", 1},
			},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertSetupErrors := `
				INSERT INTO setup_errors(service_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					? AS count
				;
				`

				for _, datum := range test.expectedData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertSetupErrors, datum.service, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				newData := make([]SetupErrorCount, 0, len(test.newData))
				for _, datum := range test.newData {
					newData = append(newData, &datum)
				}
				// The test
				ctx := context.Background()
				if err = m.UpdateSetupErrorsTable(ctx, newData); err != nil {
					t.Errorf("Could not update setup_errors table for test %s: %s", test.description, err)
				}

				checkQuery := `
				SELECT
					services.name,
					setup_errors.count
				FROM
					setup_errors
					INNER JOIN services ON services.id = setup_errors.service_id
				;
				`
				rows, err := m.db.Query(checkQuery)
				if err != nil {
					t.Errorf("Could not retrieve rows from database for test %s: %s", test.description, err)
				}

				retrievedData := make([]setupErrorCount, 0, len(test.expectedData))
				for rows.Next() {
					var serviceName string
					var count int
					if err := rows.Scan(&serviceName, &count); err != nil {
						t.Errorf("Error scanning row for test %s: %s", test.description, err)
					}
					retrievedData = append(retrievedData, setupErrorCount{serviceName, count})
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}

// TestUpdatePushErrorsTable checks that running UpdatePushErrorsTable properly updates the push_errors table in the ManagedTokenDatabase
func TestUpdatePushErrorsTable(t *testing.T) {
	type testCase struct {
		description  string
		originalData []pushErrorCount
		newData      []pushErrorCount
		expectedData []pushErrorCount
	}

	testCases := []testCase{
		{
			description:  "No data exists, none inserted",
			originalData: nil,
			newData:      nil,
			expectedData: nil,
		},
		{
			description:  "First insert of data",
			originalData: nil,
			newData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
			},
			expectedData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
			},
		},
		{
			description: "Add more data",
			originalData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
			},
			newData: []pushErrorCount{{"gopher", "gophernode", 1}},
			expectedData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"gopher", "gophernode", 1},
			},
		},
		{
			description: "A subset of existing data is modified.  Should retain everything else",
			originalData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"gopher", "gophernode", 1},
			},
			newData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"foo", "foonode2", 1},
				{"bar", "barnode", 0},
			},
			expectedData: []pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 0},
				{"baz", "baznode", 0},
				{"gopher", "gophernode", 1},
				{"foo", "foonode2", 1},
			},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {

				goodDbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
				m, err := OpenOrCreateDatabase(goodDbLocation)
				if err != nil {
					t.Errorf("Could not create new database, %s", err)
					return
				}
				defer m.Close()

				// INSERT our test data
				insertServices := "INSERT INTO services(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertNodes := "INSERT INTO nodes(name) VALUES (?) ON CONFLICT(name) DO NOTHING;"
				insertPushErrors := `
				INSERT INTO push_errors(service_id, node_id, count)
				SELECT
					(SELECT id FROM services WHERE services.name = ?) AS service_id,
					(SELECT id FROM nodes WHERE nodes.name = ?) AS node_id,
					? AS count
				;
				`
				for _, datum := range test.expectedData {
					if _, err := m.db.Exec(insertServices, datum.service); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertNodes, datum.node); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
					if _, err := m.db.Exec(insertPushErrors, datum.service, datum.node, datum.count); err != nil {
						t.Errorf("Could not insert test data into database for test %s: %s", test.description, err)
						return
					}
				}

				newData := make([]PushErrorCount, 0, len(test.newData))
				for _, datum := range test.newData {
					newData = append(newData, &datum)
				}
				// The test
				ctx := context.Background()
				if err = m.UpdatePushErrorsTable(ctx, newData); err != nil {
					t.Errorf("Could not update push_errors table for test %s: %s", test.description, err)
				}

				checkQuery := `
				SELECT
					services.name,
					nodes.name,
					push_errors.count
				FROM
					push_errors
					INNER JOIN services ON services.id = push_errors.service_id
					INNER JOIN nodes ON nodes.id = push_errors.node_id
				;
				`
				rows, err := m.db.Query(checkQuery)
				if err != nil {
					t.Errorf("Could not retrieve rows from database for test %s: %s", test.description, err)
				}

				retrievedData := make([]pushErrorCount, 0, len(test.expectedData))
				for rows.Next() {
					var serviceName string
					var nodeName string
					var count int
					if err := rows.Scan(&serviceName, &nodeName, &count); err != nil {
						t.Errorf("Error scanning row for test %s: %s", test.description, err)
					}
					retrievedData = append(retrievedData, pushErrorCount{serviceName, nodeName, count})
				}
				if !testUtils.SlicesHaveSameElements(retrievedData, test.expectedData) {
					t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
				}
			},
		)
	}
}
