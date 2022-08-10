package worker

import (
	"context"
	"fmt"
	"os/user"

	"github.com/shreyb/managed-tokens/notifications"
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

type failureByNode struct {
	node string
	error
}

func PushTokensWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())

	pushTimeout, err := utils.GetProperTimeoutFromContext(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		failureChan := make(chan failureByNode, 1)
		failureDoneChan := make(chan string, 1)
		go aggregateFailuresForNotifications(failureChan, failureDoneChan)

		pushSuccess := &pushTokenSuccess{
			serviceName: sc.Service.Name(),
			success:     true,
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
			// TODO If it's a context timeout, tell the notifications
			for _, destinationNode := range sc.Nodes {
				for _, destinationFilename := range destinationFilenames {
					pushContext, pushCancel := context.WithTimeout(ctx, pushTimeout)
					defer pushCancel()
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
						"node":       destinationNode,
					}).Debug("Attempting to push tokens to destination node")
					if err := pushToNode(pushContext, sc, sourceFilename, destinationNode, destinationFilename); err != nil {
						log.WithFields(log.Fields{
							"experiment": sc.Service.Experiment(),
							"role":       sc.Service.Role(),
						}).Error("Error pushing tokens to destination node")
						pushSuccess.success = false
						failureChan <- failureByNode{
							node:  destinationNode,
							error: err,
						}
					}
				}
			}
			close(failureChan)
			failuresTable := <-failureDoneChan
			sc.NotificationsChan <- pushNotification(failuresTable, sc.Service.Name())
		}()
	}
}

func pushToNode(ctx context.Context, sc *service.Config, sourceFile, node, destinationFile string) error {
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

func pushNotification(message, service string) notifications.Notification {
	return notifications.Notification{
		Message:          message,
		Service:          service,
		NotificationType: notifications.RunError,
	}
}

func aggregateFailuresForNotifications(inChan <-chan failureByNode, doneChan chan<- string) {
	defer close(doneChan)
	m := make(map[string]error) // Errors per service by node
	for failure := range inChan {
		m[failure.node] = failure.error
	}

	if len(m) == 0 {
		return
	}

	helpText := "The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:"
	tableString := utils.PrepareTableStringFromMap(m, helpText)

	doneChan <- tableString
}
