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
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
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
func StoreAndGetTokenWorker(ctx context.Context, chans ChannelsForWorkers) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokenWorker")
	defer span.End()

	// Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed StoreAndGetTokenWorker Notifications Chan")
	}()

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		span.SetStatus(codes.Error, "Could not parse vault storer timeout")
		log.Fatal("Could not parse vault storer timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		success := &vaultStorerSuccess{
			Service: sc.Service,
		}

		configLogger := log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
			"service":    sc.Name(),
		})

		vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)

		tokenStorers := make([]vaultToken.TokenStorer, 0, len(sc.Schedds))
		for _, schedd := range sc.Schedds {
			tokenStorers = append(tokenStorers, vaultToken.NewNonInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer))
		}

		if err := StoreAndGetTokensForSchedds(vaultStorerContext, &sc.CommandEnvironment, sc.ServiceCreddVaultTokenPathRoot, tokenStorers...); err != nil {
			var msg string
			if errors.Is(err, context.DeadlineExceeded) {
				msg = "Timeout error"
			} else {
				msg = "Could not store and get vault tokens"
				unwrappedErr := errors.Unwrap(err)
				if unwrappedErr != nil {
					var authNeededErrorPtr *vaultToken.ErrAuthNeeded
					if errors.As(unwrappedErr, &authNeededErrorPtr) {
						msg = fmt.Sprintf("%s: %s", msg, unwrappedErr.Error())
					}
				}
			}
			tracing.LogErrorWithTrace(span, configLogger, msg)
			chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
		} else {
			success.success = true
			tracing.LogSuccessWithTrace(span, configLogger, "Successfully got and stored vault tokens")
		}
		chans.GetSuccessChan() <- success
		vaultStorerCancel()
	}
}

// StoreAndGetRefreshAndVaultTokens stores a refresh token in the configured vault, and obtain vault and bearer tokens.  It will
// display all the stdout from the underlying executables to screen.
func StoreAndGetRefreshAndVaultTokens(ctx context.Context, sc *Config) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetRefreshAndVaultTokens")
	span.SetAttributes(attribute.String("service", sc.ServiceNameFromExperimentAndRole()))
	defer span.End()

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		span.SetStatus(codes.Error, "Could not parse vault storer timeout")
		log.Fatal("Could not parse vault storer timeout")
	}

	vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
	defer vaultStorerCancel()

	tokenStorers := make([]vaultToken.TokenStorer, 0, len(sc.Schedds))
	for _, schedd := range sc.Schedds {
		tokenStorers = append(tokenStorers, vaultToken.NewInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer))
	}

	return StoreAndGetTokensForSchedds(vaultStorerContext, &sc.CommandEnvironment, sc.ServiceCreddVaultTokenPathRoot, tokenStorers...)
}

// StoreAndGetTokensForSchedds will store a refresh token on the condor-configured vault server, obtain vault and bearer tokens for a service
// using HTCondor executables, and store the vault token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// If there was an error with ANY of the schedds, StoreAndGetTokensForSchedds will return an error
func StoreAndGetTokensForSchedds(ctx context.Context, environ *environment.CommandEnvironment, tokenRootPath string, tokenStorers ...vaultToken.TokenStorer) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokensForSchedds")
	span.SetAttributes(attribute.String("tokenRootPath", tokenRootPath))
	defer span.End()

	services := make(map[string]struct{})

	var authNeededErrorPtr *vaultToken.ErrAuthNeeded
	var authErr error
	var success bool = true

	// If we get any errors here, we mark the whole operation as having failed.  Also, if we see any authentication errors,
	// we want to make sure the error we return from this func wraps that error.
	for _, tokenStorer := range tokenStorers {
		func(tokenStorer vaultToken.TokenStorer) {
			ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.StoreAndGetTokensForSchedds_anonFunc")
			span.SetAttributes(attribute.String("service", tokenStorer.GetServiceName()))
			span.SetAttributes(attribute.String("credd", tokenStorer.GetCredd()))
			defer span.End()

			services[tokenStorer.GetServiceName()] = struct{}{}
			funcLogger := log.WithFields(log.Fields{
				"service": tokenStorer.GetServiceName(),
				"credd":   tokenStorer.GetCredd(),
			})
			start := time.Now()

			// Before we stage any prior vault token, check to make sure our context hasn't already been canceled
			if ctx.Err() != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "context was canceled or the deadline exceeded before token vault staging.  Will not attempt to stage a stored token file or store vault token")
				success = false
				return
			}

			// Stage prior vault token, if it exists
			restorePriorTokenFunc, err := backupCondorVaultToken(tokenStorer.GetServiceName())
			if err != nil {
				funcLogger.Errorf("Error backing up current vault token at %s.  Will overwrite this with a new vault token.", getCondorVaultTokenLocation(tokenStorer.GetServiceName()))
			}
			defer func() {
				if err := restorePriorTokenFunc(); err != nil {
					funcLogger.Errorf("Error restoring prior condor vault token.  Please see the logs to see where the token might have been backed up.")
				}
			}()

			if err = stageStoredTokenFile(tokenRootPath, tokenStorer.GetServiceName(), tokenStorer.GetCredd()); err != nil {
				switch {
				case errors.Is(err, errNoServiceCreddToken):
					funcLogger.Info("No prior vault token exists for this service/credd combination.  Will get a new vault token")
				case errors.Is(err, errMoveServiceCreddToken):
					funcLogger.Warn("There was an error staging the prior vault token for this service and credd.  Will get a new vault token")
				default:
					tracing.LogErrorWithTrace(span, funcLogger, "Could not stage prior vault token.  Please investigate, and be aware that stale credentials may get stored.")
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
					if err = storeServiceTokenForCreddFile(tokenRootPath, tokenStorer.GetServiceName(), tokenStorer.GetCredd()); err != nil {
						funcLogger.Error("Could not store condor vault token for credd for future runs.  Please investigate")
					}
				}
			}()

			// Last context check again here:  before we store vault token, check to make sure our context hasn't already been canceled
			if ctx.Err() != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "context was canceled or the deadline exceeded before token vault storage.  Will not attempt to store vault token")
				success = false
				return
			}

			// Store vault token on credd
			err = vaultToken.StoreAndValidateToken(ctx, tokenStorer, environ)
			if err != nil {
				success = false
				if errors.As(err, &authNeededErrorPtr) {
					authErr = err
				}
				storeFailureCount.WithLabelValues(tokenStorer.GetServiceName(), tokenStorer.GetCredd()).Inc()
				span.SetStatus(codes.Error, "could not store or validate vault token")
				return
			}

			dur := time.Since(start).Seconds()
			tokenStoreTimestamp.WithLabelValues(tokenStorer.GetServiceName(), tokenStorer.GetCredd()).SetToCurrentTime()
			tokenStoreDuration.WithLabelValues(tokenStorer.GetServiceName(), tokenStorer.GetCredd()).Set(dur)
		}(tokenStorer)
	}

	if !success {
		var retErr error
		logServices := make([]string, 0, len(services))

		for service := range services {
			logServices = append(logServices, service)
		}
		logServiceEntry := strings.Join(logServices, ", ")

		msg := "error obtaining and/or storing vault tokens for one or more credd"
		if authErr != nil {
			retErr = fmt.Errorf("%s: %w", msg, authErr)
		} else {
			retErr = errors.New(msg)
		}
		tracing.LogErrorWithTrace(span, log.WithField("service(s)", logServiceEntry), retErr.Error())
		return retErr
	}

	span.SetStatus(codes.Ok, "Successfully stored and obtained vault tokens for schedds")
	return nil
}
