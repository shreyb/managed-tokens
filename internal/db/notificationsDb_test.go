package db

import (
	_ "github.com/mattn/go-sqlite3"
)

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
