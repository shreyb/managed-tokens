package notifications

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

var (
	promDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "stage_duration_seconds",
		Help:      "The amount of time it took to run a stage (setup|processing|cleanup) of a Managed Tokens Service executable",
	},
		[]string{
			"executable",
			"stage",
		},
	)

	ferryRefreshTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "last_ferry_refresh",
			Help:      "The timestamp of the last successful refresh of the username --> UID table from FERRY for the Managed Tokens Service",
		},
	)

	tokenPushTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "last_token_push_timestamp",
			Help:      "The timestamp of the last successful push of a service vault token to an interactive node by the Managed Tokens Service",
		},
		[]string{
			"service",
			"node",
		},
	)
)

// BasicPromPush holds information about a prometheus pushgateway configuration.  It can be passed between processes to report metrics
// to the same Prometheus server.  It is not in and of itself thread-safe, so make sure to either use mutexes or ensure that concurrent
// usage of BasicPromPushes will not cause problems
type BasicPromPush struct {
	P *push.Pusher
	R *prometheus.Registry
}

// RegisterMetrics is a setup function used to register the metrics needed throughout the running of the managed proxy service
func (b BasicPromPush) RegisterMetrics() error {
	if err := b.R.Register(promDuration); err != nil {
		return errors.New("could not register promDuration metric for monitoring")
	}

	if err := b.R.Register(ferryRefreshTime); err != nil {
		return errors.New("could not register ferryRefreshTime metric for monitoring")
	}

	if err := b.R.Register(tokenPushTime); err != nil {
		return errors.New("could not register tokenPushTime metric for monitoring")
	}
	return nil
}

// PushNodeRoleTimestamp sets the value of proxyPushTime to the current time, and pushes that metric to the Pushgateway
// configured in b with the arguments as labels
func (b BasicPromPush) PushServiceNodeTimestamp(service, node string) error {
	tokenPushTime.WithLabelValues(service, node).SetToCurrentTime()
	err := b.P.Add()
	return err
}

// PushMyProxyStoreTime sets the value of myProxyStoreTime to the current time, and pushes that metric to the Pushgateway
// configured in b with the arguments as labels
func (b BasicPromPush) PushFERRYRefreshTime() error {
	ferryRefreshTime.SetToCurrentTime()
	err := b.P.Add()
	return err
}

// PushCountErrors creates a Gauge metric, proxyPushErrorCount, sets its value to numErrors, and pushes that to the Pushgateway
// configured in b
func (b BasicPromPush) PushCountErrors(numErrors int) error {
	servicePushFailureCount := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "failed_services_push_count",
		Help:      "The number of services for which pushing tokens failed in the last round",
	})

	if err := b.R.Register(servicePushFailureCount); err != nil {
		return err
	}
	servicePushFailureCount.Set(float64(numErrors))

	err := b.P.Add()
	return err
}

// PushPromDuration sets the value of promDuration to the time since start, and pushes that metric to the Pushgateway configured in b
// with the stage argument as a labels
func (b BasicPromPush) PushPromDuration(start time.Time, executable, stage string) error {
	promDuration.WithLabelValues(executable, stage).Set(time.Since(start).Seconds())
	err := b.P.Add()
	return err
}
