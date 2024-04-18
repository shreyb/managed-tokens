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
