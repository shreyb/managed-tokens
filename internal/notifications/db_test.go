package notifications

import (
	"context"
	"database/sql"

	// "database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"reflect"
	"testing"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/testUtils"
)

// TestSetErrorCountsByServiceNilDBCase checks that if we have a nil ManagedTokensDatabase, that setErrorCountsByService returns a nil
// serviceErrorCounts and a boolean indicating that we should not track error counts
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

// TestSetErrorCountsByService tests the non-nil ManagedTokensDatabase cases, and makes sure that in various combinations of the
// error counts stored in the ManagedTokensDatabase, that serviceErrorCounts is set correctly.
func TestSetErrorCountsByService(t *testing.T) {
	type dbData struct {
		services         []string
		nodes            []string
		priorSetupErrors []db.SetupErrorCount
		priorPushErrors  []db.PushErrorCount
	}

	type testCase struct {
		description string
		dbData
		service                    string
		expectedServiceErrorCounts *serviceErrorCounts
		expectedShouldTrackErrors  bool
	}

	testCases := []testCase{
		{
			description:                "No prior data",
			dbData:                     dbData{},
			service:                    "service1",
			expectedServiceErrorCounts: &serviceErrorCounts{},
			expectedShouldTrackErrors:  true,
		},
		{
			description: "Only single-service setup errors, 0 case",
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
			expectedServiceErrorCounts: &serviceErrorCounts{errorCount{0, false}, nil},
			expectedShouldTrackErrors:  true,
		},
		{
			description: "Only single-service setup errors, nonzero case",
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
			expectedServiceErrorCounts: &serviceErrorCounts{errorCount{42, false}, nil},
			expectedShouldTrackErrors:  true,
		},
		{
			description: "Multiple service setup errors, pick the right one",
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
			expectedServiceErrorCounts: &serviceErrorCounts{errorCount{42, false}, nil},
			expectedShouldTrackErrors:  true,
		},
		{
			description: "Single-service push errors, single node, 0 case",
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
				errorCount{},
				map[string]errorCount{
					"node1": {0, false},
				},
			},
			expectedShouldTrackErrors: true,
		},
		{
			description: "Single-service push errors, single node, non-zero case",
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
				errorCount{},
				map[string]errorCount{
					"node1": {42, false},
				},
			},
			expectedShouldTrackErrors: true,
		},
		{
			description: "Single-service push errors, multiple nodes",
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
				errorCount{},
				map[string]errorCount{
					"node1": {42, false},
					"node2": {84, false},
				},
			},
			expectedShouldTrackErrors: true,
		},
		{
			description: "Multiple-service push errors, multiple nodes, select the right service",
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
				errorCount{},
				map[string]errorCount{
					"node1": {54, false},
					"node3": {86, false},
				},
			},
			expectedShouldTrackErrors: true,
		},
		{
			description: "Multiple-service setup and push errors, multiple nodes, select the right service",
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
				errorCount{85, true},
				map[string]errorCount{
					"node1": {54, false},
					"node3": {86, false},
				},
			},
			expectedShouldTrackErrors: true,
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				ctx := context.Background()
				m, err := createAndPrepareDatabaseForTesting(tempDir, test.services, test.nodes, test.priorSetupErrors, test.priorPushErrors)
				if err != nil {
					t.Errorf("Error creating and preparing the database: %s", err)
				}
				defer m.Close()

				counts, shouldTrackErrors := setErrorCountsByService(ctx, test.service, m)
				if !reflect.DeepEqual(counts.setupErrors, test.expectedServiceErrorCounts.setupErrors) && !reflect.DeepEqual(counts.pushErrors, test.expectedServiceErrorCounts.pushErrors) {
					t.Errorf("Got different serviceErrorCounts than expected for test %s.  Expected %v, got %v", test.description, test.expectedServiceErrorCounts, counts)
				}
				if shouldTrackErrors != test.expectedShouldTrackErrors {
					t.Errorf("Got different decision about tracking errors than expected for test %s.  Expected %t, got %t", test.description, test.expectedShouldTrackErrors, shouldTrackErrors)
				}
			},
		)
	}
}

// TestSaveErrorCountsInDatabase checks that given various prior error counts and adjustments to those error counts, that
// saveErrorCountsInDatabase stores the correct error counts in the ManagedTokensDatabase
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
		description string
		dbData
		service                string
		previousErrorCounts    *serviceErrorCounts
		adjustment             func(ec *serviceErrorCounts) *serviceErrorCounts
		expectedSetupErrorData []setupErrorCount
		expectedPushErrorData  []pushErrorCount
	}

	noop := func(ec *serviceErrorCounts) *serviceErrorCounts { return ec }

	adjustSetupErrorsByOne := func(ec *serviceErrorCounts) *serviceErrorCounts {
		ec.setupErrors.set(ec.setupErrors.value + 1)
		return ec
	}

	adjustPushErrorsByOneForNode := func(node string) func(*serviceErrorCounts) *serviceErrorCounts {
		return func(ec *serviceErrorCounts) *serviceErrorCounts {
			ec.pushErrors[node] = errorCount{ec.pushErrors[node].value + 1, true}
			return ec
		}
	}

	testCases := []testCase{
		{
			description: "No prior data, no adjustment",
			dbData:      dbData{},
			service:     "service1",
			previousErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{},
				pushErrors:  nil,
			},
			adjustment:             noop,
			expectedSetupErrorData: nil,
			expectedPushErrorData:  nil,
		},
		{
			description: "Only single-service setup errors, 0 case, no adjustment",
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
			previousErrorCounts: &serviceErrorCounts{errorCount{0, false}, nil},
			adjustment:          noop,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 0},
			},
			expectedPushErrorData: nil,
		},
		{
			description: "Only single-service setup errors, nonzero case, no adjustment",
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
			previousErrorCounts: &serviceErrorCounts{errorCount{42, false}, nil},
			adjustment:          noop,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 0},
			},
			expectedPushErrorData: nil,
		},
		{
			description: "Only single-service setup errors, 0 case, adjustment of SetupErrorsCount by 1",
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
			previousErrorCounts: &serviceErrorCounts{errorCount{0, false}, nil},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 1},
			},
			expectedPushErrorData: nil,
		},
		{
			description: "Only single-service setup errors, nonzero case, adjustment of SetupErrorsCount by 1",
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
			previousErrorCounts: &serviceErrorCounts{errorCount{42, false}, nil},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 43},
			},
			expectedPushErrorData: nil,
		},
		{
			description: "Multiple service setup errors, adjust only setup errors by 1 of the correct service",
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
			previousErrorCounts: &serviceErrorCounts{errorCount{42, false}, nil},
			adjustment:          adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 43},
				{"service2", 84},
			},
			expectedPushErrorData: nil,
		},
		{
			description: "Multiple-service setup and push errors, multiple nodes, adjust setup errors",
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
			previousErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{85, false},
				pushErrors: map[string]errorCount{
					"node1": {54, false},
					"node3": {86, false},
				},
			},
			adjustment: adjustSetupErrorsByOne,
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 34},
				{"service2", 86},
			},
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 42},
				{"service1", "node2", 84},
				{"service2", "node1", 0},
				{"service2", "node3", 0},
			},
		},
		{
			description: "Single-service push errors, single node, 0 case, no adjustment",
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
				pushErrors: map[string]errorCount{
					"node1": {0, false},
				},
			},
			adjustment:             noop,
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 0},
			},
		},
		{
			description: "Single-service push errors, single node, 0 case, adjustment to pushErrors",
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
				pushErrors: map[string]errorCount{
					"node1": {0, false},
				},
			},
			adjustment:             adjustPushErrorsByOneForNode("node1"),
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 1},
			},
		},
		{
			description: "Single-service push errors, single node, non-zero case, adjust pushErrors",
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
			previousErrorCounts: &serviceErrorCounts{
				pushErrors: map[string]errorCount{
					"node1": {42, false},
				},
			},
			adjustment:             adjustPushErrorsByOneForNode("node1"),
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 43},
			},
		},
		{
			description: "Single-service push errors, multiple nodes",
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
			previousErrorCounts: &serviceErrorCounts{
				pushErrors: map[string]errorCount{
					"node1": {42, false},
					"node2": {84, false},
				},
			},
			adjustment:             adjustPushErrorsByOneForNode("node1"),
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 43},
				{"service1", "node2", 0},
			},
		},
		{
			description: "Multiple-service push errors, multiple nodes, select the right service, adjust pushErrors",
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
			previousErrorCounts: &serviceErrorCounts{
				pushErrors: map[string]errorCount{
					"node1": {54, false},
					"node3": {86, false},
				},
			},
			adjustment:             adjustPushErrorsByOneForNode("node1"),
			expectedSetupErrorData: nil,
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 42},
				{"service1", "node2", 84},
				{"service2", "node1", 55},
				{"service2", "node3", 0},
			},
		},
		{
			description: "Multiple-service setup and push errors, multiple nodes, select the right service, adjust push Errors",
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
			previousErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{85, false},
				pushErrors: map[string]errorCount{
					"node1": {54, false},
					"node3": {86, false},
				},
			},
			adjustment: adjustPushErrorsByOneForNode("node1"),
			expectedSetupErrorData: []setupErrorCount{
				{"service1", 34},
				{"service2", 0},
			},
			expectedPushErrorData: []pushErrorCount{
				{"service1", "node1", 42},
				{"service1", "node2", 84},
				{"service2", "node1", 55},
				{"service2", "node3", 0},
			},
		},
	}

	tempDir := t.TempDir()
	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				ctx := context.Background()
				m, err := createAndPrepareDatabaseForTesting(tempDir, test.services, test.nodes, test.priorSetupErrors, test.priorPushErrors)
				if err != nil {
					t.Errorf("Error creating and preparing the database: %s", err)
				}
				defer m.Close()

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
				if !testUtils.SlicesHaveSameElements(resultSetupSlice, test.expectedSetupErrorData) {
					t.Errorf("Database data does not match expected data for setup errors, test %s.  Expected %v, got %v", test.description, test.expectedSetupErrorData, resultSetupSlice)
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
				if !testUtils.SlicesHaveSameElements(resultPushSlice, test.expectedPushErrorData) {
					t.Errorf("Database data does not match expected data for push errors, test %s.  Expected %v, got %v", test.description, test.expectedPushErrorData, testSetupErrors)
				}
			},
		)
	}
}

// TestAdjustErrorCountsByServiceAndDirectNotification checks that adjustErrorCountsByServiceAndDirectNotification properly sets
// the errorCount values given various prior error counts and a Notification to process
func TestAdjustErrorCountsByServiceAndDirectNotification(t *testing.T) {
	type testCase struct {
		description string
		Notification
		errorCounts             *serviceErrorCounts
		errorCountToSendMessage int
		expectedShouldSend      bool
		expectedErrorCounts     *serviceErrorCounts
	}

	testCases := []testCase{
		{
			description: "No pre-existing errors, get setupError",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, false},
				pushErrors:  map[string]errorCount{},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{1, true},
				pushErrors:  map[string]errorCount{},
			},
		},
		{
			description: "Pre-existing errors, get setupError, not enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{1, true},
				pushErrors:  map[string]errorCount{},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors:  map[string]errorCount{},
			},
		},
		{
			description: "Pre-existing errors, get setupError, enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors:  map[string]errorCount{},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors:  map[string]errorCount{},
			},
		},
		{
			description: "Pre-existing errors mixed, get setupError, not enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{1, true},
				pushErrors: map[string]errorCount{
					"node1": {2, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {2, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
		},
		{
			description: "Pre-existing errors mixed, get setupError, enough for threshhold",
			Notification: &setupError{
				"This is a setup error",
				"service1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {2, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors: map[string]errorCount{
					"node1": {2, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
		},
		{
			description: "No pre-existing errors, get pushError on node1",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors:  map[string]errorCount{},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors: map[string]errorCount{
					"node1": {1, true},
				},
			},
		},
		{
			description: "Pre-existing errors, get pushError on node1, not enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors: map[string]errorCount{
					"node1": {1, false},
				},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{0, true},
				pushErrors: map[string]errorCount{
					"node1": {2, true},
				},
			},
		},
		{
			description: "Pre-existing errors mixed, get pushError on node1, not enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {1, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      false,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {2, true},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
		},
		{
			description: "Pre-existing errors mixed, get pushError on node1, enough for threshhold",
			Notification: &pushError{
				"This is a push error",
				"service1",
				"node1",
			},
			errorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {2, false},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
			errorCountToSendMessage: 3,
			expectedShouldSend:      true,
			expectedErrorCounts: &serviceErrorCounts{
				setupErrors: errorCount{2, true},
				pushErrors: map[string]errorCount{
					"node1": {0, true},
					"node2": {0, false},
					"node3": {2, false},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				result := adjustErrorCountsByServiceAndDirectNotification(test.Notification, test.errorCounts, test.errorCountToSendMessage)
				if result != test.expectedShouldSend {
					t.Errorf("Got wrong decision on whether/not to send notification for test %s. Expected %t, got %t", test.description, test.expectedShouldSend, result)
				}
				if !reflect.DeepEqual(test.expectedErrorCounts, test.errorCounts) {
					t.Errorf("Got wrong serviceErrorCounts for test %s.  Expected %v, got %v", test.description, test.expectedErrorCounts, test.errorCounts)
				}
			},
		)
	}
}

// createAndPrepareDatabaseForTesting is a helper function to prepare a fresh ManagedTokensDatabase for testing.
func createAndPrepareDatabaseForTesting(tempDir string, testServices, testNodes []string, testPriorSetupErrors []db.SetupErrorCount, testPriorPushErrors []db.PushErrorCount) (*db.ManagedTokensDatabase, error) {
	ctx := context.Background()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))

	m, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		return nil, fmt.Errorf("Could not create test database: %s", err)
	}
	if err := m.UpdateServices(ctx, testServices); err != nil {
		return nil, fmt.Errorf("Could not update services in test database: %s", err)
	}
	if err := m.UpdateNodes(ctx, testNodes); err != nil {
		return nil, fmt.Errorf("Could not update nodes in test database: %s", err)
	}
	if err := m.UpdateSetupErrorsTable(ctx, testPriorSetupErrors); err != nil {
		return nil, fmt.Errorf("Could not update setup errors table in test database: %s", err)
	}
	if err := m.UpdatePushErrorsTable(ctx, testPriorPushErrors); err != nil {
		return nil, fmt.Errorf("Could not update push errors table in test database: %s", err)
	}

	return m, nil
}
