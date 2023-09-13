package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
)

var (
	kinitDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "managed_tokens",
			Name:      "kinit_duration_seconds",
			Help:      "Duration (in seconds) for a kerberos ticket to be created from the service principal",
		},
		[]string{
			"service",
		},
	)
	kinitFailureCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "managed_tokens",
		Name:      "failed_kinit_count",
		Help:      "The number of times the Managed Tokens Service failed to create a kerberos ticket from the service principal",
	},
		[]string{
			"service",
		},
	)
)

const kerberosDefaultTimeoutStr string = "20s"

func init() {
	metrics.MetricsRegistry.MustRegister(kinitDuration)
	metrics.MetricsRegistry.MustRegister(kinitFailureCount)
}

// kinitSuccess is a type that conveys whether GetKerberosTicketsWorker successfully obtains a kerberos ticket for each service
type kinitSuccess struct {
	service.Service
	success bool
}

func (v *kinitSuccess) GetService() service.Service {
	return v.Service
}

func (v *kinitSuccess) GetSuccess() bool {
	return v.success
}

// GetKerberosTicketsWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// obtains kerberos tickets from the configured kerberos principals.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func GetKerberosTicketsWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed Kerberos Tickets Worker Notifications Chan")
	}()

	kerberosTimeout, err := utils.GetProperTimeoutFromContext(ctx, kerberosDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse kerberos timeout")
	}

	var wg sync.WaitGroup
	for sc := range chans.GetServiceConfigChan() {
		wg.Add(1)
		go func(sc *Config) {
			defer wg.Done()
			success := &kinitSuccess{
				Service: sc.Service,
			}
			defer func(k *kinitSuccess) {
				chans.GetSuccessChan() <- k
			}(success)

			kerbContext, kerbCancel := context.WithTimeout(ctx, kerberosTimeout)
			defer kerbCancel()

			if err := GetKerberosTicketandVerify(kerbContext, sc); err != nil {
				var msg string
				if errors.Is(err, context.DeadlineExceeded) {
					msg = "Timeout error"
				} else {
					msg = "Could not obtain and verify kerberos ticket"
				}
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
					"account":    sc.Account,
				}).Error(msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
				return
			}
			success.success = true
		}(sc)
	}
	wg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

func GetKerberosTicketandVerify(ctx context.Context, sc *Config) error {
	funcLogger := log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
	})
	start := time.Now()

	if err := kerberos.GetTicket(ctx, sc.KeytabPath, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
		msg := "Could not obtain kerberos ticket"
		funcLogger.Error(msg)
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		return err
	}

	if err := kerberos.CheckPrincipal(ctx, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
		msg := "Kerberos ticket verification failed"
		funcLogger.Error(msg)
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		return err
	}
	funcLogger.Debug("Kerberos ticket obtained and verified")
	dur := time.Since(start).Seconds()
	kinitDuration.WithLabelValues(sc.Service.Name()).Observe(dur)
	return nil
}
