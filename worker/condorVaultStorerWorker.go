package worker

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
)

const vaultTimeoutStr string = "60s"

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
	var interactive bool

	var vaultStorerTimeout time.Duration
	var ok bool
	var err error

	if vaultStorerTimeout, ok = utils.GetOverrideTimeoutFromContext(ctx); !ok {
		log.WithField("func", "StoreAndGetTokenWorker").Debug("No overrideTimeout set.  Will use default")
		vaultStorerTimeout, err = time.ParseDuration(vaultTimeoutStr)
		if err != nil {
			log.Fatal("Could not parse vault storer timeout duration")
		}
	}

	for sc := range chans.GetServiceConfigChan() {
		vaultStorerContext, vaultStorerCancel := context.WithTimeout(ctx, vaultStorerTimeout)
		defer vaultStorerCancel()

		success := &vaultStorerSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(v *vaultStorerSuccess) {
				chans.GetSuccessChan() <- v
			}(success)

			if err := utils.StoreAndGetTokens(vaultStorerContext, sc, interactive); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Could not store and get vault tokens")
			} else {
				success.success = true
			}
		}()
	}
}

func StoreAndGetRefreshAndVaultTokens(ctx context.Context, sc *service.Config) error {
	interactive := true

	vaultTimeout, err := time.ParseDuration(vaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse vault storer timeout duration")
	}
	vaultContext, vaultCancel := context.WithTimeout(ctx, vaultTimeout)
	defer vaultCancel()

	return utils.StoreAndGetTokens(vaultContext, sc, interactive)
}
