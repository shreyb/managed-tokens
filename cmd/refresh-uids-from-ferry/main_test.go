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
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	exeLogger = log.NewEntry(log.New())
	os.Exit(m.Run())
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

func TestInitTracing(t *testing.T) {
	// Test case 1: Tracing URL is not configured
	t.Run("Tracing URL not configured", func(t *testing.T) {
		viper.Set("tracing.url", "")
		shutdown, err := initTracing()
		assert.Error(t, err)
		assert.Nil(t, shutdown)
	})

	// Test case 2: Tracing URL is configured
	t.Run("Tracing URL configured", func(t *testing.T) {
		viper.Set("tracing.url", "http://example.com")
		shutdown, err := initTracing()
		assert.NoError(t, err)
		assert.NotNil(t, shutdown)
	})
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
