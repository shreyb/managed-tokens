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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/testUtils"
)

func TestInitServices(t *testing.T) {
	testServicesConfig := `
	{
		"experiments": {
			"experiment1": {
				"emails": ["email1@example.com"],
				"roles": {
					"role1": {
						"account": "account1",
						"destinationNodes": ["node1.domain"]
					},
					"role2": {
						"account": "account2",
						"destinationNodes": ["node1.domain"]
					}
				}
			},
			"experiment2": {
				"emails": ["email2@example.com"],
				"roles": {
					"role1": {
						"account": "account3",
						"destinationNodes": ["node2.domain"]
					}
				}
			}
		}
	}
	`
	type testCase struct {
		description          string
		experimentArg        string
		serviceArg           string
		expectedServiceNames []string
	}

	testCases := []testCase{
		{
			"Nothing given",
			"",
			"",
			[]string{"experiment1_role1", "experiment1_role2", "experiment2_role1"},
		},
		{
			"Experiment given",
			"experiment1",
			"",
			[]string{"experiment1_role1", "experiment1_role2"},
		},
		{
			"Service given",
			"",
			"experiment1_role2",
			[]string{"experiment1_role2"},
		},
		{
			"Experiment and Service given",
			"experiment2",
			"experiment1_role2",
			[]string{"experiment2_role1"},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				servicesReset()
				defer servicesReset()

				configReader := strings.NewReader(testServicesConfig)
				viper.SetConfigType("json")
				err := viper.ReadConfig(configReader)
				if err != nil {
					t.Error(err)
				}

				if test.experimentArg != "" {
					viper.Set("experiment", test.experimentArg)
				}
				if test.serviceArg != "" {
					viper.Set("service", test.serviceArg)
				}
				initServices()
				results := make([]string, 0, len(services))
				for _, s := range services {
					results = append(results, s.Name())
				}
				if !testUtils.SlicesHaveSameElementsOrdered[string](results, test.expectedServiceNames) {
					t.Errorf("Didn't get expected service names from initServices.  Expected %v, got %v.", test.expectedServiceNames, results)
				}
			},
		)
	}

}

func servicesReset() {
	reset()
	services = []service.Service{}
}

func TestInitTimeoutsTooLargeTimeouts(t *testing.T) {
	type testCase struct {
		description      string
		timeoutsConfig   string
		expectedErrNil   bool
		expectedTimeouts map[string]time.Duration
	}

	testCases := []testCase{
		{
			"No timeouts given",
			"",
			true,
			map[string]time.Duration{
				"global":      time.Duration(300 * time.Second),
				"kerberos":    time.Duration(20 * time.Second),
				"vaultstorer": time.Duration(60 * time.Second),
				"ping":        time.Duration(10 * time.Second),
				"push":        time.Duration(30 * time.Second),
			},
		},
		{
			"Some timeouts given",
			`
{
	"timeouts": {
		"globalTimeout": "360s",
		"kerberosTimeout": "15s",
		"vaultStorerTimeout": "15s"
	}
}
			`,
			true,
			map[string]time.Duration{
				"global":      time.Duration(360 * time.Second),
				"kerberos":    time.Duration(15 * time.Second),
				"vaultstorer": time.Duration(15 * time.Second),
				"ping":        time.Duration(10 * time.Second),
				"push":        time.Duration(30 * time.Second),
			},
		},
		{
			"Invalid timeouts given (sum exceeds global)",
			`
{
	"timeouts": {
		"globalTimeout": "300s",
		"kerberosTimeout": "20s",
		"vaultStorerTimeout": "360s"
	}
}
			`,
			false,
			nil,
		},
		{
			"Invalid timeouts given (bad parsing)",
			`
{
	"timeouts": {
		"globalTimeout": "300s",
		"kerberosTimeout": "20s",
		"vaultStorerTimeout": "60ssss"
	}
}
			`,
			true,
			map[string]time.Duration{
				"global":      time.Duration(300 * time.Second),
				"kerberos":    time.Duration(20 * time.Second),
				"vaultstorer": time.Duration(60 * time.Second),
				"ping":        time.Duration(10 * time.Second),
				"push":        time.Duration(30 * time.Second),
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				timeoutsReset()
				defer timeoutsReset()

				if test.timeoutsConfig != "" {
					configReader := strings.NewReader(test.timeoutsConfig)
					viper.SetConfigType("json")
					err := viper.ReadConfig(configReader)
					if err != nil {
						t.Error(err)
					}
				}

				err := initTimeouts()

				if !test.expectedErrNil {
					if err != nil {
						return
					} else {
						t.Errorf("Expected timeout check to fail.  It passed. %v", timeouts)
						return
					}
				}
				if err != nil {
					t.Errorf("Expected nil error.  Got %s", err)
					return
				}
				if !reflect.DeepEqual(test.expectedTimeouts, timeouts) {
					t.Errorf("Got different timeout maps.  Expected %v, got %v", test.expectedTimeouts, timeouts)
				}
			},
		)
	}
}

func timeoutsReset() {
	reset()
	timeouts = map[string]time.Duration{
		"global":      time.Duration(300 * time.Second),
		"kerberos":    time.Duration(20 * time.Second),
		"vaultstorer": time.Duration(60 * time.Second),
		"ping":        time.Duration(10 * time.Second),
		"push":        time.Duration(30 * time.Second),
	}
}
