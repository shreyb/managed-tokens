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

	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/fileCopier"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
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

	pushTimeout, defaultUsed, err := contextStore.GetProperTimeout(ctx, pushDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse push timeout")
	}
	if defaultUsed {
		log.Debug("Using default timeout for pushing tokens")
	}

	type nodeMap struct {
		m   map[string]any
		mux sync.Mutex
	}

	var configWg sync.WaitGroup
	for sc := range chans.serviceConfigChan {
		configWg.Add(1)
		go func(sc *Config) {
			defer configWg.Done()
			ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker.serviceConfig")
			defer span.End()
			span.SetAttributes(
				attribute.String("service", sc.Service.Name()),
			)

			serviceLogger := log.WithFields(log.Fields{
				"service":    sc.Service.Name(),
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			})

			var successNodes, failNodes nodeMap // Maps to store successful and failed nodes
			var nodesNotifyOnce nodeMap         // Map to store sync.Once values for each node, so we only send one
			// notification per node on chans.notificationsChan

			// Preload successNodes and nodesNotifyOnce
			successNodes.m = make(map[string]any)
			nodesNotifyOnce.m = make(map[string]any)
			for _, node := range sc.Nodes {
				successNodes.m[node] = struct{}{}
				nodesNotifyOnce.m[node] = &sync.Once{}
			}

			// Initialize failNodes
			failNodes.m = make(map[string]any)

			// Push success value for this service config
			pushSuccess := &pushTokenSuccess{
				Service: sc.Service,
				success: true,
			}
			defer func(p *pushTokenSuccess) {
				chans.successChan <- p
			}(pushSuccess)

			// Extract values from the service config that we need, compile those into a slice
			pushConfigs, err := getPushTokensValuesFromConfig(sc)
			if err != nil {
				tracing.LogErrorWithTrace(span, serviceLogger, err.Error())
				pushSuccess.changeSuccessValue(false)
				chans.notificationsChan <- notifications.NewSetupError("Error retrieving one or more values from the configuration to push tokens", sc.Service.Name())
				return
			}

			// Errgroup for all pushConfigs
			var g errgroup.Group

			// For each pushConfig, try to push to node concurrently
			for _, pc := range pushConfigs {
				if pc.cleanupFunc != nil {
					defer func() {
						if err := pc.cleanupFunc(); err != nil {
							serviceLogger.Error("Error cleaning up after pushing files.  Please investigate and clean up manually if needed")
						}
					}()
				}

				g.Go(func() error {
					ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "worker.PushTokensWorker.serviceConfig.pushConfig")
					defer span.End()
					span.SetAttributes(
						attribute.String("service", sc.Service.Name()),
						attribute.String("node", pc.node),
						attribute.String("account", pc.account),
						attribute.String("sourceFilename", pc.sourcePath),
						attribute.String("destinationFilename", pc.destinationPath),
					)

					pushConfigLogger := serviceLogger.WithFields(log.Fields{
						"node":                pc.node,
						"account":             pc.account,
						"sourceFilename":      pc.sourcePath,
						"destinationFilename": pc.destinationPath,
					})

					// Add timeout to context
					pushContext, cancel := context.WithTimeout(ctx, pushTimeout)
					defer cancel()

					err := pushToNode(pushContext, sc, pc.sourcePath, pc.node, pc.destinationPath, int(pc.numRetries), pc.retrySleepDuration)
					if err != nil && pc.errorOnFail {
						errMsg := fmt.Sprintf("Error pushing vault tokens to destination node %s", pc.node)
						pushConfigLogger.Errorf("%s: %s", errMsg, err.Error())

						// Mark this node as failed by adding it to failNodes and removing it from successNodes
						failNodes.mux.Lock()
						failNodes.m[pc.node] = struct{}{}
						failNodes.mux.Unlock()

						successNodes.mux.Lock()
						delete(successNodes.m, pc.node)
						successNodes.mux.Unlock()

						// Send notification for this node, if it's not already been done
						func() {
							nodesNotifyOnce.mux.Lock()
							defer nodesNotifyOnce.mux.Unlock()

							// Make sure our node is in the nodesNotifyOnce.m map, and that the value is a *sync.Once
							_val, ok := nodesNotifyOnce.m[pc.node] // Is this node a key in the map?
							if !ok {
								return
							}
							once, ok := _val.(*sync.Once) // Is the value a *sync.Once?
							if !ok {
								return
							}

							// Use the *sync.Once for this node to send a notification if it hasn't already been done
							once.Do(func() {
								_add := err.Error()
								if errors.Is(err, context.DeadlineExceeded) {
									_add = "(timeout error)"
								}
								chans.notificationsChan <- notifications.NewPushError(
									fmt.Sprintf("%s: %s", errMsg, _add),
									sc.ServiceNameFromExperimentAndRole(),
									pc.node)
							})
						}()

						// Increment our push failure count metric for this node
						pushFailureCount.WithLabelValues(sc.Service.Name(), pc.node).Inc()
						return err
					}

					return nil
				})
			}

			// Wait until all pushConfigs have been processed
			if err := g.Wait(); err != nil {
				pushSuccess.changeSuccessValue(false)
				tracing.LogErrorWithTrace(span, serviceLogger, "Error pushing tokens to one or more nodes")
			} else {
				span.SetStatus(codes.Ok, "Successfully pushed tokens to nodes")
				for node := range successNodes.m {
					tokenPushTimestamp.WithLabelValues(sc.Service.Name(), node).SetToCurrentTime()
				}
			}
			// In Go 1.23, we can possibly change this to use maps.Keys() to return an iter.Seq
			successesSlice := make([]string, 0, len(successNodes.m))
			failuresSlice := make([]string, 0, len(failNodes.m))

			successNodes.mux.Lock()
			for node := range successNodes.m {
				successesSlice = append(successesSlice, node)
			}
			successNodes.mux.Unlock()
			failNodes.mux.Lock()
			for node := range failNodes.m {
				failuresSlice = append(failuresSlice, node)
			}
			failNodes.mux.Unlock()

			log.WithField("service", sc.Service.Name()).Infof("Successful nodes: %s", strings.Join(successesSlice, ", "))
			log.WithField("service", sc.Service.Name()).Infof("Failed nodes: %s", strings.Join(failuresSlice, ", "))
		}(sc)
	}
	configWg.Wait() // Don't close the NotificationsChan or SuccessChan until we're done sending notifications and success statuses
}

// pushToNode copies a file from a specified source to a destination path, using the environment and account configured in the worker.Config object
func pushToNode(ctx context.Context, c *Config, sourceFile, node, destinationFile string, numRetries int, retrySleep time.Duration) error {
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

	for i := 0; i <= numRetries; i++ {
		funcLogger.Debugf("Try %d, %d retries left", i+1, numRetries-i)
		if ctx.Err() != nil {
			msg := "did not try to push file to destination node: context error"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg := fmt.Sprintf("%s: (timeout error)", msg)
				tracing.LogErrorWithTrace(span, funcLogger, msg)
				return fmt.Errorf("failed to push file to destination node: %w", ctx.Err())
			}
		}

		err := fileCopier.CopyToDestination(ctx, f)
		// Success
		if err == nil {
			break
		}
		// Unpingable node - no reason to retry
		if c.IsNodeUnpingable(node) {
			notificationErrorString := fmt.Sprintf("failed to push file to destination node: Node %s was not pingable earlier prior to attempt to push tokens; ", node)
			tracing.LogErrorWithTrace(span, funcLogger, notificationErrorString+"will not retry")
			return errors.New(notificationErrorString)
		}
		// Context has errored out - no reason to retry
		if ctx.Err() != nil {
			tracing.LogErrorWithTrace(span, funcLogger, fmt.Sprintf("failed to push file to destination node: %s: will not retry", ctx.Err()))
			return fmt.Errorf("failed to push file to destination node: %w", ctx.Err())
		}
		// Some other error - retry
		funcLogger.Error("failed to push file to destination node")
		if i == int(numRetries) {
			funcLogger.Debug("Max retries reached. Will not retry")
			return err
		}
		funcLogger.Debugf("Will retry. First sleeping for %s", retrySleep.String())
		time.Sleep(retrySleep)
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

type pushTokensConfig struct {
	sourcePath         string
	node               string
	account            string
	destinationPath    string
	env                environment.CommandEnvironment
	unpingable         bool
	fileCopierOptions  []string
	sshOptions         []string
	errorOnFail        bool
	numRetries         uint
	retrySleepDuration time.Duration
	cleanupFunc        func() error
}

func getPushTokensValuesFromConfig(c *Config) ([]pushTokensConfig, error) {
	if c == nil {
		return nil, errors.New("nil Config object passed to getPushTokensValuesFromConfig")
	}

	pushTokensConfigs := make([]pushTokensConfig, 0, 3*len(c.Nodes))

	sourceFilename, err := findFirstCreddVaultToken(c.ServiceCreddVaultTokenPathRoot, c.Service.Name(), c.Schedds)
	if err != nil {
		return nil, fmt.Errorf("could not find suitable vault token to push: %w", err)
	}

	destinationTokenFilenames := []string{
		fmt.Sprintf("/tmp/vt_u%d", c.DesiredUID),
		fmt.Sprintf("/tmp/vt_u%d-%s", c.DesiredUID, c.Service.Name()),
	}

	// Default role files
	var dontSendDefaultRoleFile bool
	defaultRoleFileDestinationFilename, err := parseDefaultRoleFileDestinationTemplateFromConfig(c)
	if err != nil {
		log.Error("Could not obtain default role file destination.  Will not push the default role file")
		dontSendDefaultRoleFile = true
	}
	defaultRoleFileName, err := prepareDefaultRoleFile(c)
	if err != nil {
		log.Error("Could not prepare default role file.  Will not push the default role file")
		dontSendDefaultRoleFile = true
	}
	sendDefaultRoleFile := !dontSendDefaultRoleFile

	// Retry values
	numRetries, err := getWorkerNumRetriesValueFromConfig(*c, PushTokensWorkerType)
	if err != nil {
		log.Debug("Could not get retry value from config.  Using default value")
		numRetries = 0
	}
	retrySleepDuration, err := getWorkerRetrySleepValueFromConfig(*c, PushTokensWorkerType)
	if err != nil {
		log.Debug("Could not get retry sleep value from config.  Using default value")
		retrySleepDuration = defaultRetrySleepDuration
	}

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

	for _, node := range c.Nodes {
		for _, destinationTokenFilename := range destinationTokenFilenames {
			// Vault tokens
			pushTokensConfigs = append(pushTokensConfigs, pushTokensConfig{
				sourcePath:         sourceFilename,
				node:               node,
				account:            c.Account,
				destinationPath:    destinationTokenFilename,
				env:                c.CommandEnvironment,
				unpingable:         c.IsNodeUnpingable(node),
				fileCopierOptions:  fileCopierOptions,
				sshOptions:         sshOptions,
				errorOnFail:        true,
				numRetries:         numRetries,
				retrySleepDuration: retrySleepDuration,
			})
		}
		// Default role file
		if sendDefaultRoleFile {
			pushTokensConfigs = append(pushTokensConfigs, pushTokensConfig{
				sourcePath:        defaultRoleFileName,
				node:              node,
				account:           c.Account,
				destinationPath:   defaultRoleFileDestinationFilename,
				env:               c.CommandEnvironment,
				unpingable:        c.IsNodeUnpingable(node),
				fileCopierOptions: fileCopierOptions,
				sshOptions:        sshOptions,
				errorOnFail:       false,
				cleanupFunc: func() error {
					if err := os.Remove(defaultRoleFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
						return errors.New("could not remove default role file")
					}
					return nil
				},
			})
		}
	}
	return pushTokensConfigs, nil
}
