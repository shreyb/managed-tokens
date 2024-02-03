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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllServiceEmailManagerOptions(t *testing.T) {
	s := new(ServiceEmailManager)
	c := make(chan Notification)
	a := new(AdminNotificationManager)

	type testCase struct {
		description   string
		funcOptSetup  func() ServiceEmailManagerOption // Get us our test ServiceEmailManagerOption to apply to s
		expectedValue any
		testValueFunc func() any // The return value of this func is the value we're testing
	}

	testCases := []testCase{
		{
			"TestSetReceiveChan",
			func() ServiceEmailManagerOption {
				return SetReceiveChan(c)
			},
			c,
			func() any { return s.ReceiveChan },
		},
		{
			"TestSetAdminNotificationManager",
			func() ServiceEmailManagerOption {
				return SetAdminNotificationManager(a)
			},
			a,
			func() any { return s.AdminNotificationManager },
		},
		{
			"TestSetServiceEmailManagerNotificationMinimum",
			func() ServiceEmailManagerOption {
				n := 42
				return SetServiceEmailManagerNotificationMinimum(n)
			},
			42,
			func() any { return s.NotificationMinimum },
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				funcOpt := test.funcOptSetup()
				funcOpt(s)
				assert.Equal(t, test.expectedValue, test.testValueFunc())
			},
		)
	}
}
