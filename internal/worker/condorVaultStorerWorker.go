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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/vaultToken"
)

// Metrics
var (
	tokenStoreTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "last_token_store_timestamp",
			Help:      "The timestamp of the last successful store of a service vault token in a condor credd by the Managed Tokens Service",
		},
		[]string{
			"service",
			"credd",
		},
	)
	tokenStoreDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "token_store_duration_seconds",
			Help:      "Duration (in seconds) for a vault token to get stored in a condor credd",
		},
		[]string{
			"service",
			"credd",
		},
	)
	storeFailureCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "failed_vault_token_store_count",
		Help:      "The number of times the Managed Tokens Service failed to store a vault token in a condor credd",
	},
		[]string{
			"service",
			"credd",
		},
	)
)

const vaultStorerDefaultTimeoutStr string = "60s"

func init() {
	metrics.MetricsRegistry.MustRegister(tokenStoreTimestamp)
	metrics.MetricsRegistry.MustRegister(tokenStoreDuration)
	metrics.MetricsRegistry.MustRegister(storeFailureCount)

}

// vaultStorerSuccess is a type that conveys whether StoreAndGetTokenWorker successfully stores and obtains tokens for each service
type vaultStorerSuccess struct {
	service.Service
	success bool
}

func (v *vaultStorerSuccess) GetService() service.Service {
	return v.Service
}

func (v *vaultStorerSuccess) GetSuccess() bool {
	return v.success
}

// StoreAndGetTokenWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// stores a refresh token in the configured vault and obtains vault and bearer tokens.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func StoreAndGetTokenWorker(ctx context.Context, chans channelGroup) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokenWorker")
	defer span.End()

	// Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
	defer func() {
		chans.closeWorkerSendChans()
		log.Debug("Closed StoreAndGetTokenWorker Notifications and Success Chan")
	}()

	vaultStorerTimeout, defaultUsed, err := contextStore.GetProperTimeout(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		span.SetStatus(codes.Error, "Could not parse vault storer timeout")
		log.Fatal("Could not parse vault storer timeout")
	}
	if defaultUsed {
		log.Debug("Using default timeout for vault storer")
	}

	for sc := range chans.serviceConfigChan {
		func(sc *Config) {
			success := &vaultStorerSuccess{
				Service: sc.Service,
				success: true,
			}

			defer func(s *vaultStorerSuccess) {
				chans.successChan <- s
			}(success)

			configLogger := log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
				"service":    sc.Name(),
			})

			errsToReport := make([]error, 0) // slice of errors we need to specifically highlight
			for _, schedd := range sc.Schedds {
				func(ctx context.Context, schedd string) {
					ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokenWorker_anonFunc")
					span.SetAttributes(attribute.String("service", sc.ServiceNameFromExperimentAndRole()))
					span.SetAttributes(attribute.String("schedd", schedd))
					defer span.End()

					scheddLogger := configLogger.WithField("schedd", schedd)

					ts := vaultToken.NewNonInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer)

					vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
					defer vaultStorerCancel()

					if err := StoreAndGetTokensForSchedd(vaultStorerContext, &sc.CommandEnvironment, sc.ServiceCreddVaultTokenPathRoot, ts); err != nil {
						success.success = false

						// Check to see if we need to report a specific error
						msg := "could not store and get vault tokens for schedd"
						if errors.Is(err, context.DeadlineExceeded) {
							errsToReport = append(errsToReport, fmt.Errorf("%s %s: timeout error", msg, schedd))
						} else {
							errsToReport = append(errsToReport, fmt.Errorf("%s %s: %w", msg, schedd, err))
						}
						logErrorWithTracing(scheddLogger, span, fmt.Errorf("msg: %w", err))
						return
					}
					logSuccessWithTracing(scheddLogger, span, "Successfully got and stored vault token for schedd")
				}(ctx, schedd)
			}

			if !success.success {
				msg := "Could not store and get vault tokens"
				for _, err := range errsToReport {
					msg = fmt.Sprintf("%s; %s", msg, err.Error())
				}
				logErrorWithTracing(configLogger, span, errors.New(msg))
				chans.notificationsChan <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
				return
			}
			logSuccessWithTracing(configLogger, span, "Successfully stored and got all vault tokens")
		}(sc)
	}
}

// StoreAndGetTokenInteractiveWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// stores a refresh token in the configured vault and obtains vault and bearer tokens.  It is meant to be used for a single interactive
// operation to store a single service's vault token on the configured credds.  It will display all the stdout from the underlying executables
// to screen.
func StoreAndGetTokenInteractiveWorker(ctx context.Context, chans channelGroup) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokenInteractiveWorker")
	defer span.End()

	// Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
	defer func() {
		chans.closeWorkerSendChans()
		log.Debug("Closed StoreAndGetTokenInteractiveWorker Notifications and Success Chans")
	}()

	vaultStorerTimeout, defaultUsed, err := contextStore.GetProperTimeout(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		span.SetStatus(codes.Error, "Could not parse vault storer timeout")
		log.Fatal("Could not parse vault storer timeout")
	}
	if defaultUsed {
		log.Debug("Using default timeout for vault storer")
	}

	sc := <-chans.serviceConfigChan // Get our service config
	success := &vaultStorerSuccess{
		Service: sc.Service,
		success: true,
	}

	defer func(s *vaultStorerSuccess) {
		chans.successChan <- s
	}(success)

	configLogger := log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
		"service":    sc.Name(),
	})

	// Store and get tokens for each schedd
	for _, schedd := range sc.Schedds {
		func(ctx context.Context, schedd string) {
			ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokenInteractiveWorker_anonFunc")
			span.SetAttributes(attribute.String("service", sc.ServiceNameFromExperimentAndRole()))
			span.SetAttributes(attribute.String("schedd", schedd))
			defer span.End()

			scheddLogger := configLogger.WithField("schedd", schedd)

			ts := vaultToken.NewInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer)

			vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
			defer vaultStorerCancel()

			if err := StoreAndGetTokensForSchedd(vaultStorerContext, &sc.CommandEnvironment, sc.ServiceCreddVaultTokenPathRoot, ts); err != nil {
				success.success = false
				logErrorWithTracing(scheddLogger, span, fmt.Errorf("could not store and get vault tokens for schedd: %w", err))
				return
			}
			logSuccessWithTracing(scheddLogger, span, "Successfully got and stored vault token for schedd")
		}(ctx, schedd)
	}

	if !success.success {
		msg := "Could not store and get vault tokens"
		logErrorWithTracing(configLogger, span, errors.New(msg))
		return
	}
	logSuccessWithTracing(configLogger, span, "Successfully stored and got all vault tokens")
}

// StoreAndGetTokensForSchedds will store a refresh token on the condor-configured vault server, obtain vault and bearer tokens for a service
// using HTCondor executables, and store the vault token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// If there was an error with ANY of the schedds, StoreAndGetTokensForSchedds will return an error
func StoreAndGetTokensForSchedd[T tokenStorer](ctx context.Context, environ *environment.CommandEnvironment, tokenRootPath string, ts T) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokensForSchedd")
	span.SetAttributes(attribute.String("tokenRootPath", tokenRootPath))
	span.SetAttributes(attribute.String("service", ts.GetServiceName()))
	span.SetAttributes(attribute.String("credd", ts.GetCredd()))
	defer span.End()

	var success bool = true

	funcLogger := log.WithFields(log.Fields{
		"service": ts.GetServiceName(),
		"credd":   ts.GetCredd(),
	})
	start := time.Now()

	// Before we stage any prior vault token, check to make sure our context hasn't already been canceled
	if ctx.Err() != nil {
		err := fmt.Errorf("context was canceled or the deadline exceeded before token vault staging.  Will not attempt to stage a stored token file or store vault token: %w", ctx.Err())
		logErrorWithTracing(funcLogger, span, err)
		success = false
		return err
	}

	// Stage prior vault token, if it exists
	restorePriorTokenFunc, err := backupCondorVaultToken(ts.GetServiceName())
	if err != nil {
		funcLogger.Errorf("Error backing up current vault token at %s.  Will overwrite this with a new vault token.", getCondorVaultTokenLocation(ts.GetServiceName()))
	}
	defer func() {
		if err := restorePriorTokenFunc(); err != nil {
			funcLogger.Errorf("Error restoring prior condor vault token.  Please see the logs to see where the token might have been backed up.")
		}
	}()

	if err = stageStoredTokenFile(tokenRootPath, ts.GetServiceName(), ts.GetCredd()); err != nil {
		switch {
		case errors.Is(err, errNoServiceCreddToken):
			funcLogger.Info("No prior vault token exists for this service/credd combination.  Will get a new vault token")
		case errors.Is(err, errMoveServiceCreddToken):
			funcLogger.Warn("There was an error staging the prior vault token for this service and credd.  Will get a new vault token")
		default:
			logErrorWithTracing(funcLogger, span, fmt.Errorf("Could not stage prior vault token.  Please investigate, and be aware that stale credentials may get stored."))
			success = false
		}
	}

	// Make sure we store whatever comes out of storing the vault token, if that is a successful operation.
	// Note that if this operation fails, assuming we got a condor vault token, that will
	// stick around unless something else cleans up vault tokens.  This is actually OK, since
	// eventually that vault token will expire, and htgettoken will determine that a new one is
	// needed.
	defer func() {
		if success {
			if err = storeServiceTokenForCreddFile(tokenRootPath, ts.GetServiceName(), ts.GetCredd()); err != nil {
				funcLogger.Error("Could not store condor vault token for credd for future runs.  Please investigate")
			}
		}
	}()

	// Last context check again here:  before we store vault token, check to make sure our context hasn't already been canceled
	if ctx.Err() != nil {
		err := fmt.Errorf("context was canceled or the deadline exceeded before token vault storage.  Will not attempt to store vault token: %w", ctx.Err())
		logErrorWithTracing(funcLogger, span, err)
		success = false
		return err
	}

	// Store vault token on credd
	if err := vaultToken.StoreAndValidateToken(ctx, ts, environ); err != nil {
		storeFailureCount.WithLabelValues(ts.GetServiceName(), ts.GetCredd()).Inc()

		err = fmt.Errorf("could not store or validate vault token: %w", err)
		logErrorWithTracing(funcLogger, span, err)
		return err
	}

	dur := time.Since(start).Seconds()
	tokenStoreTimestamp.WithLabelValues(ts.GetServiceName(), ts.GetCredd()).SetToCurrentTime()
	tokenStoreDuration.WithLabelValues(ts.GetServiceName(), ts.GetCredd()).Set(dur)

	logSuccessWithTracing(funcLogger, span, "Successfully stored and obtained vault tokens for schedds")
	return nil
}

// tokenStorer contains the methods needed to store a vault token in the condor credd and a hashicorp vault.  It should be passed into
// StoreAndValidateTokens so that any token that is stored is also validated
type tokenStorer interface {
	*vaultToken.InteractiveTokenStorer | *vaultToken.NonInteractiveTokenStorer
	GetServiceName() string
	GetCredd() string
	GetVaultServer() string
}
