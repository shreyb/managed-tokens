package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

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
	// Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed StoreAndGetTokenWorker Notifications Chan")
	}()

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
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
			configLogger.Error(msg)
			chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.ServiceNameFromExperimentAndRole())
		} else {
			success.success = true
			configLogger.Info("Successfully got and stored vault tokens")
		}
		chans.GetSuccessChan() <- success
		vaultStorerCancel()
	}
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

	return StoreAndGetTokensForSchedds(vaultStorerContext, &sc.CommandEnvironment, sc.ServiceCreddVaultTokenPathRoot, tokenStorers...)
}

// StoreAndGetTokensForSchedds will store a refresh token on the condor-configured vault server, obtain vault and bearer tokens for a service
// using HTCondor executables, and store the vault token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// If there was an error with ANY of the schedds, StoreAndGetTokensForSchedds will return an error
func StoreAndGetTokensForSchedds(ctx context.Context, environ *environment.CommandEnvironment, tokenRootPath string, tokenStorers ...vaultToken.TokenStorer) error {
	services := make(map[string]struct{})

	var authNeededErrorPtr *vaultToken.ErrAuthNeeded
	var authErr error
	var success bool = true

	// If we get any errors here, we mark the whole operation as having failed.  Also, if we see any authentication errors,
	// we want to make sure the error we return from this func wraps that error.
	for _, tokenStorer := range tokenStorers {
		func(tokenStorer vaultToken.TokenStorer) {
			services[tokenStorer.GetServiceName()] = struct{}{}
			funcLogger := log.WithFields(log.Fields{
				"service": tokenStorer.GetServiceName(),
				"credd":   tokenStorer.GetCredd(),
			})
			start := time.Now()

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
					funcLogger.Error("Could not stage prior vault token or clear the current vault token.  Please investigate, and be aware that stale credentials may get stored.")
					success = false
				}
			}

			// Make sure we store whatever comes out of storing the vault token
			// Note that if this operation fails, assuming we got a condor vault token, that will
			// stick around unless something else cleans up vault tokens.  This is actually OK, since
			// eventually that vault token will expire, and htgettoken will determine that a new one is
			// needed.
			defer func() {
				if err = storeServiceTokenForCreddFile(tokenRootPath, tokenStorer.GetServiceName(), tokenStorer.GetCredd()); err != nil {
					funcLogger.Error("Could not store condor vault token for credd for future runs.  Please investigate")
				}
			}()

			// Store vault token on credd
			err = vaultToken.StoreAndValidateToken(ctx, tokenStorer, environ)
			if err != nil {
				success = false
				if errors.As(err, &authNeededErrorPtr) {
					authErr = err
				}
				storeFailureCount.WithLabelValues(tokenStorer.GetServiceName(), tokenStorer.GetCredd()).Inc()
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
		log.WithField("service(s)", logServiceEntry).Error(retErr.Error())
		return retErr
	}

	return nil
}

func getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd string) string {
	funcLogger := log.WithFields(log.Fields{
		"tokenRootPath": tokenRootPath,
		"service":       serviceName,
		"credd":         credd,
	})
	var uid string
	currentUser, err := user.Current()
	if err != nil {
		funcLogger.Error(`Could not get current user.  Will use string "000" instead`)
		uid = "000"
	} else {
		uid = currentUser.Uid
	}

	tokenFilename := fmt.Sprintf("vt_u%s-%s-%s", uid, credd, serviceName)
	return path.Join(tokenRootPath, tokenFilename)
}

// getCondorVaultTokenLocation returns the location of vault token that HTCondor uses based on the current user's UID
func getCondorVaultTokenLocation(serviceName string) string {
	var uid string
	currentUser, err := user.Current()
	if err != nil {
		log.WithField("service", serviceName).Error(`Could not get current user.  Will use string "000" instead`)
		uid = "000"
	} else {
		uid = currentUser.Uid
	}
	filename := fmt.Sprintf("vt_u%s-%s", uid, serviceName)
	return path.Join(os.TempDir(), filename)
}

// TODO funcs needed:
// TODO:  Now make sure that our mains set up the worker configs

// TODO unit test this as much as can be possible.  It may not be that possible to create the errors since

// backupCondorVaultToken looks in the standard HTCondor location for a vault token.  If it finds
// one there, it will create a backup copy.  It returns a function that will restore the backup
// copy to the standard HTCondor location.  This returned func should ideally be defer called
// by the caller.
func backupCondorVaultToken(serviceName string) (restorePriorTokenFunc func() error, retErr error) {
	funcLogger := log.WithField("service", serviceName)

	// Check for token at condorVaultTokenLocation, and move it out if needed
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)
	if _, err := os.Stat(condorVaultTokenLocation); !errors.Is(err, os.ErrNotExist) {
		// We had a vault token at condorVaultTokenLocation.  Move it to a temp file for now
		previousTokenTempFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_condor_vault_token")
		if err != nil {
			funcLogger.Debug("Could not create temp file for old token file")
			return nil, err
		}
		funcLogger.Debugf("condor vault token already exists at %s.  Moving to temp location %s", condorVaultTokenLocation, previousTokenTempFile.Name())
		// TODO:  Think about how to test this
		if err := os.Rename(condorVaultTokenLocation, previousTokenTempFile.Name()); err != nil {
			funcLogger.Error("Could not move currently-existing condor vault token to staging location")
			retErr = err
		}
		restorePriorTokenFunc = func() error {
			// TODO:  This part is not tested.  Think about how to do that
			if err := os.Rename(previousTokenTempFile.Name(), condorVaultTokenLocation); err != nil {
				// Create location in os.TempDir() that is stamped for possible later retrieval
				now := time.Now().Format(time.RFC3339)
				finalBackupLocation := path.Join(os.TempDir(), fmt.Sprintf("managed_tokens_vt_bak-%s-%s", serviceName, now))
				funcLogger.Errorf("Could not move previous token back to condor vault location.  Attempting to save it to %s", finalBackupLocation)
				if err := os.Rename(previousTokenTempFile.Name(), finalBackupLocation); err != nil {
					funcLogger.Errorf("Could not restore previously-existing vault token.  Will not delete backup copy made at %s", previousTokenTempFile.Name())
				}
				return errors.New("could not restore previous token")
			}
			return nil
		}
	}
	return
}

// stageStoredTokenFile checks to see if there already exists a vault token for the given service and
// credd.  If so, it will move that file to where HTCondor expects it (as defined by the return value of
// getCondorVaultLocation)
func stageStoredTokenFile(tokenRootPath, serviceName, credd string) error {
	funcLogger := log.WithFields(log.Fields{
		"service": serviceName,
		"credd":   credd,
	})
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)

	storedServiceCreddTokenLocation := getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd)
	if _, err := os.Stat(storedServiceCreddTokenLocation); errors.Is(err, os.ErrNotExist) {
		funcLogger.Infof("No service credd token exists at %s.", storedServiceCreddTokenLocation)
		return errNoServiceCreddToken
	}

	if err := os.Rename(storedServiceCreddTokenLocation, condorVaultTokenLocation); err != nil {
		funcLogger.Error("Could not move stored service-credd vault token into place.  Will attempt to remove file at condor vault token location to ensure that a fresh one is generated.")
		if err2 := os.Remove(condorVaultTokenLocation); err2 != nil {
			funcLogger.Error("Could not remove condor vault token after failure to move stored service-credd vault token into place.  Please investigate")
			return err2
		}
		return errMoveServiceCreddToken
	}

	funcLogger.Infof("Successfully moved stored token %s into place at %s", storedServiceCreddTokenLocation, condorVaultTokenLocation)
	return nil
}

// storeServiceTokenForCreddFile moves the vault token in the condor staging path (defined by getCondorVaultLocation)
// to the service-credd storage path (defined by getServiceTokenForCreddLocation)
func storeServiceTokenForCreddFile(tokenRootPath, serviceName, credd string) error {
	funcLogger := log.WithFields(log.Fields{
		"service": serviceName,
		"credd":   credd,
	})
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)
	storedServiceCreddTokenLocation := getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd)

	funcLogger.Debug("Attempting to move condor vault token to service-credd vault token storage path")
	err := os.Rename(condorVaultTokenLocation, storedServiceCreddTokenLocation)
	if err != nil {
		funcLogger.Errorf("Could not move condor vault token to service-credd vault storage path: %s", err)
		return err
	}
	funcLogger.Infof("Successfully moved condor vault token to service-credd vault storage path: %s", storedServiceCreddTokenLocation)
	return nil
}

var (
	errNoServiceCreddToken   = errors.New("no prior service credd token exists")
	errMoveServiceCreddToken = errors.New("could not move service credd token into place")
)
