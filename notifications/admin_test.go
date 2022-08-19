package notifications

import (
	"reflect"
	"sync"
	"testing"
)

func TestAddSetupErrorToAdminErrors(t *testing.T) {
	adminErrors = sync.Map{}
	type testCase struct {
		description     string
		service         string
		notificationMsg string
	}

	testCases := []testCase{
		{
			"Add first notification",
			"test_service",
			"There was an error",
		},
		{
			"Add second notification to same service",
			"test_service",
			"There is another error",
		},
		{
			"Add notification to different service",
			"test_service2",
			"Yet another error",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				n := &setupError{
					message: test.notificationMsg,
					service: test.service,
				}

				addErrorToAdminErrors(n)

				val, ok := adminErrors.Load(test.service)
				if !ok {
					t.Error("Test error not loaded")
				}
				found := false
				if admData, ok := val.(adminData); ok {
					for _, setupErr := range admData.SetupErrors {
						if setupErr == test.notificationMsg {
							found = true
						}
					}
					if !found {
						t.Errorf(
							"Expected to find %s stored in adminErrors[\"test_service\"].SetupErrors.  Got %v instead",
							test.notificationMsg,
							admData.SetupErrors,
						)
					}
				} else {
					t.Errorf("Wrong data type stored in adminErrors: %T", val)
				}
			},
		)
	}
}

func TestAddPushErrorToAdminErrors(t *testing.T) {
	adminErrors = sync.Map{}
	type testCase struct {
		description     string
		service         string
		node            string
		notificationMsg string
	}

	testCases := []testCase{
		{
			"Add first notification",
			"test_service",
			"node1",
			"There was an error",
		},
		{
			"Add second notification to same service, same node",
			"test_service",
			"node1",
			"There is another error",
		},
		{
			"Add third notification to same service, different node",
			"test_service",
			"node2",
			"Error here too",
		},
		{
			"Add notification to different service",
			"test_service2",
			"node3",
			"Yet another error",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				n := &pushError{
					message: test.notificationMsg,
					service: test.service,
					node:    test.node,
				}

				addErrorToAdminErrors(n)

				val, ok := adminErrors.Load(test.service)
				if !ok {
					t.Error("Test error not stored in adminErrors")
				}
				if admData, ok := val.(adminData); ok {
					pushErrorsDatum, ok := admData.PushErrors.Load(test.node)
					if !ok {
						t.Error("Test error not stored in adminErrors.pushErrors")
					}
					if storedPushError, ok := pushErrorsDatum.(string); ok {
						if storedPushError != test.notificationMsg {
							t.Errorf(
								"Test error does not match stored error.  Expected %s, got %s",
								test.notificationMsg,
								storedPushError,
							)
						}
					} else {
						t.Errorf(
							"Wrong type stored in pushErrors sync.Map.  Expected string, got %T",
							pushErrorsDatum,
						)
					}
				}
			},
		)
	}
}

func TestPrepareAdminErrorsForMessage(t *testing.T) {
	adminErrors = sync.Map{}
	finalCheckData := make(map[string]AdminDataFinal)
	notifications := []Notification{
		&setupError{
			message: "Setup error 1",
			service: "test_service1",
		},
		&setupError{
			message: "Setup error 2",
			service: "test_service1",
		},
		&setupError{
			message: "Setup error 1",
			service: "test_service2",
		},
		&pushError{
			message: "Push error 1",
			node:    "node1",
			service: "test_service1",
		},
		&pushError{
			message: "Push error 1",
			node:    "node2",
			service: "test_service1",
		},
		&pushError{
			message: "Push error 1",
			node:    "node3",
			service: "test_service2",
		},
	}

	pushErrorsMap := make(map[string]map[string]string)
	// Prepare final test data
	for _, n := range notifications {

		data, ok := finalCheckData[n.GetService()]
		if !ok {
			finalCheckData[n.GetService()] = AdminDataFinal{}
		}

		switch notificationValue := n.(type) {
		case *setupError:
			data.SetupErrors = append(data.SetupErrors, notificationValue.message)
			finalCheckData[notificationValue.service] = data
		case *pushError:
			// Populate pushErrorsMap for processing below
			_, ok := pushErrorsMap[notificationValue.service]
			if !ok {
				pushErrorsMap[notificationValue.service] = make(map[string]string)
			}
			pushErrorsMap[notificationValue.service][notificationValue.node] = notificationValue.message
		}
	}

	for service, data := range finalCheckData {
		data.PushErrorsTable = PrepareTableStringFromMap(
			pushErrorsMap[service],
			"The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:",
			[]string{"Node", "Error"},
		)
		finalCheckData[service] = data
	}

	// Populate test data into adminErrors
	for _, n := range notifications {
		addErrorToAdminErrors(n)
	}

	finalTestData := prepareAdminErrorsForMessage()

	if !reflect.DeepEqual(finalTestData, finalCheckData) {
		t.Errorf(
			"Final test data did not match expected data.  Expected %v, got %v",
			finalCheckData,
			finalTestData,
		)
	}

}

func TestIsEmpty(t *testing.T) {
	type testCase struct {
		description    string
		loaderFunc     func() adminData
		expectedResult bool
	}

	testCases := []testCase{
		{
			"Empty adminData",
			func() adminData {
				return adminData{}
			},
			true,
		},
		{
			"adminData has empty non-nil setupErrors",
			func() adminData {
				return adminData{
					SetupErrors: []string{},
				}
			},
			true,
		},
		{
			"adminData has populated SetupErrors",
			func() adminData {
				return adminData{
					SetupErrors: []string{"Test error"},
				}
			},
			false,
		},
		{
			"adminData has nil PushErrors",
			func() adminData {
				var m sync.Map
				return adminData{
					PushErrors: m,
				}
			},
			true,
		},
		{
			"adminData has populated PushErrors",
			func() adminData {
				var m sync.Map
				for i := 1; i <= 10; i++ {
					m.Store(i, struct{}{})
				}
				return adminData{
					PushErrors: m,
				}
			},
			false,
		},
		{
			"adminData has populated SetupErrors and PushErrors",
			func() adminData {
				var m sync.Map
				for i := 1; i <= 10; i++ {
					m.Store(i, struct{}{})
				}
				return adminData{
					SetupErrors: []string{"This is an error"},
					PushErrors:  m,
				}
			},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				testData := test.loaderFunc()
				if result := testData.isEmpty(); result != test.expectedResult {
					t.Errorf(
						"Got wrong result for isEmpty.  Expected %t, got %t",
						test.expectedResult,
						result,
					)
				}
			},
		)
	}
}
