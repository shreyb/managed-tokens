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
	"fmt"
	"math/rand"
	"path"
	"testing"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestAllAdminNotificationManagerOptions(t *testing.T) {
	a := new(AdminNotificationManager)
	tempDir := t.TempDir()
	dbLocation := path.Join(tempDir, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000)))
	database, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		t.Fatal("Could not create database for testing")
	}

	type testCase struct {
		description   string
		funcOptSetup  func() AdminNotificationManagerOption // Get us our test AdminNotificationManagerOption to apply to a
		expectedValue any
		testValueFunc func() any // The return value of this func is the value we're testing
	}

	testCases := []testCase{
		{
			"SetDatabaseOption",
			func() AdminNotificationManagerOption {
				return SetAdminNotificationManagerDatabase(database)
			},
			database,
			func() any { return a.Database },
		},
		{
			"TestSetAdminNotificationManagerNotificationMinimum",
			func() AdminNotificationManagerOption {
				notificationMinimum := 42
				return SetAdminNotificationManagerNotificationMinimum(notificationMinimum)
			},
			42,
			func() any { return a.NotificationMinimum },
		},
		{
			"TestSetTrackErrorCountsToTrue",
			func() AdminNotificationManagerOption {
				return SetTrackErrorCountsToTrue()
			},
			true,
			func() any { return a.TrackErrorCounts },
		},
		{
			"TestSetDatabaseReadOnlyToTrue",
			func() AdminNotificationManagerOption {
				return SetDatabaseReadOnlyToTrue()
			},
			true,
			func() any { return a.DatabaseReadOnly },
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				funcOpt := test.funcOptSetup()
				funcOpt(a)
				assert.Equal(t, test.expectedValue, test.testValueFunc())
			},
		)
	}
}
