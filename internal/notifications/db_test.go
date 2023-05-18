package notifications

import (
	"reflect"
	"testing"
)

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
