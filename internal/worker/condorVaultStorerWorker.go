package worker

import (
	"context"
	"errors"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/vaultToken"
)

const vaultStorerDefaultTimeoutStr string = "60s"

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
	var interactive bool

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

			if err := StoreAndGetTokensForSchedds(vaultStorerContext, sc.UserPrincipal, sc.Service.Name(), sc.Schedds, sc.VaultServer, sc.CommandEnvironment, interactive); err != nil {
				var msg string
				if errors.Is(err, context.DeadlineExceeded) {
					msg = "Timeout error"
				} else {
					msg = "Could not store and get vault tokens"
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
	interactive := true

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout")
	}

	vaultContext, vaultCancel := context.WithTimeout(ctx, vaultStorerTimeout)
	defer vaultCancel()

	return StoreAndGetTokensForSchedds(vaultContext, sc.UserPrincipal, sc.Service.Name(), sc.Schedds, sc.VaultServer, sc.CommandEnvironment, interactive)
}

// StoreAndGetTokensForSchedds will store a refresh token on the condor-configured vault server, obtain vault and bearer tokens for a service
// using HTCondor executables, and store the vault token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// If there was an error with ANY of the schedds, StoreAndGetTokensForSchedds will return an error
func StoreAndGetTokensForSchedds(ctx context.Context, userPrincipal, serviceName string, schedds []string, vaultServer string, environ environment.CommandEnvironment, interactive bool) error {
	// TODO:  Here or in the executable we need to read in SEC_CREDENTIAL_GETTOKEN_OPTS and pass it into the call to vaultTOken.StoreAndValidateTokens.
	var wg sync.WaitGroup
	funcLogger := log.WithField("serviceName", serviceName)

	// Listener for all of the getTokensAndStoreInVault goroutines
	var isError bool
	errorCollectionDone := make(chan struct{}) // Channel to close when we're done determining if there was an error or not
	errChan := make(chan error, len(schedds))
	go func() {
		defer close(errorCollectionDone)
		for err := range errChan {
			if err != nil {
				isError = true
			}
		}
	}()

	// One goroutine per schedd
	for _, schedd := range schedds {
		wg.Add(1)
		go func(schedd string) {
			defer wg.Done()
			if err := vaultToken.StoreAndValidateTokens(ctx, serviceName, schedd, vaultServer, &environ, interactive); err != nil {
				errChan <- err
			}
			errChan <- nil
		}(schedd)
	}
	wg.Wait()
	close(errChan)
	<-errorCollectionDone // Don't inspect isError until we've given all vault storing goroutines chance to report an error

	if isError {
		msg := "Error obtaining and/or storing vault tokens for one or more credd"
		funcLogger.Errorf(msg)
		return errors.New(msg)
	}
	return nil
}
