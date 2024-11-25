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

	"github.com/stretchr/testify/assert"
)

func TestDisableNotificationsOptionString(t *testing.T) {
	testCases := []struct {
		option   disableNotificationsOption
		expected string
	}{
		{
			option:   DISABLED_BY_CONFIGURATION,
			expected: "configuration",
		},
		{
			option:   DISABLED_BY_FLAG,
			expected: "flag",
		},
		{
			option:   disableNotificationsOption(999),
			expected: "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := tc.option.String()
			assert.Equal(t, tc.expected, result)
		})
	}
}
