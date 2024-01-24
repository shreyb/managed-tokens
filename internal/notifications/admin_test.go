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
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestPrepareAdminErrorsForFull Message checks that a set of setup errors and push errors gets properly translated into a table for sending notifications
func TestPrepareAdminErrorsForFullMessage(t *testing.T) {
	adminErrors = packageErrors{}
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

	finalTestData := prepareAdminErrorsForFullMessage()

	for service, serviceData := range finalCheckData {
		// Check SetupErrors
		if !reflect.DeepEqual(finalTestData[service].SetupErrors, serviceData.SetupErrors) {
			t.Errorf(
				"Final test data did not match expected data.  Expected %v, got %v",
				finalCheckData,
				finalTestData,
			)
		}

		// Check pushErrors - make sure node, error message in each slice
		for _, n := range notifications {
			if pushErr, ok := n.(*pushError); ok {
				if !strings.Contains(finalTestData[pushErr.service].PushErrorsTable, pushErr.node) {
					t.Errorf(
						"Expected node %s to be in the test push errors table for service %s",
						pushErr.node,
						pushErr.service,
					)
				}
				if !strings.Contains(finalTestData[pushErr.service].PushErrorsTable, pushErr.message) {
					t.Errorf(
						"Expected message %s to be in the test push errors table for service %s",
						pushErr.message,
						pushErr.service,
					)
				}

			}
		}
	}
}

// TestPrepareAbridgedAdminSlice checks that a packageErrors object gets properly translated into the proper slices
func TestPrepareAbridgedAdminSlice(t *testing.T) {
	adminErrors = packageErrors{}
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

	// Populate test data into adminErrors
	for _, n := range notifications {
		addErrorToAdminErrors(n)
	}

	expectedSetupErrors := []string{
		"test_service1: Setup error 1",
		"test_service1: Setup error 2",
		"test_service2: Setup error 1",
	}
	expectedPushErrors := []string{
		"test_service1@node1: Push error 1",
		"test_service1@node2: Push error 1",
		"test_service2@node3: Push error 1",
	}

	testSetupErrors, testPushErrors := prepareAbridgedAdminSlices()
	sort.Slice(testSetupErrors, func(i, j int) bool { return testSetupErrors[i] < testSetupErrors[j] })
	sort.Slice(testPushErrors, func(i, j int) bool { return testPushErrors[i] < testPushErrors[j] })
	sort.Slice(expectedSetupErrors, func(i, j int) bool { return expectedSetupErrors[i] < expectedSetupErrors[j] })
	if !reflect.DeepEqual(testSetupErrors, expectedSetupErrors) {
		t.Errorf("Setup Errors slice did not match.  Expected %s, got %s", expectedSetupErrors, testSetupErrors)
	}
	if !reflect.DeepEqual(testPushErrors, expectedPushErrors) {
		t.Errorf("Push Errors slice did not match.  Expected %s, got %s", expectedPushErrors, testPushErrors)
	}
}
