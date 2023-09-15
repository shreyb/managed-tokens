// Package metrics contains a Prometheus metrics registry that importing code can use to register Prometheus metrics.  When all metrics collection
// is complete, the PushToPrometheus function can be used to push metrics to a Prometheus Pushgateway server, which is configured through viper.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	log "github.com/sirupsen/logrus"
)

var (
	// MetricsRegistry is a prometheus registry that can be exported and used by importing libraries
	MetricsRegistry = prometheus.NewRegistry()
)

// PushToPrometheus uses the package MetricsRegistry to push registered metrics to the configured Prometheus pushgateway
func PushToPrometheus(hostName, jobName string) error {
	pusher := push.New(hostName, jobName).Gatherer(MetricsRegistry)
	if err := pusher.Add(); err != nil {
		log.Errorf("Could not push metrics to the prometheus pushgateway: %s", err)
		return err
	}
	return nil
}
