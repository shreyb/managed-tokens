package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/testutils"
)

func TestSetErrorCountsByServiceNilDBCase(t *testing.T) {
	ctx := context.Background()
	result, trackErrors := setErrorCountsByService(ctx, "fakeService", nil)
	if result != nil {
		t.Errorf("Expected nil serviceErrorCounts.  Got %v", result)
	}
	if trackErrors {
		t.Error("Expected trackErrors to be false.  Got true.")
	}
}

func TestSetErrorCountsByService(t *testing.T) {
	type dbData struct {
		services         []string
		nodes            []string
		priorSetupErrors []db.SetupErrorCount
		priorPushErrors  []db.PushErrorCount
	}

	type testCase struct {
		helptext string
		dbData
		service                    string
		expectedServiceErrorCounts *serviceErrorCounts
		expectedShouldTrackErrors  bool
	}

	setupZero := 0
	setupFortyTwo := 42
	setupEightyFive := 85

	testCases := []testCase{
		{
			helptext:                   "No prior data",
			dbData:                     dbData{},
			service:                    "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{0, nil, nil},
			expectedShouldTrackErrors:  true,
		},
		{
			helptext: "Only single-service setup errors, 0 case",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						0,
					},
				},
			},
			service:                    "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{setupZero, nil, &setupZero},
			expectedShouldTrackErrors:  true,
		},
		{
			helptext: "Only single-service setup errors, nonzero case",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						42,
					},
				},
			},
			service:                    "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{setupFortyTwo, nil, &setupFortyTwo},
			expectedShouldTrackErrors:  true,
		},
		{
			helptext: "Multiple service setup errors, pick the right one",
			dbData: dbData{
				services: []string{"service1", "service2"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						42,
					},
					&setupErrorCount{
						"service2",
						84,
					},
				},
			},
			service:                    "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{setupFortyTwo, nil, &setupFortyTwo},
			expectedShouldTrackErrors:  true,
		},
		{
			helptext: "Single-service push errors, single node, 0 case",
			dbData: dbData{
				services: []string{"service1"},
				nodes:    []string{"node1"},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						0,
					},
				},
			},
			service: "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{
				0,
				map[string]int{
					"node1": 0,
				},
				nil,
			},
			expectedShouldTrackErrors: true,
		},
		{
			helptext: "Single-service push errors, single node, non-zero case",
			dbData: dbData{
				services: []string{"service1"},
				nodes:    []string{"node1"},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						42,
					},
				},
			},
			service: "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{
				0,
				map[string]int{
					"node1": 42,
				},
				nil,
			},
			expectedShouldTrackErrors: true,
		},
		{
			helptext: "Single-service push errors, multiple nodes",
			dbData: dbData{
				services: []string{"service1"},
				nodes:    []string{"node1", "node2"},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						42,
					},
					&pushErrorCount{
						"service1",
						"node2",
						84,
					},
				},
			},
			service: "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{
				0,
				map[string]int{
					"node1": 42,
					"node2": 84,
				},
				nil,
			},
			expectedShouldTrackErrors: true,
		},
		{
			helptext: "Multiple-service push errors, multiple nodes, select the right service",
			dbData: dbData{
				services: []string{"service1", "service2"},
				nodes:    []string{"node1", "node2", "node3"},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						42,
					},
					&pushErrorCount{
						"service1",
						"node2",
						84,
					},
					&pushErrorCount{
						"service2",
						"node1",
						54,
					},
					&pushErrorCount{
						"service2",
						"node3",
						86,
					},
				},
			},
			service: "service2",
			expectedServiceErrorCounts: &serviceErrorCounts{
				0,
				map[string]int{
					"node1": 54,
					"node3": 86,
				},
				nil,
			},
			expectedShouldTrackErrors: true,
		},
		{
			helptext: "Multiple-service setup and push errors, multiple nodes, select the right service",
			dbData: dbData{
				services: []string{"service1", "service2"},
				nodes:    []string{"node1", "node2", "node3"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						34,
					},
					&setupErrorCount{
						"service2",
						85,
					},
				},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						42,
					},
					&pushErrorCount{
						"service1",
						"node2",
						84,
					},
					&pushErrorCount{
						"service2",
						"node1",
						54,
					},
					&pushErrorCount{
						"service2",
						"node3",
						86,
					},
				},
			},
			service: "service2",
			expectedServiceErrorCounts: &serviceErrorCounts{
				setupEightyFive,
				map[string]int{
					"node1": 54,
					"node3": 86,
				},
				&setupEightyFive,
			},
			expectedShouldTrackErrors: true,
		},
	}

	for _, test := range testCases {
		func() {
			ctx := context.Background()
			dbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
			defer os.Remove(dbLocation)

			m, err := db.OpenOrCreateDatabase(dbLocation)
			if err != nil {
				t.Errorf("Could not create test database: %s", err)
				return
			}
			defer m.Close()

			if err := m.UpdateServices(ctx, test.services); err != nil {
				t.Errorf("Could not update services in test database: %s", err)
				return
			}
			if err := m.UpdateNodes(ctx, test.nodes); err != nil {
				t.Errorf("Could not update nodes in test database: %s", err)
				return
			}
			if err := m.UpdateSetupErrorsTable(ctx, test.priorSetupErrors); err != nil {
				t.Errorf("Could not update setup errors table in test database: %s", err)
				return
			}
			if err := m.UpdatePushErrorsTable(ctx, test.priorPushErrors); err != nil {
				t.Errorf("Could not update push errors table in test database: %s", err)
				return
			}

			counts, shouldTrackErrors := setErrorCountsByService(ctx, test.service, m)
			if !reflect.DeepEqual(counts, test.expectedServiceErrorCounts) {
				t.Errorf("Got different serviceErrorCounts than expected for test %s.  Expected %v, got %v", test.helptext, test.expectedServiceErrorCounts, counts)
			}
			if shouldTrackErrors != test.expectedShouldTrackErrors {
				t.Errorf("Got different decision about tracking errors than expected for test %s.  Expected %t, got %t", test.helptext, test.expectedShouldTrackErrors, shouldTrackErrors)
			}
		}()
	}
}

func TestSaveErrorCountsInDatabase(t *testing.T) {
	// Create fake managed tokens db, populate it with various info, set a few different errorCounts, make sure correct info is saved by running Get methods
	// Note.  Use same tests cases as last test , just add adjustments
	type dbData struct {
		services         []string
		nodes            []string
		priorSetupErrors []db.SetupErrorCount
		priorPushErrors  []db.PushErrorCount
	}

	type testCase struct {
		helptext string
		dbData
		service                string
		previousErrorCounts    *serviceErrorCounts
		adjustment             func(ec *serviceErrorCounts) *serviceErrorCounts
		expectedSetupErrorData []setupErrorCount
		expectedPushErrorData  []pushErrorCount
	}

	noop := func(ec *serviceErrorCounts) *serviceErrorCounts { return ec }

	adjustSetupErrorsByOne := func(ec *serviceErrorCounts) *serviceErrorCounts {
		ec.setupErrors++
		return ec
	}

	setupZero := 0
	setupFortyTwo := 42

	// adjustPushErrorsByOneForNode := func(node string) func(*serviceErrorCounts) *serviceErrorCounts {
	// 	return func(ec *serviceErrorCounts) *serviceErrorCounts {
	// 		if _, ok := ec.pushErrors[node]; ok {
	// 			ec.pushErrors[node]++
	// 		} else {
	// 			ec.pushErrors[node] = 1
	// 		}
	// 		return ec
	// 	}
	// }

	testCases := []testCase{
		{
			helptext: "No prior data, no adjustment",
			dbData:   dbData{},
			service:  "service1",
			previousErrorCounts: &serviceErrorCounts{
				setupErrors:    setupZero,
				pushErrors:     nil,
				setupErrorsPtr: &setupZero,
			},
			adjustment:             func(ec *serviceErrorCounts) *serviceErrorCounts { return ec },
			expectedSetupErrorData: nil,
			expectedPushErrorData:  nil,
		},
		{
			helptext: "Only single-service setup errors, 0 case, no adjustment",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						0,
					},
				},
			},
			service:             "service1",
			previousErrorCounts: &serviceErrorCounts{setupZero, nil, &setupZero},
			adjustment:          noop,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 0},
			},
			expectedPushErrorData: nil,
		},
		{
			helptext: "Only single-service setup errors, nonzero case, no adjustment",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						42,
					},
				},
			},
			service:             "service1",
			previousErrorCounts: &serviceErrorCounts{setupFortyTwo, nil, &setupFortyTwo},
			adjustment:          noop,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 42},
			},
			expectedPushErrorData: nil,
		},
		{
			helptext: "Only single-service setup errors, 0 case, adjustment of SetupErrorsCount by 1",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						0,
					},
				},
			},
			service:             "service1",
			previousErrorCounts: &serviceErrorCounts{setupZero, nil, &setupZero},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 1},
			},
			expectedPushErrorData: nil,
		},
		{
			helptext: "Only single-service setup errors, nonzero case, adjustment of SetupErrorsCount by 1",
			dbData: dbData{
				services: []string{"service1"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						42,
					},
				},
			},
			service:             "service1",
			previousErrorCounts: &serviceErrorCounts{setupFortyTwo, nil, &setupFortyTwo},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 43},
			},
			expectedPushErrorData: nil,
		},
		{
			helptext: "Multiple service setup errors, adjust only setup errors by 1 of the correct service",
			dbData: dbData{
				services: []string{"service1", "service2"},
				priorSetupErrors: []db.SetupErrorCount{
					&setupErrorCount{
						"service1",
						42,
					},
					&setupErrorCount{
						"service2",
						84,
					},
				},
			},
			service:             "service1",
			previousErrorCounts: &serviceErrorCounts{setupFortyTwo, nil, &setupFortyTwo},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 43},
				{"service2", 84},
			},
			expectedPushErrorData: nil,
		},
		{
			helptext: "Single-service push errors, single node, 0 case, no adjustment",
			dbData: dbData{
				services: []string{"service1"},
				nodes:    []string{"node1"},
				priorPushErrors: []db.PushErrorCount{
					&pushErrorCount{
						"service1",
						"node1",
						0,
					},
				},
			},
			service: "service1",
			previousErrorCounts: &serviceErrorCounts{
				pushErrors: map[string]int{
					"node1": 0,
				},
			},
			adjustment:             noop,
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 0},
			},
		},
		// {
		// 	helptext: "Single-service push errors, single node, non-zero case",
		// 	dbData: dbData{
		// 		services: []string{"service1"},
		// 		nodes:    []string{"node1"},
		// 		priorPushErrors: []db.PushErrorCount{
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node1",
		// 				42,
		// 			},
		// 		},
		// 	},
		// 	service: "service1",
		// 	expectedServiceErrorCounts: &serviceErrorCounts{
		// 		0,
		// 		map[string]int{
		// 			"node1": 42,
		// 		},
		// 	},
		// 	expectedShouldTrackErrors: true,
		// },
		// {
		// 	helptext: "Single-service push errors, multiple nodes",
		// 	dbData: dbData{
		// 		services: []string{"service1"},
		// 		nodes:    []string{"node1", "node2"},
		// 		priorPushErrors: []db.PushErrorCount{
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node1",
		// 				42,
		// 			},
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node2",
		// 				84,
		// 			},
		// 		},
		// 	},
		// 	service: "service1",
		// 	expectedServiceErrorCounts: &serviceErrorCounts{
		// 		0,
		// 		map[string]int{
		// 			"node1": 42,
		// 			"node2": 84,
		// 		},
		// 	},
		// 	expectedShouldTrackErrors: true,
		// },
		// {
		// 	helptext: "Multiple-service push errors, multiple nodes, select the right service",
		// 	dbData: dbData{
		// 		services: []string{"service1", "service2"},
		// 		nodes:    []string{"node1", "node2", "node3"},
		// 		priorPushErrors: []db.PushErrorCount{
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node1",
		// 				42,
		// 			},
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node2",
		// 				84,
		// 			},
		// 			&pushErrorCount{
		// 				"service2",
		// 				"node1",
		// 				54,
		// 			},
		// 			&pushErrorCount{
		// 				"service2",
		// 				"node3",
		// 				86,
		// 			},
		// 		},
		// 	},
		// 	service: "service2",
		// 	expectedServiceErrorCounts: &serviceErrorCounts{
		// 		0,
		// 		map[string]int{
		// 			"node1": 54,
		// 			"node3": 86,
		// 		},
		// 	},
		// 	expectedShouldTrackErrors: true,
		// },
		// {
		// 	helptext: "Multiple-service setup and push errors, multiple nodes, select the right service",
		// 	dbData: dbData{
		// 		services: []string{"service1", "service2"},
		// 		nodes:    []string{"node1", "node2", "node3"},
		// 		priorSetupErrors: []db.SetupErrorCount{
		// 			&setupErrorCount{
		// 				"service1",
		// 				34,
		// 			},
		// 			&setupErrorCount{
		// 				"service2",
		// 				85,
		// 			},
		// 		},
		// 		priorPushErrors: []db.PushErrorCount{
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node1",
		// 				42,
		// 			},
		// 			&pushErrorCount{
		// 				"service1",
		// 				"node2",
		// 				84,
		// 			},
		// 			&pushErrorCount{
		// 				"service2",
		// 				"node1",
		// 				54,
		// 			},
		// 			&pushErrorCount{
		// 				"service2",
		// 				"node3",
		// 				86,
		// 			},
		// 		},
		// 	},
		// 	service: "service2",
		// 	expectedServiceErrorCounts: &serviceErrorCounts{
		// 		85,
		// 		map[string]int{
		// 			"node1": 54,
		// 			"node3": 86,
		// 		},
		// 	},
		// 	expectedShouldTrackErrors: true,
		// },
	}

	for _, test := range testCases {
		func() {
			ctx := context.Background()
			dbLocation := path.Join(os.TempDir(), fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
			defer os.Remove(dbLocation)

			m, err := db.OpenOrCreateDatabase(dbLocation)
			if err != nil {
				t.Errorf("Could not create test database: %s", err)
				return
			}
			defer m.Close()

			if err := m.UpdateServices(ctx, test.services); err != nil {
				t.Errorf("Could not update services in test database: %s", err)
				return
			}
			if err := m.UpdateNodes(ctx, test.nodes); err != nil {
				t.Errorf("Could not update nodes in test database: %s", err)
				return
			}
			if err := m.UpdateSetupErrorsTable(ctx, test.priorSetupErrors); err != nil {
				t.Errorf("Could not update setup errors table in test database: %s", err)
				return
			}
			if err := m.UpdatePushErrorsTable(ctx, test.priorPushErrors); err != nil {
				t.Errorf("Could not update push errors table in test database: %s", err)
				return
			}

			// The actual test
			if err = saveErrorCountsInDatabase(ctx, test.service, m, test.adjustment(test.previousErrorCounts)); err != nil {
				t.Errorf("Could not save error counts in database: %s", err)
				return
			}

			testSetupErrors, err := m.GetSetupErrorsInfo(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("Could not get setup error counts from database: %s", err)
				return
			}
			resultSetupSlice := make([]setupErrorCount, 0, len(testSetupErrors))
			for _, val := range testSetupErrors {
				toAdd := setupErrorCount{val.Service(), val.Count()}
				resultSetupSlice = append(resultSetupSlice, toAdd)
			}
			if !testutils.SlicesHaveSameElements(resultSetupSlice, test.expectedSetupErrorData) {
				t.Errorf("Database data does not match expected data for setup errors, test %s.  Expected %v, got %v", test.helptext, test.expectedSetupErrorData, resultSetupSlice)
			}

			testPushErrors, err := m.GetPushErrorsInfo(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("Could not get push error counts from database: %s", err)
				return
			}
			resultPushSlice := make([]pushErrorCount, 0, len(testPushErrors))
			for _, val := range testPushErrors {
				toAdd := pushErrorCount{val.Service(), val.Node(), val.Count()}
				resultPushSlice = append(resultPushSlice, toAdd)
			}
			if !testutils.SlicesHaveSameElements(resultPushSlice, test.expectedPushErrorData) {
				t.Errorf("Database data does not match expected data for push errors, test %s.  Expected %v, got %v", test.helptext, test.expectedSetupErrorData, testSetupErrors)
			}

			// counts, shouldTrackErrors := setErrorCountsByService(ctx, test.service, m)
			// if !reflect.DeepEqual(counts, test.expectedServiceErrorCounts) {
			// 	t.Errorf("Got different serviceErrorCounts than expected for test %s.  Expected %v, got %v", test.helptext, test.expectedServiceErrorCounts, counts)
			// }
			// if shouldTrackErrors != test.expectedShouldTrackErrors {
			// 	t.Errorf("Got different decision about tracking errors than expected for test %s.  Expected %t, got %t", test.helptext, test.expectedShouldTrackErrors, shouldTrackErrors)
			// }
		}()
	}
}

func TestAdjustErrorCountsByServiceAndDirectNotification(t *testing.T) {
	type testCase struct {
		helptext string
		Notification
		errorCounts         *serviceErrorCounts
		notificationMinimum int
		expectedShouldSend  bool
		expectedErrorCounts *serviceErrorCounts
	}

	testCases := []testCase{
		{
			helptext: "No pre-existing errors, get setupError",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors:  map[string]int{},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 1,
				pushErrors:  map[string]int{},
			},
		},
		{
			helptext: "Pre-existing errors, get setupError, not enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 1,
				pushErrors:  map[string]int{},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors:  map[string]int{},
			},
		},
		{
			helptext: "Pre-existing errors, get setupError, enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors:  map[string]int{},
			},
			notificationMinimum: 3,
			expectedShouldSend:  true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors:  map[string]int{},
			},
		},
		{
			helptext: "Pre-existing errors mixed, get setupError, not enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 1,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
		},
		{
			helptext: "Pre-existing errors mixed, get setupError, enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
			notificationMinimum: 3,
			expectedShouldSend:  true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
		},
		{
			helptext: "No pre-existing errors, get pushError on node1",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors:  map[string]int{},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors: map[string]int{
					"node1": 1,
				},
			},
		},
		{
			helptext: "Pre-existing errors, get pushError on node1, not enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors: map[string]int{
					"node1": 1,
				},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 0,
				pushErrors: map[string]int{
					"node1": 2,
				},
			},
		},
		{
			helptext: "Pre-existing errors mixed, get pushError on node1, not enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 1,
					"node2": 0,
					"node3": 2,
				},
			},
			notificationMinimum: 3,
			expectedShouldSend:  false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
		},
		{
			helptext: "Pre-existing errors mixed, get pushError on node1, enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 2,
					"node2": 0,
					"node3": 2,
				},
			},
			notificationMinimum: 3,
			expectedShouldSend:  true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: 2,
				pushErrors: map[string]int{
					"node1": 0,
					"node2": 0,
					"node3": 2,
				},
			},
		},
	}

	for _, test := range testCases {
		result := adjustErrorCountsByServiceAndDirectNotification(test.Notification, test.errorCounts, test.notificationMinimum)
		if result != test.expectedShouldSend {
			t.Errorf("Got wrong decision on whether/not to send notification for test %s. Expected %t, got %t", test.helptext, test.expectedShouldSend, result)
		}
		if !reflect.DeepEqual(test.expectedErrorCounts, test.errorCounts) {
			t.Errorf("Got wrong serviceErrorCounts for test %s.  Expected %v, got %v", test.helptext, test.expectedErrorCounts, test.errorCounts)
		}
	}

}
