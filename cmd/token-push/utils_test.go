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
	"strings"
	"testing"

	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestGetPrometheusJobName(t *testing.T) {
	type testCase struct {
		description           string
		jobNameSetting        string
		devEnvironmentLabel   string
		expectedJobNameResult string
	}

	testCases := []testCase{
		{
			"Completely default",
			"",
			devEnvironmentLabelDefault,
			"managed_tokens",
		},
		{
			"Set different jobname than default",
			"myjobname",
			devEnvironmentLabelDefault,
			"myjobname",
		},
		{
			"Set different devEnvLabel than default",
			"",
			"test",
			"managed_tokens_test",
		},
		{
			"Set different jobname and devEnvLabel than default",
			"myjobname",
			"test",
			"myjobname_test",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				reset()
				defer reset()
				if test.jobNameSetting != "" {
					viper.Set("prometheus.jobname", test.jobNameSetting)
				}
				if test.devEnvironmentLabel != "" {
					devEnvironmentLabel = test.devEnvironmentLabel
				}
				if result := getPrometheusJobName(); result != test.expectedJobNameResult {
					t.Errorf("Got wrong jobname.  Expected %s, got %s", test.expectedJobNameResult, result)
				}
			},
		)
	}
}

func TestResolveDisableNotifications(t *testing.T) {
	servicesStringSlice := []string{"experiment1_role1", "experiment2_role1", "experiment3_role1"}
	services := make([]service.Service, 0, len(servicesStringSlice))
	for _, s := range servicesStringSlice {
		services = append(services, service.NewService(s))
	}

	type testCase struct {
		description                     string
		serviceSlice                    []service.Service
		viperConfig                     string
		expectedBlockAdminNotifications bool
		expectedNoServiceNotifications  []string
	}

	testCases := []testCase{
		{
			"Global is false, service-levels are all non-existent",
			services,
			`
{"disableNotifications": false}
	`,
			false,
			make([]string, 0, len(services)),
		},
		{
			"Global is false, service-levels are all false",
			services,
			`
{
	"disableNotifications": false,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": false
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": false
				}
			}
		},
		"experiment3": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": false
				}
			}
		}
	}
}
	`,
			false,
			make([]string, 0, len(services)),
		},
		{
			"Global is true, no values set in services",
			services,
			`
{
	"disableNotifications": true,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		},
		"experiment3": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		}
	}
}
			`,
			true,
			[]string{"experiment1_role1", "experiment2_role1", "experiment3_role1"},
		},
		{
			"Global is true, values set in services that match global",
			services,
			`
{
	"disableNotifications": true,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": true
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": true
				}
			}
		},
		"experiment3": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": true
				}
			}
		}
	}
}
			`,
			true,
			[]string{"experiment1_role1", "experiment2_role1", "experiment3_role1"},
		},
		{
			"Global is false, any service-level is true", //  Note: this is fine because service email manager won't get started, so notifications won't get sent or forwarded to admin
			services,
			`
{
	"disableNotifications": false,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": true
				}
			}
		},
		"experiment3": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		}
	}
}
			`,
			false,
			[]string{"experiment2_role1"},
		},
		{
			"If global is true, and service-level is false",
			services,
			`
{
	"disableNotifications": true,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"disableNotificationsOverride": false
				}
			}
		},
		"experiment3": {
			"roles": {
				"role1": {
					"randomKey": "foo"
				}
			}
		}
	}
}
			`,
			false,
			[]string{"experiment1_role1", "experiment3_role1"},
		},
		{
			"Experiment-overridden service case",
			append(services, newExperimentOverriddenService("experiment1_role1", "experiment-override")),
			`
{
	"disableNotifications": false,
	"experiments": {
		"experiment1": {
			"roles": {
				"role1": {
					"fakeKey": "foo"
				}
			}
		},
		"experiment-override": {
			"experimentOverride": "experiment1",
			"roles": {
				"role1": {
					"disableNotificationsOverride": true
				}
			}
		},
		"experiment2": {
			"roles": {
				"role1": {
					"fakeKey": "foo"
				}
			}
		}
	}
}
			`,
			false,
			[]string{"experiment-override_role1"},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				viper.Reset()
				viper.SetConfigType("json")
				viper.ReadConfig(strings.NewReader(test.viperConfig))

				blockAdminNotifications, noServiceNotifications := resolveDisableNotifications(test.serviceSlice)
				assert.Equal(t, test.expectedBlockAdminNotifications, blockAdminNotifications)
				assert.Equal(t, test.expectedNoServiceNotifications, noServiceNotifications)
			},
		)
	}
}

func reset() {
	viper.Reset()
	devEnvironmentLabel = ""
}
