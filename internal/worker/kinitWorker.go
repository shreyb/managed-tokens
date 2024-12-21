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
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/kerberos"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
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
func GetKerberosTicketsWorker(ctx context.Context, chans channelGroup) {
	ctx, span := tracer.Start(ctx, "GetKerberosTicketsWorker")
	defer span.End()

	defer func() {
		chans.closeWorkerSendChans()
		log.Debug("Closed Kerberos Tickets Worker Notifications and Success Chan")
	}()

	kerberosTimeout, defaultUsed, err := contextStore.GetProperTimeout(ctx, kerberosDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse kerberos timeout")
	}
	if defaultUsed {
		log.Debug("Using default kerberos timeout")
	}

	var wg sync.WaitGroup
	for sc := range chans.serviceConfigChan {
		wg.Add(1)
		go func(sc *Config) {
			defer wg.Done()

			ctx, span := tracer.Start(ctx, "GetKerberosTicketsWorker_anonFunc")
			span.SetAttributes(
				attribute.String("account", sc.Account),
				attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
			)
			defer span.End()

			success := &kinitSuccess{
				Service: sc.Service,
			}
			defer func(k *kinitSuccess) {
				chans.successChan <- k
			}(success)

			kerbContext, kerbCancel := context.WithTimeout(ctx, kerberosTimeout)
			defer kerbCancel()

			if err := GetKerberosTicketandVerify(kerbContext, sc); err != nil {
				msg := "Could not obtain and verify kerberos ticket"
				if errors.Is(err, context.DeadlineExceeded) {
					msg = msg + ": timeout error"
				}
				logErrorWithTracing(log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
					"account":    sc.Account,
				}), span, errors.New(msg))
				chans.notificationsChan <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
				return
			}
			span.SetStatus(codes.Ok, "Kerberos ticket obtained and verified")
			success.success = true
		}(sc)
	}
	wg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

func GetKerberosTicketandVerify(ctx context.Context, sc *Config) error {
	ctx, span := tracer.Start(ctx, "GetKerberosTicketandVerify")
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
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		err = fmt.Errorf("could not obtain kerberos ticket: %w", err)
		logErrorWithTracing(funcLogger, span, err)
		return err
	}

	if err := kerberos.CheckPrincipal(ctx, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
		kinitFailureCount.WithLabelValues(sc.Service.Name()).Inc()
		err = fmt.Errorf("kerberos ticket verification failed: %w", err)
		logErrorWithTracing(funcLogger, span, err)
		return err
	}

	dur := time.Since(start).Seconds()
	kinitDuration.WithLabelValues(sc.Service.Name()).Set(dur)
	logSuccessWithTracing(funcLogger, span, "Kerberos ticket obtained and verified")
	return nil
}
