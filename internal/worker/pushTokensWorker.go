package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"sync"
	"text/template"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/fileCopier"
	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
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
	service.Service
	success bool
	mux     sync.Mutex
}

func (p *pushTokenSuccess) changeSuccessValue(changeTo bool) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.success = changeTo
}

func init() {
	metrics.MetricsRegistry.MustRegister(tokenPushTime)
}

func (v *pushTokenSuccess) GetService() service.Service {
	return v.Service
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
		var successNodes, failNodes sync.Map

		pushSuccess := &pushTokenSuccess{
			Service: sc.Service,
			success: true,
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
			var nodeWg sync.WaitGroup
			for _, destinationNode := range sc.Nodes {
				nodeWg.Add(1)
				go func(destinationNode string) {
					defer nodeWg.Done()
					var filenameWg sync.WaitGroup

					// Channel for each goroutine launched below to send notifications on for later aggregation
					nChan := make(chan notifications.Notification, len(destinationFilenames))
					for _, destinationFilename := range destinationFilenames {
						filenameWg.Add(1)
						go func(destinationFilename string) {
							defer filenameWg.Done()
							pushContext, pushCancel := context.WithTimeout(ctx, pushTimeout)
							defer pushCancel()
							log.WithFields(log.Fields{
								"experiment": sc.Service.Experiment(),
								"role":       sc.Service.Role(),
								"node":       destinationNode,
							}).Debug("Attempting to push tokens to destination node")
							if err := pushToNode(pushContext, sc, sourceFilename, destinationNode, destinationFilename); err != nil {
								var notificationErrorString string
								if sc.IsNodeUnpingable(destinationNode) {
									notificationErrorString = fmt.Sprintf("Node %s was not pingable earlier prior to attempt to push tokens; ", destinationNode)
								}
								if pushContext.Err() != nil {
									if errors.Is(pushContext.Err(), context.DeadlineExceeded) {
										notificationErrorString = notificationErrorString + pushContext.Err().Error() + " (timeout error)"
									} else {
										notificationErrorString = notificationErrorString + pushContext.Err().Error()
									}
									log.WithFields(log.Fields{
										"experiment": sc.Service.Experiment(),
										"role":       sc.Service.Role(),
									}).Errorf("Error pushing vault tokens to destination node: %s", pushContext.Err())
								} else {
									notificationErrorString = notificationErrorString + err.Error()
									log.WithFields(log.Fields{
										"experiment": sc.Service.Experiment(),
										"role":       sc.Service.Role(),
									}).Error("Error pushing vault tokens to destination node")
								}
								pushSuccess.changeSuccessValue(false)
								failNodes.LoadOrStore(destinationNode, struct{}{})
								nChan <- notifications.NewPushError(notificationErrorString, sc.ServiceNameFromExperimentAndRole(), destinationNode)
							}
						}(destinationFilename)
					}

					// Since we're pushing the same file to two different locations,
					// only report one failure to push a file to a node.
					notificationsForwardDone := make(chan struct{})
					go func() {
						defer close(notificationsForwardDone)
						var once sync.Once
						sendNotificationFunc := func(n notifications.Notification) func() {
							return func() {
								chans.GetNotificationsChan() <- n
							}
						}
						for n := range nChan {
							once.Do(sendNotificationFunc(n))
						}
					}()
					filenameWg.Wait()          // Wait until all push operations for this node are complete
					close(nChan)               // Close aggregation chan
					<-notificationsForwardDone // Wait until we've forwarded the message on

					// Set tokenPushTime metric
					if pushSuccess.success {
						tokenPushTime.WithLabelValues(sc.Service.Name(), destinationNode).SetToCurrentTime()
						successNodes.LoadOrStore(destinationNode, struct{}{})
					}
				}(destinationNode)
				nodeWg.Wait()

				// Push the default role file.  We handle this separately from the tokens because it's a non-essential file to push,
				// so we don't want to have notifications going out ONLY saying that this push fails, if that's the case

				// Make sure we can get the destination filename
				destinationFilename, err := parseDefaultRoleFileTemplateFromConfig(sc)
				if err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
					}).Error("Could not obtain default role file destination.  Will not push the default role file")
					return
				}

				// Write the default role file to send
				defaultRoleFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_default_role_file_")
				if err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Service.Experiment(),
						"role":       sc.Service.Role(),
					}).Error("Error creating temporary file for default role string.  Will not push the default role file")
					return
				}
				// Remove the tempfile when we're done
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
					}).Error("Error writing default role string to temporary file.  Please clean up manually.  Will not push the default role file")
					return
				}

				// Send the tempfile to the destination node.  If we fail here, don't count this as an error
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

		successesSlice := make([]string, 0)
		failuresSlice := make([]string, 0)
		successNodes.Range(func(key, value any) bool {
			if keyVal, ok := key.(string); ok {
				successesSlice = append(successesSlice, keyVal)
			} else {
				log.Errorf("Error storing node in successesSlice:  corrupt data in successNodes sync.Map of type %T: %v", key, key)
			}
			return true
		})
		failNodes.Range(func(key, value any) bool {
			if keyVal, ok := key.(string); ok {
				failuresSlice = append(failuresSlice, keyVal)
			} else {
				log.Errorf("Error storing node in failesSlice:  corrupt data in failNodes sync.Map of type %T: %v", key, key)
			}
			return true
		})
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
		c.CommandEnvironment,
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

// Note that these three funcs were implemented as functions with the *Config object as an argument, and not
// with a pointer receiver, because they are not meant to be inherent behaviors of a *Config object.

// GetDefaultRoleFileTemplateValueFromExtras retrieves the default role file template value from the worker.Config,
// and asserts that it is a string.  Callers should check the bool return value to make sure the type assertion
// passes, for example:
//
//	c := worker.NewConfig( // various options )
//	// set the default role file template in here
//	tmplString, ok := GetDefaultRoleFileTemplateValueFromExtras(c)
//	if !ok { // handle missing or incorrect value }
func GetDefaultRoleFileTemplateValueFromExtras(c *Config) (string, bool) {
	defaultRoleFileTemplateString, ok := c.Extras[DefaultRoleFileTemplate].(string)
	return defaultRoleFileTemplateString, ok
}

// parseDefaultRoleFileTemplateFromConfig parses the default role file template and returns the string with
// the executed template string
func parseDefaultRoleFileTemplateFromConfig(c *Config) (string, error) {
	// Get default role file template string from *Config
	templateString, ok := GetDefaultRoleFileTemplateValueFromExtras(c)
	if !ok {
		msg := "could not retrieve default role file destination template from worker configuration"
		log.WithFields(log.Fields{
			"experiment": c.Service.Experiment(),
			"role":       c.Service.Role(),
		}).Error(msg)
		return "", errors.New(msg)
	}

	// Execute template
	defaultRoleFileTemplate := template.Must(template.New("defaultRoleFileTemplate").Parse(templateString))
	tmplArgs := *c
	var b strings.Builder
	if err := defaultRoleFileTemplate.Execute(&b, tmplArgs); err != nil {
		msg := "could not execute default role file destination template"
		log.WithFields(log.Fields{
			"experiment": c.Service.Experiment(),
			"role":       c.Service.Role(),
		}).Error(msg, ": ", err)
		return "", err
	}
	return b.String(), nil
}
