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
	"bytes"
	"slices"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/exp/constraints"

	"github.com/fermitools/managed-tokens/internal/worker"
)

var validWorkerTypes = []worker.WorkerType{
	worker.GetKerberosTicketsWorkerType,
	worker.StoreAndGetTokenWorkerType,
	worker.StoreAndGetTokenInteractiveWorkerType,
	worker.PingAggregatorWorkerType,
	worker.PushTokensWorkerType,
}

// workerTypeToConfigString converts a worker type to a string that the configuration uses
func workerTypeToConfigString(wt worker.WorkerType) string {
	s := wt.String()
	first := bytes.ToLower([]byte(s[0:1]))
	return string(first) + s[1:]
}

// getWorkerConfigValue retrieves the value of a worker-specific key from the configuration
func getWorkerConfigValue(wt worker.WorkerType, key string) any {
	if !slices.Contains(validWorkerTypes, wt) {
		return nil
	}
	workerConfigPath := "workerType." + workerTypeToConfigString(wt) + "." + key
	return viper.Get(workerConfigPath)
}

// getWorkerConfigString retrieves the configuration value for the given worker type and key,
// and returns it as a string. If the value is not a string, an empty string is returned.
func getWorkerConfigString(wt worker.WorkerType, key string) string {
	val := getWorkerConfigValue(wt, key)
	if v, ok := val.(string); ok {
		return v
	}
	return ""
}

// getWorkerConfigInt retrieves the configuration value for the given worker type and key,
// and returns it as a string. If the value is not a string, an empty string is returned.
func getWorkerConfigInteger[T constraints.Integer](wt worker.WorkerType, key string) T {
	val := getWorkerConfigValue(wt, key)
	if v, ok := val.(T); ok {
		return v
	}
	return 0
}

// getWorkerConfigStringSlice retrieves the configuration value for the given worker type and key,
// and returns it as a slice of strings. If the value is not a []string, an empty slice is returned.
func getWorkerConfigStringSlice(wt worker.WorkerType, key string) []string {
	empty := make([]string, 0)
	val := getWorkerConfigValue(wt, key)
	if v, ok := val.([]string); ok {
		return v
	}
	return empty
}

// getWorkerConfigTimeDuration retrieves the configuration value for the given worker type and key,
// and returns it as a time.Duration. If the configuration value cannot be parsed into a time.Duration,
// 0 is returned
func getWorkerConfigTimeDuration(wt worker.WorkerType, key string) time.Duration {
	val := getWorkerConfigValue(wt, key)
	v, ok := val.(string)
	if !ok {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}
