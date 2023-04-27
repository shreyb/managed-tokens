package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"testing"

	_ "github.com/mattn/go-sqlite3"
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(services, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, services)
		}
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(nodes, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, nodes)
		}
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if err == nil && !slicesHaveSameElements(data, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, data)
		}
	}

}

// TestGetSetupErrorsInfo checks that GetSetupErrorsInfo properly retrieves setupError counts from the database
func TestGetSetupErrorsInfo(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []setupErrorCount
	}
	testCases := []testCase{
		{
			"No information in database",
			[]setupErrorCount{},
		},
		{
			"Some setup error counts in database",
			[]setupErrorCount{
				{"foo", 1},
				{"bar", 2},
				{"baz", 0},
			},
		},
	}
	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(retrievedData, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
		}
	}
}

// TestGetPushErrorsInfo checks that GetPushErrorsInfo properly retrieves pushError counts from the database
func TestGetPushErrorsInfo(t *testing.T) {
	type testCase struct {
		description  string
		expectedData []pushErrorCount
	}
	testCases := []testCase{
		{
			"No information in database",
			[]pushErrorCount{},
		},
		{
			"Some push error counts in database",
			[]pushErrorCount{
				{"foo", "foonode", 1},
				{"bar", "barnode", 2},
				{"baz", "baznode", 0},
				{"baz", "baznode2", 1},
			},
		},
	}
	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
			t.Errorf("Failure to obtain setup errors data for test %s: %s", test.description, err)
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
		if !slicesHaveSameElements(retrievedData, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
		}
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(retrievedData, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
		}
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(retrievedData, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
		}
	}
}
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

	for _, test := range testCases {
		goodDbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
		defer os.Remove(goodDbLocation)

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
		if !slicesHaveSameElements(retrievedData, test.expectedData) {
			t.Errorf("Retrieved data and expected data do not match.  Expected %v, got %v", test.expectedData, retrievedData)
		}
	}
}
func TestUpdatePushErrorsTable(t *testing.T) {}

// TODO
// TODO:  Figure out how to make OpenNotificationsDatabse return non-nil error so we can test it
// TODO:  Test notificationsDb-specific functions like we do for the FERRYUIDDbs
// TODO:  Implement checkSchema utility func.  Need to change the createUIDTable statment once I decide on schema

// TestOpenNotificationsDatabase checks that Open properly opens the underlying SQLite database underlying NotificationsDatabase, and returns the proper error
// if there is an issue
// func TestOpenNotificationsDatabase(t *testing.T) {
// 	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 	defer os.Remove(dbLocation)
// 	f := &NotificationsDatabase{filename: dbLocation}
// 	err := f.Open()
// 	if err != nil {
// 		t.Errorf("Error does not match expected error. Expected nil, got %s", err)
// 	}
// }

// TODO:  Can't make this fail.  Figured out how to
// func TestOpenNotificationsDatabaseBad(t *testing.T) {

// 	badLocation := "/this/path/does/not/exist"
// 	var dbOpenError *databaseOpenError

// 	f := &NotificationsDatabase{filename: badLocation}
// 	err := f.Open()
// 	if !errors.As(err, &dbOpenError) {
// 		t.Errorf("Error does not match expected error for test.  Expected a databaseOpenError, got %v", err)
// 	}
// }

// // TestOpenOrCreateNotificationsDatabase checks that we can create and reopen a new NotificationsDatabase
// func TestOpenOrCreateNotificationsDatabase(t *testing.T) {
// 	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 	defer os.Remove(dbLocation)

// 	// Test that we can create a new db at a new location
// 	func() {
// 		goodTestDb, err := OpenOrCreateNotificationsDatabase(dbLocation)
// 		if err != nil {
// 			t.Errorf("Could not create new database, %s", err)
// 		}
// 		defer goodTestDb.Close()

// 		if err = checkSchemaForNotificationsDatabase(goodTestDb); err != nil {
// 			t.Error("Schema check failed")
// 		}
// 	}()

// 	// Test that we can reopen the db
// 	goodTestDb, err := OpenOrCreateNotificationsDatabase(dbLocation)
// 	if err != nil {
// 		t.Errorf("Could not open previously-created database, %s", err)
// 	}
// 	goodTestDb.Close()
// }

// TestOpenOrCreateNotificationsDatabaseCheckError creates a file that will fail the
// NotificationsDatabase check.  We want to make sure we get the proper returned *databaseCheckError
// This call to OpenOrCreateDatabase should return an error because we create a tempfile, and because it exists,
// OpenOrCreateDatabase never runs initialize() and assigns the ApplicationId
// func TestOpenOrCreateNotificationsDatabaseCheckError(t *testing.T) {
// 	var checkError *databaseCheckError
// 	dbLocation, err := os.CreateTemp(os.TempDir(), "managed-tokens")
// 	if err != nil {
// 		t.Error("Could not create temp file for test database")
// 	}
// 	defer os.Remove(dbLocation.Name())

// 	badTestDb, err := OpenOrCreateNotificationsDatabase(dbLocation.Name())
// 	if !errors.As(err, &checkError) {
// 		t.Errorf(
// 			"Returned error from OpenOrCreateDatabase is of wrong type.  Expected %T, got %T",
// 			checkError,
// 			err,
// 		)
// 	} else {
// 		badTestDb.Close()
// 	}

// }

// TestInitializeNotificationsDatabase makes sure that the *(NotificationsDatabase).initialize func returns a *NotificationsDatabase object with the
// // correct ApplicationId and schema
// func TestInitializeNotificationsDatabase(t *testing.T) {
// 	dbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 	defer os.Remove(dbLocation)

// 	f := &NotificationsDatabase{filename: dbLocation}
// 	if err := f.initialize(); err != nil {
// 		t.Error("Could not initialize NotificationsDatabase")
// 	}
// 	defer f.Close()
// 	if err := checkSchemaForNotificationsDatabase(f); err != nil {
// 		t.Error("initialized NotificationsDatabase failed schema check")
// 	}
// }

// TODO:  Test notificationsDb-specific functions like we do for the FERRYUIDDbs

// // TestCreateUidTables checks that *(NotificationsDatabase).createUidsTable creates the proper UID table in the database by checking the schema
// func TestCreateUidsTable(t *testing.T) {
// 	var err error
// 	goodDbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 	defer os.Remove(goodDbLocation)

// 	goodTestDb := &NotificationsDatabase{filename: goodDbLocation}
// 	goodTestDb.db, err = sql.Open("sqlite3", goodDbLocation)
// 	if err != nil {
// 		t.Error("Could not open the UID database file")
// 	}
// 	defer goodTestDb.Close()

// 	if err := goodTestDb.createUidsTable(); err != nil {
// 		t.Error("Could not create UIDs table in NotificationsDatabase")
// 	}
// 	if err := checkSchema(goodTestDb); err != nil {
// 		t.Error("Test NotificationsDatabase has wrong schema")
// 	}
// }

// // TestInsertAndConfirmUIDsInTable checks that we can insert and retrieve
// // FERRY data into the NotificationsDatabase
// func TestInsertAndConfirmUIDsInTable(t *testing.T) {
// 	type testCase struct {
// 		description           string
// 		fakeData              []FerryUIDDatum
// 		expectedRetrievedData []FerryUIDDatum
// 	}

// 	testCases := []testCase{
// 		{
// 			"Use an empty database with no data stored",
// 			[]FerryUIDDatum{},
// 			[]FerryUIDDatum{},
// 		},
// 		{
// 			"Use a database with fake data",
// 			[]FerryUIDDatum{
// 				&checkDatum{
// 					username: "testuser",
// 					uid:      12345,
// 				},
// 				&checkDatum{
// 					username: "anothertestuser",
// 					uid:      67890,
// 				},
// 			},
// 			[]FerryUIDDatum{
// 				&checkDatum{
// 					username: "testuser",
// 					uid:      12345,
// 				},
// 				&checkDatum{
// 					username: "anothertestuser",
// 					uid:      67890,
// 				},
// 			},
// 		},
// 	}

// 	for _, test := range testCases {
// 		t.Run(
// 			test.description,
// 			func(t *testing.T) {
// 				goodDbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 				defer os.Remove(goodDbLocation)

// 				goodTestDb, err := OpenOrCreateNotificationsDatabase(goodDbLocation)
// 				if err != nil {
// 					t.Errorf("Could not create new database, %s", err)
// 				}
// 				defer goodTestDb.Close()

// 				if err := goodTestDb.InsertUidsIntoTableFromFERRY(context.Background(), test.fakeData); err != nil {
// 					t.Error("Could not insert fake data into test database")
// 				}

// 				retrievedData, err := goodTestDb.ConfirmUIDsInTable(context.Background())
// 				if err != nil {
// 					t.Error("Could not retrieve fake data from test database")
// 				}

// 				for _, expectedDatum := range test.expectedRetrievedData {
// 					found := false
// 					for _, testDatum := range retrievedData {
// 						if (testDatum.Uid() == expectedDatum.Uid()) && (testDatum.Username() == expectedDatum.Username()) {
// 							found = true
// 							break
// 						}
// 					}
// 					if !found {
// 						t.Errorf(
// 							"Did not find expected datum %v in retrieved data",
// 							expectedDatum,
// 						)
// 					}
// 				}
// 			},
// 		)
// 	}

// }

// // TestGetUIDsByUsername checks that we can retrieve the proper UID given a particular username.
// // If we give a user that does not exist, we should get 0 as the result, and an error
// func TestGetUIDsByUsername(t *testing.T) {

// 	type testCase struct {
// 		description   string
// 		testUsername  string
// 		expectedUid   int
// 		expectedError error
// 	}

// 	testCases := []testCase{
// 		{
// 			"Get UID for user we know is in the database",
// 			"testuser",
// 			12345,
// 			nil,
// 		},
// 		{
// 			"Attempt to get UID for user who is not in the database",
// 			"nonexistentuser",
// 			0,
// 			errors.New("sql: no rows in result set"),
// 		},
// 	}

// 	fakeData := []FerryUIDDatum{
// 		&checkDatum{
// 			username: "testuser",
// 			uid:      12345,
// 		},
// 		&checkDatum{
// 			username: "anothertestuser",
// 			uid:      67890,
// 		},
// 	}

// 	goodDbLocation := path.Join("/tmp/", fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
// 	defer os.Remove(goodDbLocation)

// 	goodTestDb, err := OpenOrCreateNotificationsDatabase(goodDbLocation)
// 	if err != nil {
// 		t.Errorf("Could not create new database, %s", err)
// 	}
// 	defer goodTestDb.Close()

// 	if err := goodTestDb.InsertUidsIntoTableFromFERRY(context.Background(), fakeData); err != nil {
// 		t.Error("Could not insert fake data into test database")
// 	}

// 	for _, test := range testCases {
// 		t.Run(
// 			test.description,
// 			func(t *testing.T) {
// 				ctx := context.Background()
// 				retrievedUid, err := goodTestDb.GetUIDByUsername(ctx, test.testUsername)
// 				if err != nil {
// 					if test.expectedError != nil {
// 						// if err.Error() != test.expectedError.Error() {
// 						if err.Error() != test.expectedError.Error() {
// 							t.Errorf(
// 								"Got wrong error.  Expected %v, got %v",
// 								test.expectedError,
// 								err,
// 							)
// 						}
// 					} else {
// 						t.Error("Could not get UID by username")
// 					}
// 				}
// 				if retrievedUid != test.expectedUid {
// 					t.Errorf(
// 						"Retrieved UID and expected UID do not match.  Expected %d, got %d",
// 						test.expectedUid,
// 						retrievedUid,
// 					)
// 				}
// 			},
// 		)
// 	}

// }

// TODO:  Implement this.  Need to change the createUIDTable statment once I decide on schema
// checkSchema is a testing utility function to make sure that a test NotificationsDatabase has the right schema
//
//	func checkSchemaForNotificationsDatabase(f *NotificationsDatabase) error {
//		var schema string
//		if err := f.db.QueryRow("SELECT sql FROM sqlite_master;").Scan(&schema); err != nil {
//			return err
//		}
//		expectedSchema := strings.TrimRight(strings.TrimSpace(createUIDTableStatement), ";")
//		if schema != expectedSchema {
//			return fmt.Errorf(
//				"Schema for database does not match expected schema.  Expected %s, got %s",
//				expectedSchema,
//				schema,
//			)
//		}
//		return nil
//	}
//
// Temporary placeholder
// func checkSchemaForNotificationsDatabase(f *NotificationsDatabase) error { return nil }
