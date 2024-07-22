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

import "fmt"

// SetWorkerRetryValue is a function that sets the retry value for a specific worker type.  It returns a ConfigOption
func SetWorkerRetryValue(workerType WorkerType, value uint) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.workerSpecificConfig[workerType] = value
		return nil
	})
}

// getWorkerRetryValueFromConfig retrieves the retry value for a specific worker type from the given configuration.
// It returns the retry value as a uint and a non-nil error if the worker type is not found in the configuration or if the value is not of type uint.
func getWorkerRetryValueFromConfig(c Config, w WorkerType) (uint, error) {
	val, ok := c.workerSpecificConfig[w]
	if !ok {
		return 0, fmt.Errorf("workerType %s not found in workerSpecificConfig", w)
	}

	valUInt, ok := val.(uint)
	if !ok {
		return 0, fmt.Errorf("value for workerType %s is not of type uint.  Got type %T", w, val)
	}

	return valUInt, nil
}
