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

func TestSetWorkerRetryValue(t *testing.T) {
	opt := SetWorkerRetryValue(GetKerberosTicketsWorkerType, 5)
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]any)

	err := opt(c)
	assert.Nil(t, err)

	val, ok := c.workerSpecificConfig[GetKerberosTicketsWorkerType]
	if !ok {
		t.Errorf("Expected workerSpecificConfig to contain GetKerberosTicketsWorkerType")
	}

	valInt, ok := val.(uint)
	if !ok {
		t.Errorf("Expected value to be of type uint")
	}
	assert.Equal(t, uint(5), valInt)
}
func TestGetWorkerRetryValueFromConfig(t *testing.T) {
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]any)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = uint(5)

	// Test case: Worker type exists in the config
	val, err := getWorkerRetryValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.Nil(t, err)
	assert.Equal(t, uint(5), val)

	// Test case: Worker type does not exist in the config
	val, err = getWorkerRetryValueFromConfig(*c, PushTokensWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, uint(0), val)

	// Test case: Worker type exists in the config but value is not of type uint
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = "invalid"
	val, err = getWorkerRetryValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, uint(0), val)
}
