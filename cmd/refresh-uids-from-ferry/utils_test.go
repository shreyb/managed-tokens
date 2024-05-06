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
	"os"
	"strings"
	"testing"

	"github.com/fermitools/managed-tokens/internal/testUtils"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func reset() {
	viper.Reset()
	devEnvironmentLabel = ""
}

func TestGetAllAccountsFromConfig(t *testing.T) {
	// Set up test data
	viper.Set("experiments", map[string]interface{}{
		"experiment1": map[string]interface{}{
			"roles": map[string]interface{}{
				"role1": map[string]interface{}{
					"account": "account1",
				},
				"role2": map[string]interface{}{
					"account": "account2",
				},
			},
		},
		"experiment2": map[string]interface{}{
			"roles": map[string]interface{}{
				"role3": map[string]interface{}{
					"account": "account3",
				},
			},
		},
	})

	// Call the function under test
	accounts := getAllAccountsFromConfig()

	// Assert the expected results
	expected := []string{"account1", "account2", "account3"}
	assert.Equal(t, len(expected), len(accounts))
	assert.True(t, testUtils.SlicesHaveSameElementsOrdered(accounts, expected))
}

func TestGetDevEnvironmentLabel(t *testing.T) {
	type testCase struct {
		description   string
		envSetup      func()
		configSetup   func()
		expectedValue string
	}

	configSetFromFakeConfig := func() {
		fakeFileText := strings.NewReader(`{"devEnvironmentLabel": "test_config"}`)
		viper.SetConfigType("json")
		viper.ReadConfig(fakeFileText)
	}

	testCases := []testCase{
		{
			"Environment variable is set",
			func() { t.Setenv("MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL", "test_env") },
			nil,
			"test_env",
		},
		{
			"Config file has dev label",
			nil,
			configSetFromFakeConfig,
			"test_config",
		},
		{
			"Neither env nor config file has dev label",
			nil,
			nil,
			devEnvironmentLabelDefault,
		},
		{
			"Both env and config file have dev label",
			func() { t.Setenv("MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL", "test_env") },
			configSetFromFakeConfig,
			"test_env",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			reset()
			if tc.envSetup != nil {
				tc.envSetup()
				defer os.Unsetenv("MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL")
			}
			if tc.configSetup != nil {
				tc.configSetup()
			}

			result := getDevEnvironmentLabel()
			assert.Equal(t, tc.expectedValue, result)
		})
	}
}
