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

package main

import (
	"fmt"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestGetWorkerConfigStringSlice(t *testing.T) {
	viper.Reset()
	workerType := "getKerberosTickets"
	key := "myKey"
	expectedValue := []string{"value1", "value2"}

	// Set up the configuration
	viper.Set("workerType."+workerType+"."+key, expectedValue)

	// Call the function
	result := getWorkerConfigStringSlice(workerType, key)

	// Check the result
	assert.Equal(t, expectedValue, result)
}

func TestGetWorkerConfigInt(t *testing.T) {
	viper.Reset()
	workerType := "getKerberosTickets"
	key := "myKey"
	expectedValue := 42

	// Set up the test by mocking the configuration value
	viper.Set("workerType."+workerType+"."+key, expectedValue)

	// Call the function being tested
	value := getWorkerConfigInt(workerType, key)

	// Check if the returned value matches the expected value
	if value != expectedValue {
		t.Errorf("Got wrong value for worker config int. Expected %d, got %d", expectedValue, value)
	}

	// Reset the configuration
	viper.Reset()
}
func TestGetWorkerConfigString(t *testing.T) {
	viper.Reset()
	workerType := "getKerberosTickets"
	key := "myKey"
	expectedValue := "myValue"

	// Set up the configuration
	viper.Set("workerType."+workerType+"."+key, expectedValue)

	// Call the function
	value := getWorkerConfigString(workerType, key)

	// Check the result
	if value != expectedValue {
		t.Errorf("Got wrong value for worker config string. Expected %s, got %s", expectedValue, value)
	}

	// Clean up the configuration
	viper.Reset()
}
func TestGetWorkerConfigValue(t *testing.T) {
	// Set up test cases
	testCases := []struct {
		workerType string
		key        string
		config     map[string]any
		expected   any
	}{
		{
			workerType: "getKerberosTickets",
			key:        "key1",
			config: map[string]any{
				"workerType.getKerberosTickets.key1": "value1",
			},
			expected: "value1",
		},
		{
			workerType: "getKerberosTickets",
			key:        "key2",
			config: map[string]any{
				"workerType.getKerberosTickets.key2": 42,
			},
			expected: 42,
		},
		{
			workerType: "getKerberosTickets",
			key:        "key3",
			config: map[string]any{
				"workerType.getKerberosTickets.key3": []string{"value1", "value2"},
			},
			expected: []string{"value1", "value2"},
		},
		{
			workerType: "getKerberosTickets",
			key:        "key4",
			config:     map[string]any{},
			expected:   nil,
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("WorkerType: %s, Key: %s", tc.workerType, tc.key), func(t *testing.T) {
			// Set up test environment
			viper.Reset()
			for k, v := range tc.config {
				viper.Set(k, v)
			}

			// Call the function
			result := getWorkerConfigValue(tc.workerType, tc.key)

			// Check the result
			assert.Equal(t, tc.expected, result)
			// if !reflect.DeepEqual(result, tc.expected) {
			// 	t.Errorf("Unexpected result. Expected: %v, Got: %v", tc.expected, result)
			// }
		})
	}
}

func TestIsValidWorkerTypeString(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{
			input:    "GetKerberosTickets",
			expected: true,
		},
		{
			input:    "StoreAndGetTokenInteractive",
			expected: true,
		},
		{
			input:    "StoreAndGetToken",
			expected: true,
		},
		{
			input:    "PingAggregator",
			expected: true,
		},
		{
			input:    "PushTokens",
			expected: true,
		},
		{
			input:    "UnknownWorkerType",
			expected: false,
		},
	}

	for _, test := range tests {
		result := isValidWorkerTypeString(test.input)
		assert.Equal(t, test.expected, result)
	}
}
