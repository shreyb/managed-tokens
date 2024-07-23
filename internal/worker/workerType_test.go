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

package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkerTypeString(t *testing.T) {
	tests := []struct {
		workerType WorkerType
		expected   string
	}{
		{
			workerType: GetKerberosTicketsWorkerType,
			expected:   "GetKerberosTicketsWorker",
		},
		{
			workerType: StoreAndGetTokenWorkerType,
			expected:   "StoreAndGetTokenWorker",
		},
		{
			workerType: PingAggregatorWorkerType,
			expected:   "PingAggregatorWorker",
		},
		{
			workerType: PushTokensWorkerType,
			expected:   "PushTokensWorker",
		},
		{
			workerType: 5, // UnknownWorkerType
			expected:   "UnknownWorkerType",
		},
	}

	for _, test := range tests {
		result := test.workerType.String()
		assert.Equal(t, test.expected, result)
	}
}
func TestIsValidWorkerType(t *testing.T) {
	tests := []struct {
		workerType WorkerType
		expected   bool
	}{
		{
			workerType: GetKerberosTicketsWorkerType,
			expected:   true,
		},
		{
			workerType: StoreAndGetTokenWorkerType,
			expected:   true,
		},
		{
			workerType: PingAggregatorWorkerType,
			expected:   true,
		},
		{
			workerType: PushTokensWorkerType,
			expected:   true,
		},
		{
			workerType: invalidWorkerType,
			expected:   false,
		},
		{
			workerType: invalidWorkerType + 1, // UnknownWorkerType
			expected:   false,
		},
	}

	for _, test := range tests {
		result := isValidWorkerType(test.workerType)
		assert.Equal(t, test.expected, result)
	}
}
