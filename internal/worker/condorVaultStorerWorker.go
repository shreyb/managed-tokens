package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/vaultToken"
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
	tokenStoreDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "managed_tokens",
			Name:      "token_store_duration_seconds",
			Help:      "Duration (in seconds) for a vault token to get stored in a condor credd",
			Buckets:   prometheus.LinearBuckets(5, 0.5, 20),
		},
		[]string{
			"service",
			"credd",
		},
	)
	storeFailureCount = prometheus.NewCounterVec(prometheus.CounterOpts{
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
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed StoreAndGetTokenWorker Notifications Chan")
	}()

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout")
	}

	// One goroutine per service config
	var wg sync.WaitGroup
	for sc := range chans.GetServiceConfigChan() {
		success := &vaultStorerSuccess{
			Service: sc.Service,
		}
		wg.Add(1)

		go func(sc *Config) {
			defer wg.Done()
			defer func(v *vaultStorerSuccess) {
				chans.GetSuccessChan() <- v
			}(success)

			configLogger := log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			})

			vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
			defer vaultStorerCancel()

			tokenStorers := make([]vaultToken.TokenStorer, 0, len(sc.Schedds))
			for _, schedd := range sc.Schedds {
				tokenStorers = append(tokenStorers, vaultToken.NewNonInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer))
			}

			if err := StoreAndGetTokensForSchedds(vaultStorerContext, &sc.CommandEnvironment, sc.Service.Name(), tokenStorers...); err != nil {
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
				configLogger.Error(msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
			} else {
				success.success = true
				configLogger.Info("Successfully got and stored vault tokens")
			}
		}(sc)
	}
	wg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

// StoreAndGetRefreshAndVaultTokens stores a refresh token in the configured vault, and obtain vault and bearer tokens.  It will
// display all the stdout from the underlying executables to screen.
func StoreAndGetRefreshAndVaultTokens(ctx context.Context, sc *Config) error {
	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout")
	}

	vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
	defer vaultStorerCancel()

	tokenStorers := make([]vaultToken.TokenStorer, 0, len(sc.Schedds))
	for _, schedd := range sc.Schedds {
		tokenStorers = append(tokenStorers, vaultToken.NewInteractiveTokenStorer(sc.Service.Name(), schedd, sc.VaultServer))
	}

	return StoreAndGetTokensForSchedds(vaultStorerContext, &sc.CommandEnvironment, sc.Service.Name(), tokenStorers...)
}

// StoreAndGetTokensForSchedds will store a refresh token on the condor-configured vault server, obtain vault and bearer tokens for a service
// using HTCondor executables, and store the vault token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// If there was an error with ANY of the schedds, StoreAndGetTokensForSchedds will return an error
func StoreAndGetTokensForSchedds(ctx context.Context, environ *environment.CommandEnvironment, serviceName string, tokenStorers ...vaultToken.TokenStorer) error {
	funcLogger := log.WithField("serviceName", serviceName)

	g := new(errgroup.Group)

	// One goroutine per TokenStorer
	for _, tokenStorer := range tokenStorers {
		tokenStorer := tokenStorer
		g.Go(func() error {
			start := time.Now()
			err := vaultToken.StoreAndValidateToken(ctx, tokenStorer, environ)
			if err != nil {
				storeFailureCount.WithLabelValues(serviceName, tokenStorer.GetCredd()).Inc()
			} else {
				dur := time.Since(start).Seconds()
				tokenStoreTimestamp.WithLabelValues(serviceName, tokenStorer.GetCredd()).SetToCurrentTime()
				tokenStoreDuration.WithLabelValues(serviceName, tokenStorer.GetCredd()).Observe(dur)
			}
			return err
		})
	}

	// Wait for all StoreAndValidateToken operations to complete
	if err := g.Wait(); err != nil {
		msg := "error obtaining and/or storing vault tokens for one or more credd"

		var retErr error
		var authNeededErrorPtr *vaultToken.ErrAuthNeeded
		if errors.As(err, &authNeededErrorPtr) {
			retErr = fmt.Errorf("%s: %w", msg, err)
		} else {
			retErr = errors.New(msg)
		}
		funcLogger.Error(retErr.Error())
		return retErr
	}

	return nil
}
