package notifications

import (
	"reflect"
	"testing"
)

func TestAdjustErrorCountsByServiceAndDirectNotification(t *testing.T) {
	type testCase struct {
		Notification
		errorCounts         *serviceErrorCounts
		notificationMinimum int
		expectedShouldSend  bool
		expectedErrorCounts *serviceErrorCounts
	}

	testCases := []testCase{
		{
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
	}

	for _, test := range testCases {
		result := adjustErrorCountsByServiceAndDirectNotification(test.Notification, test.errorCounts, test.notificationMinimum)
		if result != test.expectedShouldSend {
			t.Errorf("Got wrong decision on whether/not to send notification. Expected %t, got %t", test.expectedShouldSend, result)
		}
		// if test.expectedErrorCounts.setupErrors != test.errorCounts.setupErrors {
		if !reflect.DeepEqual(test.expectedErrorCounts, test.errorCounts) {
			t.Errorf("Got wrong serviceErrorCounts.  Expected %v, got %v", test.expectedErrorCounts, test.errorCounts)
		}
	}

}
