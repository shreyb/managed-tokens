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
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSetWorkerNumRetriesValue(t *testing.T) {
	opt := SetWorkerNumRetriesValue(GetKerberosTicketsWorkerType, 5)
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]map[workerSpecificConfigOption]any)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = make(map[workerSpecificConfigOption]any, 0)

	err := opt(c)
	assert.Nil(t, err)

	val, ok := c.workerSpecificConfig[GetKerberosTicketsWorkerType]
	if !ok {
		t.Errorf("Expected workerSpecificConfig to contain GetKerberosTicketsWorkerType")
	}

	valInt, ok := val[numRetriesOption].(uint)
	if !ok {
		t.Errorf("Expected value to be of type uint")
	}
	assert.Equal(t, uint(5), valInt)
}
func TestGetWorkerRetryValueFromConfig(t *testing.T) {
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]map[workerSpecificConfigOption]any)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = make(map[workerSpecificConfigOption]any, 0)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType][numRetriesOption] = uint(5)

	// Test case: Worker type exists in the config
	val, err := getWorkerNumRetriesValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.Nil(t, err)
	assert.Equal(t, uint(5), val)

	// Test case: Worker type does not exist in the config
	val, err = getWorkerNumRetriesValueFromConfig(*c, PushTokensWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, uint(0), val)

	// Test case: Worker type exists in the config but value is not of type uint
	c.workerSpecificConfig[GetKerberosTicketsWorkerType][numRetriesOption] = "invalid"
	val, err = getWorkerNumRetriesValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, uint(0), val)
}
func TestIsValidWorkerSpecificConfigOption(t *testing.T) {
	// Test case: Valid worker specific config option
	validOption := isValidWorkerSpecificConfigOption(numRetriesOption)
	assert.True(t, validOption)

	// Test case: Invalid worker specific config option
	invalidOption := isValidWorkerSpecificConfigOption(workerSpecificConfigOption(2))
	assert.False(t, invalidOption)
}
func TestSetWorkerRetrySleepValue(t *testing.T) {
	opt := SetWorkerRetrySleepValue(GetKerberosTicketsWorkerType, 5*time.Second)
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]map[workerSpecificConfigOption]any)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = make(map[workerSpecificConfigOption]any, 0)

	// Make sure our returned ConfigOption works
	err := opt(c)
	assert.Nil(t, err)

	// Make sure the top-level map was intialized correctly
	val, ok := c.workerSpecificConfig[GetKerberosTicketsWorkerType]
	if !ok {
		t.Errorf("Expected workerSpecificConfig to contain GetKerberosTicketsWorkerType")
	}

	// Check that the value was set correctly
	valDuration, ok := val[retrySleepOption].(time.Duration)
	if !ok {
		t.Errorf("Expected value to be of type time.Duration")
	}
	assert.Equal(t, 5*time.Second, valDuration)
}

func TestGetWorkerRetrySleepValueFromConfig(t *testing.T) {
	c := &Config{}
	c.workerSpecificConfig = make(map[WorkerType]map[workerSpecificConfigOption]any)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType] = make(map[workerSpecificConfigOption]any, 0)
	c.workerSpecificConfig[GetKerberosTicketsWorkerType][retrySleepOption] = 5 * time.Second

	// Test case: Worker type exists in the config
	val, err := getWorkerRetrySleepValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.Nil(t, err)
	assert.Equal(t, 5*time.Second, val)

	// Test case: Worker type does not exist in the config
	val, err = getWorkerRetrySleepValueFromConfig(*c, PushTokensWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, time.Duration(0), val)

	// Test case: Worker type exists in the config but value is not of type time.Duration
	c.workerSpecificConfig[GetKerberosTicketsWorkerType][retrySleepOption] = "invalid"
	val, err = getWorkerRetrySleepValueFromConfig(*c, GetKerberosTicketsWorkerType)
	assert.NotNil(t, err)
	assert.Equal(t, time.Duration(0), val)
}
