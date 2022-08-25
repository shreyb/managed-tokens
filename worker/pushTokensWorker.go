package worker

import (
	"context"
	"fmt"
	"os/user"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shreyb/managed-tokens/fileCopier"
	"github.com/shreyb/managed-tokens/kerberos"
	"github.com/shreyb/managed-tokens/metrics"
	"github.com/shreyb/managed-tokens/notifications"
	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

var tokenPushTime = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "last_token_push_timestamp",
		Help:      "The timestamp of the last successful push of a service vault token to an interactive node by the Managed Tokens Service",
	},
	[]string{
		"service",
		"node",
	},
)

const pushDefaultTimeoutStr string = "30s"

type pushTokenSuccess struct {
	serviceName string
	success     bool
}

func init() {
	metrics.MetricsRegistry.MustRegister(tokenPushTime)
}

func (v *pushTokenSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *pushTokenSuccess) GetSuccess() bool {
	return v.success
}

func PushTokensWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer close(chans.GetNotificationsChan())

	pushTimeout, err := utils.GetProperTimeoutFromContext(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		pushSuccess := &pushTokenSuccess{
			serviceName: sc.Service.Name(),
			success:     true,
		}

		func() {
			defer func(p *pushTokenSuccess) {
				chans.GetSuccessChan() <- p
			}(pushSuccess)

			// kswitch
			if err := kerberos.SwitchCache(ctx, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
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
						}).Error("Error pushing vault tokens to destination node")
						pushSuccess.success = false
						chans.GetNotificationsChan() <- notifications.NewPushError(err.Error(), sc.Service.Name(), destinationNode)
					}
				}
				if pushSuccess.success {
					tokenPushTime.WithLabelValues(sc.Service.Name(), destinationNode).SetToCurrentTime()
				}
			}
			// TODO Do an INFO logging of successes and failures across service
		}()
	}
}

func pushToNode(ctx context.Context, sc *service.Config, sourceFile, node, destinationFile string) error {
	f := fileCopier.NewSSHFileCopier(
		sourceFile,
		sc.Account,
		node,
		destinationFile,
		"",
		&sc.CommandEnvironment,
	)

	if err := fileCopier.CopyToDestination(ctx, f); err != nil {
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
