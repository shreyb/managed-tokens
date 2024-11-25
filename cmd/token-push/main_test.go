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
	"context"
	"errors"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/testUtils"
)

func TestMain(m *testing.M) {
	exeLogger = log.NewEntry(log.New())
	os.Exit(m.Run())
}

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
				if !testUtils.SlicesHaveSameElementsOrderedType[string](results, test.expectedServiceNames) {
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
		expectedTimeouts map[timeoutKey]time.Duration
	}

	testCases := []testCase{
		{
			"No timeouts given",
			"",
			true,
			map[timeoutKey]time.Duration{
				timeoutGlobal:      time.Duration(300 * time.Second),
				timeoutKerberos:    time.Duration(20 * time.Second),
				timeoutVaultStorer: time.Duration(60 * time.Second),
				timeoutPing:        time.Duration(10 * time.Second),
				timeoutPush:        time.Duration(30 * time.Second),
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
			map[timeoutKey]time.Duration{
				timeoutGlobal:      time.Duration(360 * time.Second),
				timeoutKerberos:    time.Duration(15 * time.Second),
				timeoutVaultStorer: time.Duration(15 * time.Second),
				timeoutPing:        time.Duration(10 * time.Second),
				timeoutPush:        time.Duration(30 * time.Second),
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
			map[timeoutKey]time.Duration{
				timeoutGlobal:      time.Duration(300 * time.Second),
				timeoutKerberos:    time.Duration(20 * time.Second),
				timeoutVaultStorer: time.Duration(60 * time.Second),
				timeoutPing:        time.Duration(10 * time.Second),
				timeoutPush:        time.Duration(30 * time.Second),
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
	timeouts = map[timeoutKey]time.Duration{
		timeoutGlobal:      time.Duration(300 * time.Second),
		timeoutKerberos:    time.Duration(20 * time.Second),
		timeoutVaultStorer: time.Duration(60 * time.Second),
		timeoutPing:        time.Duration(10 * time.Second),
		timeoutPush:        time.Duration(30 * time.Second),
	}
}

func TestInitConfig(t *testing.T) {
	tempConfigDir := t.TempDir()
	testData := []byte("")
	os.WriteFile(path.Join(tempConfigDir, "config.yml"), testData, 0644)
	os.WriteFile(path.Join(tempConfigDir, "managedTokens.yml"), testData, 0644)

	type testCase struct {
		description        string
		configFile         string
		expectedConfigFile string
		expectedErr        error
	}

	testCases := []testCase{
		{
			"Config file exists",
			path.Join(tempConfigDir, "config.yml"),
			path.Join(tempConfigDir, "config.yml"),
			nil,
		},
		{
			"Config file does not exist, default config name used",
			"",
			path.Join(tempConfigDir, "managedTokens.yml"),
			nil,
		},
		{
			"User flag points to nonexistent file",
			"/path/to/nonexistent.yaml",
			"",
			os.ErrNotExist,
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.description,
			func(t *testing.T) {
				viper.Reset()
				viper.AddConfigPath(tempConfigDir)
				viper.Set("configfile", tc.configFile)

				err := initConfig()

				if tc.expectedErr == nil {
					assert.NoError(t, err)
					assert.Equal(t, tc.expectedConfigFile, viper.ConfigFileUsed())
				} else {
					assert.ErrorIs(t, err, tc.expectedErr)
				}
			},
		)
	}
}

func TestOpenDatabaseAndLoadServices(t *testing.T) {
	tempDbDir := t.TempDir()
	ctx := context.Background()

	type testCase struct {
		description    string
		dbLocation     string
		errNil         bool
		expectedDbPath string
	}

	testCases := []testCase{
		{
			"Valid db location",
			path.Join(tempDbDir, "test.db"),
			true,
			path.Join(tempDbDir, "test.db"),
		},
		{
			"Invalid db location",
			os.DevNull,
			false,
			"",
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.description,
			func(t *testing.T) {
				viper.Reset()
				viper.Set("dbLocation", tc.dbLocation)
				db, err := openDatabaseAndLoadServices(ctx)
				if tc.errNil {
					assert.Equal(t, tc.expectedDbPath, db.Location())
				} else {
					assert.Error(t, err)
				}
			},
		)
	}
}

func TestDisableNotifyFlagWorkaround(t *testing.T) {
	// Reset everything
	notificationsDisabledBy = DISABLED_BY_CONFIGURATION
	viper.Reset()

	// Save previous os.Args, and restore it at the end of this test
	prevArgs := os.Args
	t.Cleanup(func() { os.Args = prevArgs })

	// Set one of the disable notifications flags
	os.Args = []string{"executable-name", "--dont-notify"}
	initFlags()

	// Set the disabling to false here so we can test that the flag overrides this setting
	fakeViperConfig := `
{
	"disableNotifications": false
}
	`
	viper.SetConfigType("json")
	viper.ReadConfig(strings.NewReader(fakeViperConfig))

	disableNotifyFlagWorkaround()
	assert.Equal(t, DISABLED_BY_FLAG, notificationsDisabledBy)
	assert.Equal(t, true, viper.GetBool("disableNotifications"))
}

func TestCheckRunOnboardingFlags(t *testing.T) {
	type testCase struct {
		runOnboarding bool
		service       string
		expectedErr   error
	}

	testCases := map[string]testCase{
		"Run onboarding flag set without service": {
			true,
			"",
			errors.New("run-onboarding flag set without a service to run onboarding for"),
		},

		"Run onboarding flag set with service": {
			true,
			"some_service",
			nil,
		},
		"Run onboarding flag not set": {
			false,
			"",
			nil,
		},
	}

	for label, tc := range testCases {
		t.Run(label, func(t *testing.T) {
			reset()
			viper.Set("run-onboarding", tc.runOnboarding)
			viper.Set("service", tc.service)

			err := checkRunOnboardingFlags()
			assert.Equal(t, tc.expectedErr, err)
		})
	}
}
