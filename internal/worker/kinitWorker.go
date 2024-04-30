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
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/kerberos"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
)

var (
	kinitDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "kinit_duration_seconds",
			Help:      "Duration (in seconds) for a kerberos ticket to be created from the service principal",
		},
		[]string{
			"service",
		},
	)
	kinitFailureCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
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
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.GetKerberosTicketsWorker")
	defer span.End()

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

			ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.GetKerberosTicketsWorker_anonFunc")
			span.SetAttributes(
				attribute.String("account", sc.Account),
				attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
			)
			defer span.End()

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
				tracing.LogErrorWithTrace(span, log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
					"account":    sc.Account,
				}), msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
				return
			}
			span.SetStatus(codes.Ok, "Kerberos ticket obtained and verified")
			success.success = true
		}(sc)
	}
	wg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

func GetKerberosTicketandVerify(ctx context.Context, sc *Config) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.GetKerberosTicketandVerify")
	span.SetAttributes(
		attribute.String("experiment", sc.Service.Experiment()),
		attribute.String("role", sc.Service.Role()),
	)
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
	})
	start := time.Now()

	if err := kerberos.GetTicket(ctx, sc.KeytabPath, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Could not obtain kerberos ticket")
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		return err
	}

	if err := kerberos.CheckPrincipal(ctx, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Kerberos ticket verification failed")
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		return err
	}

	dur := time.Since(start).Seconds()
	kinitDuration.WithLabelValues(sc.Service.Name()).Set(dur)
	span.SetStatus(codes.Ok, "Kerberos ticket obtained and verified")
	funcLogger.Debug("Kerberos ticket obtained and verified")
	return nil
}
