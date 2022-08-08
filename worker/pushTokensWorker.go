package worker

import (
	"context"
	"fmt"
	"os/user"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

const pushDefaultTimeoutStr string = "30s"

type pushTokenSuccess struct {
	serviceName string
	success     bool
}

func (v *pushTokenSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *pushTokenSuccess) GetSuccess() bool {
	return v.success
}

func PushTokensWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())

	pushTimeout, err := utils.GetProperTimeoutFromContext(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		pushSuccess := &pushTokenSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(p *pushTokenSuccess) {
				chans.GetSuccessChan() <- p
			}(pushSuccess)

			// kswitch
			if err := utils.SwitchKerberosCache(ctx, sc); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Could not switch utils caches")
				return
			}

			// TODO Hopefully we won't need this bit with the current UID if I can get htgettoken to write out vault tokens to a random tempfile
			// TODO Delete the source file.  Like with a defer os.Remove or something like that
			currentUser, err := user.Current()
			if err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error(err)
				return
			}
			currentUID := currentUser.Uid

			sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, sc.Service.Name())
			destinationFilenames := []string{
				fmt.Sprintf("/tmp/vt_u%d", sc.DesiredUID),
				fmt.Sprintf("/tmp/vt_u%d-%s", sc.DesiredUID, sc.Service.Name()),
			}

			// Send to nodes
			numFailures := 0
			for _, destinationNode := range sc.Nodes {
				for _, destinationFilename := range destinationFilenames {
					pushContext, pushCancel := context.WithTimeout(ctx, pushTimeout)
					defer pushCancel()
					if err := pushToNodes(pushContext, sc, sourceFilename, destinationNode, destinationFilename); err != nil {
						log.WithFields(log.Fields{
							"experiment": sc.Service.Experiment(),
							"role":       sc.Service.Role(),
						}).Error("Error pushing tokens to destination nodes")
						numFailures += 1
					}
				}
			}

			if numFailures == 0 {
				pushSuccess.success = true
			}
		}()

	}
}

func pushToNodes(ctx context.Context, sc *service.Config, sourceFile, node, destinationFile string) error {
	rsyncConfig := utils.NewRsyncSetup(
		sc.Account,
		node,
		destinationFile,
		"",
		&sc.CommandEnvironment,
	)

	if err := rsyncConfig.CopyToDestination(ctx, sourceFile); err != nil {
		log.WithFields(log.Fields{
			"experiment":          sc.Service.Experiment(),
			"role":                sc.Service.Role(),
			"sourceFilename":      sourceFile,
			"destinationFilename": destinationFile,
			"node":                node,
		}).Errorf("Could not copy file to destination")
		return err
	}
	log.WithFields(log.Fields{
		"experiment":          sc.Service.Experiment(),
		"role":                sc.Service.Role(),
		"sourceFilename":      sourceFile,
		"destinationFilename": destinationFile,
		"node":                node,
	}).Info("Success copying file to destination")
	return nil

}
