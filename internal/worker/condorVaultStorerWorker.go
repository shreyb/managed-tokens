package worker

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/vaultToken"
)

const vaultStorerDefaultTimeoutStr string = "60s"

type vaultStorerSuccess struct {
	serviceName string
	success     bool
}

func (v *vaultStorerSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *vaultStorerSuccess) GetSuccess() bool {
	return v.success
}

func StoreAndGetTokenWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer close(chans.GetNotificationsChan())
	var interactive bool

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		success := &vaultStorerSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(v *vaultStorerSuccess) {
				chans.GetSuccessChan() <- v
			}(success)

			vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
			defer vaultStorerCancel()

			if err := vaultToken.StoreAndGetTokens(vaultStorerContext, sc.Service.Name(), sc.UserPrincipal, sc.CommandEnvironment, interactive); err != nil {
				msg := "Could not store and get vault tokens"
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error(msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.Service.Name())
			} else {
				success.success = true
			}
		}()
	}
}

func StoreAndGetRefreshAndVaultTokens(ctx context.Context, sc *service.Config) error {
	interactive := true

	vaultStorerTimeout, err := utils.GetProperTimeoutFromContext(ctx, vaultStorerDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout")
	}

	vaultContext, vaultCancel := context.WithTimeout(ctx, vaultStorerTimeout)
	defer vaultCancel()

	return vaultToken.StoreAndGetTokens(vaultContext, sc.Service.Name(), sc.UserPrincipal, sc.CommandEnvironment, interactive)
}
