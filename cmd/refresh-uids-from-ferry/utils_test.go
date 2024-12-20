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
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/testUtils"
)

func reset() {
	viper.Reset()
	devEnvironmentLabel = ""
}

func TestGetAllAccountsFromConfig(t *testing.T) {
	// Set up test data
	viper.Set("experiments", map[string]any{
		"experiment1": map[string]any{
			"roles": map[string]any{
				"role1": map[string]any{
					"account": "account1",
				},
				"role2": map[string]any{
					"account": "account2",
				},
			},
		},
		"experiment2": map[string]any{
			"roles": map[string]any{
				"role3": map[string]any{
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
	assert.True(t, testUtils.SlicesHaveSameElementsOrderedType(accounts, expected))
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
func TestSendSetupErrorToAdminMgr(t *testing.T) {
	receiveChan := make(chan notifications.SourceNotification)
	msg := "Test error message"
	done := make(chan bool)

	go func() {
		defer func() { done <- true }()
		select {
		case notification := <-receiveChan:
			assert.Equal(t, "Test error message", notification.Notification.GetMessage())
		default:
			t.Error("Expected to receive a notification, but none received")
		}
	}()

	sendSetupErrorToAdminMgr(receiveChan, msg)
	assert.Eventually(t, func() bool {
		return <-done
	}, time.Second, 10*time.Millisecond)
}

func TestGetAndAggregateFERRYData(t *testing.T) {
	nonNilErr := errors.New("this is a test error")

	type testCase struct {
		description       string
		username          string
		authFunc          func() func(context.Context, string, string) (*http.Response, error)
		ferryDataChan     chan db.FerryUIDDatum
		notificationsChan chan notifications.SourceNotification
		expectedErr       error
	}

	testCases := []testCase{
		{
			"error getting ferry data",
			"test_user",
			func() func(context.Context, string, string) (*http.Response, error) {
				return func(ctx context.Context, url, verb string) (*http.Response, error) {
					return nil, nonNilErr
				}
			},
			// buffer these channels so we can send values without preemptively starting up listeners like we normally would
			make(chan db.FerryUIDDatum, 1),
			make(chan notifications.SourceNotification, 1),
			nonNilErr,
		},
		{
			"no error getting ferry data",
			"test_user",
			func() func(context.Context, string, string) (*http.Response, error) {
				return func(ctx context.Context, url, verb string) (*http.Response, error) {
					fakeResponseBody := `{"ferry_status":"success","ferry_error":[],"ferry_output":{"expirationdate":"2024-01-01T00:00:00Z","fullname":"Test User","groupaccount":false,"status":true,"uid":12345,"vopersonid":"12345"}}`
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(strings.NewReader(fakeResponseBody)),
					}, nil
				}
			},
			// buffer these channels so we can send values without preemptively starting up listeners like we normally would
			make(chan db.FerryUIDDatum, 1),
			make(chan notifications.SourceNotification, 1),
			nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			defer close(tc.ferryDataChan)
			defer close(tc.notificationsChan)
			err := getAndAggregateFERRYData(context.Background(), tc.username, tc.authFunc, tc.ferryDataChan, tc.notificationsChan)
			assert.ErrorIs(t, err, tc.expectedErr)

			if tc.expectedErr != nil {
				// If there is no expected error, we check that a notification got sent.  We don't care in this case about the contents
				// of the notification, just that one was sent
				testCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()

				select {
				case <-testCtx.Done():
					t.Error("Expected to receive a notification, but none received")
				case <-tc.notificationsChan:
					// This is the expected behavior, so we do nothing and let the test continue
				}
				return
			}

			// At this point, we don't expect an error, so the entry should have gotten sent on the ferryDataChan. Like before, we don't
			// care about the contents of the entry, just that one was sent
			testCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			select {
			case <-testCtx.Done():
				t.Error("Expected to receive a notification, but none received")
			case <-tc.ferryDataChan:
				// This is the expected behavior
			}
		})
	}
}
