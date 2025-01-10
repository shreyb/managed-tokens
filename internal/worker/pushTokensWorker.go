// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/fileCopier"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
)

// Sleep time between each retry
const defaultRetrySleepDuration = 60 * time.Second

// Metrics
var (
	tokenPushTimestamp = prometheus.NewGaugeVec(
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
	tokenPushDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "managed_tokens",
			Name:      "token_push_duration_seconds",
			Help:      "Duration (in seconds) for a vault token to get pushed to a node",
		},
		[]string{
			"service",
			"node",
		},
	)
	pushFailureCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "failed_token_push_count",
		Help:      "The number of times the Managed Tokens service failed to push a token to an interactive node",
	},
		[]string{
			"service",
			"node",
		},
	)
)

const pushDefaultTimeoutStr string = "30s"

func init() {
	metrics.MetricsRegistry.MustRegister(tokenPushTimestamp)
	metrics.MetricsRegistry.MustRegister(tokenPushDuration)
	metrics.MetricsRegistry.MustRegister(pushFailureCount)
}

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

func (v *pushTokenSuccess) GetService() service.Service {
	return v.Service
}

func (v *pushTokenSuccess) GetSuccess() bool {
	return v.success
}

// PushTokenWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// pushes vault tokens to all the configured destination nodes.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func PushTokensWorker(ctx context.Context, chans channelGroup) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker")
	defer span.End()

	defer func() {
		chans.closeWorkerSendChans()
		log.Debug("Closed PushTokensWorker Notifications and Success Chans")
	}()

	pushTimeout, err := utils.GetProperTimeoutFromContext(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}

	var configWg sync.WaitGroup
	for sc := range chans.serviceConfigChan {
		var successNodes, failNodes sync.Map
		serviceLogger := log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
		})

		pushSuccess := &pushTokenSuccess{
			Service: sc.Service,
			success: true,
		}
		configWg.Add(1)

		go func(sc *Config) {
			defer configWg.Done()
			ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker_anonFunc")
			span.SetAttributes(
				attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
			)
			defer span.End()

			func() {
				defer func(p *pushTokenSuccess) {
					chans.successChan <- p
				}(pushSuccess)

				// Prepare default role file for the service
				// Make sure we can get the destination filename
				// Write the default role file to send

				var dontSendDefaultRoleFile bool
				defaultRoleFileDestinationFilename, err := parseDefaultRoleFileDestinationTemplateFromConfig(sc)
				if err != nil {
					serviceLogger.Error("Could not obtain default role file destination.  Will not push the default role file")
					dontSendDefaultRoleFile = true
				}
				defaultRoleFileName, err := prepareDefaultRoleFile(sc)
				if err != nil {
					serviceLogger.Error("Could not prepare default role.  Will not push the default role file")
					dontSendDefaultRoleFile = true
				}
				sendDefaultRoleFile := !dontSendDefaultRoleFile

				// Remove the tempfile when we're done
				defer func() {
					if err := os.Remove(defaultRoleFileName); err != nil {
						serviceLogger.WithField("filename", defaultRoleFileName).Error("Error deleting temporary file for default role string. Please clean up manually")
					}
				}()

				sourceFilename, err := findFirstCreddVaultToken(sc.ServiceCreddVaultTokenPathRoot, sc.Service.Name(), sc.Schedds)
				if err != nil {
					msg := "Could not find suitable vault token to push.  Will not push any vault tokens for this service."
					tracing.LogErrorWithTrace(span, serviceLogger, msg)
					pushSuccess.changeSuccessValue(false)
					chans.notificationsChan <- notifications.NewSetupError(msg, sc.Service.Name())
					return
				}

				destinationFilenames := []string{
					fmt.Sprintf("/tmp/vt_u%d", sc.DesiredUID),
					fmt.Sprintf("/tmp/vt_u%d-%s", sc.DesiredUID, sc.Service.Name()),
				}

				// Retry values
				numRetries, err := getWorkerNumRetriesValueFromConfig(*sc, PushTokensWorkerType)
				if err != nil {
					serviceLogger.Debug("Could not get retry value from config.  Using default value")
					numRetries = 0
				}
				retrySleepDuration, err := getWorkerRetrySleepValueFromConfig(*sc, PushTokensWorkerType)
				if err != nil {
					serviceLogger.Debug("Could not get retry sleep value from config.  Using default value")
					retrySleepDuration = defaultRetrySleepDuration
				}

				// Send to nodes
				var nodeWg sync.WaitGroup
				for _, destinationNode := range sc.Nodes {
					nodeWg.Add(1)
					go func(destinationNode string) {
						defer nodeWg.Done()

						ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker_anonFunc_nodeAnonFunc")
						span.SetAttributes(
							attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
							attribute.String("node", destinationNode),
						)
						defer span.End()

						nodeLogger := serviceLogger.WithField("node", destinationNode)

						var filenameWg sync.WaitGroup

						// Channel for each goroutine launched below to send notifications on for later aggregation
						nChan := make(chan notifications.Notification, len(destinationFilenames))
						nodeStatus := struct {
							success bool
							mux     sync.Mutex
						}{success: true}

						for _, destinationFilename := range destinationFilenames {
							filenameWg.Add(1)
							go func(destinationFilename string) {
								defer filenameWg.Done()

								ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker_anonFunc_nodeAnonFunc_filenameAnonFunc")
								span.SetAttributes(
									attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
									attribute.String("node", destinationNode),
									attribute.String("filename", destinationFilename),
								)
								defer span.End()

								pushTimeoutWithRetries := time.Duration((int(pushTimeout+retrySleepDuration) * (int(numRetries) + 1)))
								pushContext, pushCancel := context.WithTimeout(ctx, pushTimeoutWithRetries)
								defer pushCancel()

								var notificationErrorString string
								var err error
								func() {
									for i := 0; i <= int(numRetries); i++ {
										trialContext, trialSpan := otel.GetTracerProvider().Tracer("managed-tokens").Start(pushContext, "worker.PushTokensWorker_anonFunc_nodeAnonFunc_filenameAnonFunc_trialAnonFunc")
										trialSpan.SetAttributes(
											attribute.String("service", sc.ServiceNameFromExperimentAndRole()),
											attribute.String("node", destinationNode),
											attribute.String("filename", destinationFilename),
											attribute.Int("try", i+1),
										)
										defer trialSpan.End()
										_trialContext, _trialCancel := context.WithTimeout(trialContext, pushTimeout)
										defer _trialCancel()

										nodeLogger.Debugf("Attempting to push tokens to destination node, try %d, %d retries left", i+1, int(numRetries)-i)
										err = pushToNode(_trialContext, sc, sourceFilename, destinationNode, destinationFilename)
										// Success
										if err == nil {
											return
										}
										// Unpingable node - no reason to retry
										if sc.IsNodeUnpingable(destinationNode) {
											notificationErrorString = fmt.Sprintf("Node %s was not pingable earlier prior to attempt to push tokens; ", destinationNode)
											tracing.LogErrorWithTrace(trialSpan, nodeLogger, notificationErrorString+"will not retry")
											return
										}
										// Context has errored out - no reason to retry
										if _trialContext.Err() != nil {
											notificationErrorString = notificationErrorString + pushContext.Err().Error()
											if errors.Is(_trialContext.Err(), context.DeadlineExceeded) {
												notificationErrorString = notificationErrorString + " (timeout error)"
											}
											tracing.LogErrorWithTrace(trialSpan, nodeLogger, fmt.Sprintf("Error pushing vault tokens to destination node: %s: will not retry", pushContext.Err()))
											return
										}
										// Some other error - retry
										notificationErrorString = notificationErrorString + err.Error()
										nodeLogger.Errorf("Error pushing vault tokens to destination node.  Will sleep %s and then retry", retrySleepDuration.String())
										time.Sleep(retrySleepDuration)
										_trialCancel()
									}
								}()
								if err != nil {
									pushSuccess.changeSuccessValue(false) // Mark the whole service config as failed

									// Mark this node as failed
									failNodes.LoadOrStore(destinationNode, struct{}{})
									func() {
										nodeStatus.mux.Lock()
										defer nodeStatus.mux.Unlock()
										nodeStatus.success = false
									}()

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
									chans.notificationsChan <- n
								}
							}
							for n := range nChan {
								once.Do(sendNotificationFunc(n))
							}
						}()
						filenameWg.Wait()          // Wait until all push operations for this node are complete
						close(nChan)               // Close aggregation chan
						<-notificationsForwardDone // Wait until we've forwarded the message on

						// Set tokenPushTimestamp metric
						if nodeStatus.success {
							span.SetStatus(codes.Ok, "Successfully pushed tokens to node")
							tokenPushTimestamp.WithLabelValues(sc.Service.Name(), destinationNode).SetToCurrentTime()
							successNodes.LoadOrStore(destinationNode, struct{}{})
						}

						// Send the default role file to the destination node.  If we fail here, don't count this as an error
						if sendDefaultRoleFile {
							pushContext, pushCancel := context.WithTimeout(ctx, pushTimeout)
							defer pushCancel()
							if err := pushToNode(pushContext, sc, defaultRoleFileName, destinationNode, defaultRoleFileDestinationFilename); err != nil {
								serviceLogger.WithField("node", destinationNode).Error("Error pushing default role file to destination node")
							} else {
								serviceLogger.WithField("node", destinationNode).Debug("Success pushing default role file to destination node")
							}
						}
					}(destinationNode)
					nodeWg.Wait()
				}
			}()

			// Aggreagte our successful and failed pushes here
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
		}(sc)
	}
	configWg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

// pushToNode copies a file from a specified source to a destination path, using the environment and account configured in the worker.Config object
func pushToNode(ctx context.Context, c *Config, sourceFile, node, destinationFile string) error {
	startTime := time.Now()
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.pushToNode")
	span.SetAttributes(
		attribute.String("service", c.ServiceNameFromExperimentAndRole()),
		attribute.String("node", node),
		attribute.String("sourceFilename", sourceFile),
		attribute.String("destinationFilename", destinationFile),
	)
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"experiment":          c.Service.Experiment(),
		"role":                c.Service.Role(),
		"sourceFilename":      sourceFile,
		"destinationFilename": destinationFile,
		"node":                node,
	})

	var fileCopierOptions []string
	fileCopierOptions, ok := GetFileCopierOptionsFromExtras(c)
	if !ok {
		log.WithField("service", c.Service.Name()).Error(`Stored FileCopierOptions in config is not a string. Using default value of ""`)
		fileCopierOptions = []string{}
	}

	var sshOptions []string
	sshOptions, ok = GetSSHOptionsFromExtras(c)
	if !ok {
		log.WithField("service", c.Service.Name()).Error(`Stored SSHOptions in config is not a []string. Using default value of []string{}`)
		sshOptions = []string{}
	}

	f := fileCopier.NewSSHFileCopier(
		sourceFile,
		c.Account,
		node,
		destinationFile,
		fileCopierOptions,
		sshOptions,
		c.CommandEnvironment,
	)

	err := fileCopier.CopyToDestination(ctx, f)
	if err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Could not copy file to destination")
		pushFailureCount.WithLabelValues(c.Service.Name(), node).Inc()
		return err
	}

	dur := time.Since(startTime).Seconds()
	tokenPushDuration.WithLabelValues(c.Service.Name(), node).Set(dur)
	tracing.LogSuccessWithTrace(span, funcLogger, "Success copying file to destination")
	return nil

}

// Note that these funcs were implemented as functions with the *Config object as an argument, and not
// with a pointer receiver, because they are not meant to be inherent behaviors of a *Config object.

// parseDefaultRoleFileTemplateFromConfig parses the default role file template and returns the string with
// the executed template string
func parseDefaultRoleFileDestinationTemplateFromConfig(c *Config) (string, error) {
	// Get default role file template string from *Config
	funcLogger := log.WithFields(log.Fields{
		"experiment": c.Service.Experiment(),
		"role":       c.Service.Role(),
	})
	templateString, ok := GetDefaultRoleFileDestinationTemplateValueFromExtras(c)
	if !ok {
		msg := "could not retrieve default role file destination template from worker configuration"
		funcLogger.Error(msg)
		return "", errors.New(msg)
	}

	// Execute template
	defaultRoleFileTemplate := template.Must(template.New("defaultRoleFileTemplate").Parse(templateString))
	tmplArgs := *c
	var b strings.Builder
	if err := defaultRoleFileTemplate.Execute(&b, tmplArgs); err != nil {
		msg := "could not execute default role file destination template"
		funcLogger.Error(msg, ": ", err)
		return "", err
	}
	return b.String(), nil
}

// prepareDefaultRoleFile prepares the role file for the given service config
func prepareDefaultRoleFile(sc *Config) (string, error) {
	serviceLogger := log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
	})
	defaultRoleFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_default_role_file_")
	if err != nil {
		serviceLogger.Error("Error creating temporary file for default role string.  Will not push the default role file")
		return "", err
	}
	if _, err := defaultRoleFile.WriteString(sc.Role() + "\n"); err != nil {
		serviceLogger.WithField("filename", defaultRoleFile.Name()).Error("Error writing default role string to temporary file.  Please clean up manually.  Will not push the default role file")
		return "", err
	}
	serviceLogger.WithField("filename", defaultRoleFile.Name()).Debug("Wrote default role file to transfer to nodes")
	return defaultRoleFile.Name(), nil
}

// findFirstCreddVaultToken will cycle through the credds slice in an order determined by slices.Sort() and attempt to find a stored vault token for
// a credd at the tokenRootPath.  If this fails, it will return the location used by condor_vault_storer to store tokens
func findFirstCreddVaultToken(tokenRootPath, serviceName string, credds []string) (string, error) {
	funcLogger := log.WithField("service", serviceName)

	if len(credds) == 0 {
		return "", errors.New("no credds given")
	}

	creddsSorted := make([]string, len(credds))
	copy(creddsSorted, credds)
	slices.Sort(creddsSorted)

	// Check each possible credd path for the token file.  If we find one, use it
	for i := 0; i < len(creddsSorted); i++ {
		trialCredd := creddsSorted[i]
		trialPath := getServiceTokenForCreddLocation(tokenRootPath, serviceName, trialCredd)

		if _, err := os.Stat(trialPath); err == nil {
			funcLogger.WithFields(log.Fields{
				"credd":     trialCredd,
				"tokenPath": trialPath,
			}).Debug("Returning credd vault token location")
			return trialPath, nil
		}
	}

	// We didn't find a usable vault token at the tokenRootPath, so try /tmp for a condor vault token
	trialPath := getCondorVaultTokenLocation(serviceName)
	if _, err := os.Stat(trialPath); err == nil {
		funcLogger.WithFields(log.Fields{
			"tokenPath": trialPath,
		}).Debug("No credd-specific vault token was found, but there was a vault token at the condor vault token location. Returning condor vault token location")
		return trialPath, nil
	}

	return "", errors.New("could not find any vault tokens to return")
}
