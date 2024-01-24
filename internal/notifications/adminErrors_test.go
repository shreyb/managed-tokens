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

package notifications

import (
	"sync"
	"testing"
)

// TestAddSetupErrorToAdminErrors makes sure that setup errors get added to the AdminErrors global var properly
func TestAddSetupErrorToAdminErrors(t *testing.T) {
	adminErrors = packageErrors{}
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

				val, ok := adminErrors.errorsMap.Load(test.service)
				if !ok {
					t.Error("Test error not loaded")
				}
				found := false
				if admData, ok := val.(*adminData); ok {
					for _, setupErr := range admData.SetupErrors {
						if setupErr == test.notificationMsg {
							found = true
							break
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

// TestAddPushErrorToAdminErrors checks that push errors get added to the AdminErrors global variable properly
func TestAddPushErrorToAdminErrors(t *testing.T) {
	adminErrors = packageErrors{}
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

				val, ok := adminErrors.errorsMap.Load(test.service)
				if !ok {
					t.Error("Test error not stored in adminErrors")
				}
				if admData, ok := val.(*adminData); ok {
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

// TestIsEmpty checks the adminData.isEmpty method to make sure that it returns the expected result, given a few different pieces of test data
func TestIsEmpty(t *testing.T) {
	type testCase struct {
		description    string
		loaderFunc     func() *adminData
		expectedResult bool
	}

	testCases := []testCase{
		{
			"Empty adminData",
			func() *adminData {
				return &adminData{}
			},
			true,
		},
		{
			"adminData has empty non-nil setupErrors",
			func() *adminData {
				return &adminData{
					SetupErrors: []string{},
				}
			},
			true,
		},
		{
			"adminData has populated SetupErrors",
			func() *adminData {
				return &adminData{
					SetupErrors: []string{"Test error"},
				}
			},
			false,
		},
		{
			"adminData has nil PushErrors",
			func() *adminData {
				return &adminData{
					PushErrors: sync.Map{},
				}
			},
			true,
		},
		{
			"adminData has populated PushErrors",
			func() *adminData {
				var a adminData
				for i := 1; i <= 10; i++ {
					a.PushErrors.Store(i, struct{}{})
				}
				return &a
			},
			false,
		},
		{
			"adminData has populated SetupErrors and PushErrors",
			func() *adminData {
				var a adminData
				for i := 1; i <= 10; i++ {
					a.PushErrors.Store(i, struct{}{})
				}
				a.SetupErrors = []string{"This is an error"}
				return &a
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
