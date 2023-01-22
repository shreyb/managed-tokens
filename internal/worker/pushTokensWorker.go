package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shreyb/managed-tokens/internal/fileCopier"
	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/utils"
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

// pushTokenSuccess is a type that conveys whether PushTokensWorker successfully pushes vault tokens to destination nodes for a service
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

// PushTokenWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// pushes vault tokens to all the configured destination nodes.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func PushTokensWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed PushTokensWorker Notifications Chan")
	}()

	pushTimeout, err := utils.GetProperTimeoutFromContext(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		successNodes := make(map[string]struct{})
		failNodes := make(map[string]struct{})

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
				}).Error("Could not switch kerberos cache")
				return
			}

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
						var notificationErrorString string
						if pushContext.Err() != nil {
							if errors.Is(pushContext.Err(), context.DeadlineExceeded) {
								notificationErrorString = pushContext.Err().Error() + " (timeout error)"
							} else {
								notificationErrorString = pushContext.Err().Error()
							}
							log.WithFields(log.Fields{
								"experiment": sc.Service.Experiment(),
								"role":       sc.Service.Role(),
							}).Errorf("Error pushing vault tokens to destination node: %s", pushContext.Err())
						} else {
							notificationErrorString = err.Error()
							log.WithFields(log.Fields{
								"experiment": sc.Service.Experiment(),
								"role":       sc.Service.Role(),
							}).Error("Error pushing vault tokens to destination node")
						}
						pushSuccess.success = false
						failNodes[destinationNode] = struct{}{}
						chans.GetNotificationsChan() <- notifications.NewPushError(notificationErrorString, sc.Service.Name(), destinationNode)
					}
				}
				if pushSuccess.success {
					tokenPushTime.WithLabelValues(sc.Service.Name(), destinationNode).SetToCurrentTime()
					successNodes[destinationNode] = struct{}{}
				}

				// Push the default role file.  We handle this separately from the tokens because it's a non-essential file to push,
				// so we don't want to have notifications going out ONLY saying that this push fails, if that's the case
				// Write our file
				defaultRoleFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_default_role_file_")
				if err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
					}).Error("Error creating temporary file for default role string")
					return
				}
				// Remove the file when we're done
				defer func() {
					if err := os.Remove(defaultRoleFile.Name()); err != nil {
						log.WithFields(log.Fields{
							"experiment": sc.Service.Experiment(),
							"role":       sc.Service.Role(),
							"filename":   defaultRoleFile.Name(),
						}).Error("Error deleting temporary file for default role string. Please clean up manually")
					}
				}()
				if _, err := defaultRoleFile.WriteString(sc.Role() + "\n"); err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
						"filename":   defaultRoleFile.Name(),
					}).Error("Error writing to temporary file for default role string. Please clean up manually")
				}

				// Send it to our node
				destinationFilename := fmt.Sprintf("/tmp/jobsub_default_role_%s_%d", sc.Experiment(), sc.DesiredUID)
				pushContext, pushCancel := context.WithTimeout(ctx, pushTimeout)
				defer pushCancel()
				if err := pushToNode(pushContext, sc, defaultRoleFile.Name(), destinationNode, destinationFilename); err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
						"node":       destinationNode,
					}).Error("Error pushing default role file to destination node")
				} else {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
						"node":       destinationNode,
					}).Debug("Success pushing default role file to destination node")
				}
			}

		}()

		successesSlice := make([]string, 0, len(successNodes))
		failuresSlice := make([]string, 0, len(failNodes))
		for successNode := range successNodes {
			successesSlice = append(successesSlice, successNode)
		}
		for failNode := range failNodes {
			failuresSlice = append(failuresSlice, failNode)
		}
		log.WithField("service", sc.Service.Name()).Infof("Successful nodes: %s", strings.Join(successesSlice, ", "))
		log.WithField("service", sc.Service.Name()).Infof("Failed nodes: %s", strings.Join(failuresSlice, ", "))
	}
}

// pushToNode copies a file from a specified source to a destination path, using the environment and account configured in the worker.Config object
func pushToNode(ctx context.Context, c *Config, sourceFile, node, destinationFile string) error {
	f := fileCopier.NewSSHFileCopier(
		sourceFile,
		c.Account,
		node,
		destinationFile,
		"",
		&c.CommandEnvironment,
	)

	if err := fileCopier.CopyToDestination(ctx, f); err != nil {
		log.WithFields(log.Fields{
			"experiment":          c.Service.Experiment(),
			"role":                c.Service.Role(),
			"sourceFilename":      sourceFile,
			"destinationFilename": destinationFile,
			"node":                node,
		}).Errorf("Could not copy file to destination")
		return err
	}
	log.WithFields(log.Fields{
		"experiment":          c.Service.Experiment(),
		"role":                c.Service.Role(),
		"sourceFilename":      sourceFile,
		"destinationFilename": destinationFile,
		"node":                node,
	}).Info("Success copying file to destination")
	return nil

}
