package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	MetricsRegistry = prometheus.NewRegistry()
)

// PushToPrometheus uses the MetricsRegistry to push registered metrics to the configured Prometheus pushgateway
func PushToPrometheus() error {
	pusher := push.New(viper.GetString("prometheus.host"), viper.GetString("prometheus.jobname")).Gatherer(MetricsRegistry)
	if err := pusher.Add(); err != nil {
		log.Error("Could not push metrics to the prometheus pushgateway")
		log.Error(err)
		return err
	}
	return nil
}
