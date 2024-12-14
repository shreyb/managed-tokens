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

// Package metrics contains a Prometheus metrics registry that importing code can use to register Prometheus metrics.  When all metrics collection
// is complete, the PushToPrometheus function can be used to push metrics to a Prometheus Pushgateway server, which is configured through viper.
package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

var (
	// MetricsRegistry is a prometheus registry that can be exported and used by importing libraries
	MetricsRegistry = prometheus.NewRegistry()
)

// PushToPrometheus uses the package MetricsRegistry to push registered metrics to the configured Prometheus pushgateway
func PushToPrometheus(hostName, jobName string) error {
	pusher := push.New(hostName, jobName).Gatherer(MetricsRegistry)
	if err := pusher.Push(); err != nil {
		return fmt.Errorf("could not push metrics to the prometheus pushgateway: %w", err)
	}
	return nil
}
