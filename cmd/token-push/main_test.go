package main

import (
	"strings"
	"testing"

	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/testUtils"
	"github.com/spf13/viper"
)

var testConfig string = `
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

func TestInitServices(t *testing.T) {
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

				configReader := strings.NewReader(testConfig)
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
