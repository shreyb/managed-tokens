package notifications

import "testing"

func TestAddSetupErrorToAdminErrors(t *testing.T) {
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
