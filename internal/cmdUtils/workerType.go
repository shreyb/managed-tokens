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

package cmdUtils

import (
	"strings"

	"github.com/spf13/viper"

	"github.com/fermitools/managed-tokens/internal/worker"
)

var validWorkerTypes = []worker.WorkerType{
	worker.GetKerberosTicketsWorkerType,
	worker.StoreAndGetTokenWorkerType,
	worker.StoreAndGetTokenInteractiveWorkerType,
	worker.PingAggregatorWorkerType,
	worker.PushTokensWorkerType,
}

// GetWorkerConfigValue retrieves the value of a worker-specific key from the configuration
func GetWorkerConfigValue(workerType, key string) any {
	if !isValidWorkerTypeString(workerType) {
		return nil
	}

	workerConfigPath := "workerType." + workerType + "." + key

	return viper.Get(workerConfigPath)
}

// GetWorkerConfigString retrieves the configuration value for the given worker type and key,
// and returns it as a string. If the value is not a string, an empty string is returned.
func GetWorkerConfigString(workerType, key string) string {
	val := GetWorkerConfigValue(workerType, key)
	if v, ok := val.(string); ok {
		return v
	}
	return ""
}

// GetWorkerConfigInt retrieves the configuration value for the given worker type and key,
// and returns it as a string. If the value is not a string, an empty string is returned.
func GetWorkerConfigInt(workerType, key string) int {
	val := GetWorkerConfigValue(workerType, key)
	if v, ok := val.(int); ok {
		return v
	}
	return 0
}

// GetWorkerConfigStringSlice retrieves the configuration value for the given worker type and key,
// and returns it as a slice of strings. If the value is not a []string, an empty slice is returned.
func GetWorkerConfigStringSlice(workerType, key string) []string {
	empty := make([]string, 0)
	val := GetWorkerConfigValue(workerType, key)
	if v, ok := val.([]string); ok {
		return v
	}
	return empty
}

// isValidWorkerTypeString checks if the given string is equal to the string representation
// of a valid WorkerType wt as determined by wt.String()
func isValidWorkerTypeString(s string) bool {
	for _, wt := range validWorkerTypes {
		if strings.EqualFold(wt.String(), s) {
			return true
		}
	}
	return false
}
