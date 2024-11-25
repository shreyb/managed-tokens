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

package main

import (
	"context"
	"fmt"
	"reflect"
	"runtime"

	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/worker"
)

// startServiceConfigWorkerForProcessing starts up a worker using the provided workerFunc, gives it a set of channels to receive *worker.Configs
// and send notification.Notifications on, and sends *worker.Configs to the worker
func startServiceConfigWorkerForProcessing(ctx context.Context, workerFunc worker.Worker,
	serviceConfigs map[string]*worker.Config, timeoutCheckKey timeoutKey, disableNotifications bool) chansForWorkers {
	// Channels, context, and worker for getting kerberos tickets
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "startServiceConfigWorkerForProcessing")
	span.SetAttributes(
		attribute.KeyValue{
			Key:   "workerFunc",
			Value: attribute.StringValue(runtime.FuncForPC(reflect.ValueOf(workerFunc).Pointer()).Name()),
		},
	)
	defer span.End()

	var useCtx context.Context

	channels := worker.NewChannelsForWorkers(len(serviceConfigs))

	if !disableNotifications {
		startListenerOnWorkerNotificationChans(ctx, channels.GetNotificationsChan())
	}

	if timeout, ok := timeouts[timeoutCheckKey]; ok {
		useCtx = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		useCtx = ctx
	}
	go workerFunc(useCtx, channels)
	if len(serviceConfigs) > 0 { // We add this check because if there are no serviceConfigs, don't load them into any channel
		serviceConfigSlice := make([]*worker.Config, 0, len(serviceConfigs))
		for _, sc := range serviceConfigs {
			serviceConfigSlice = append(serviceConfigSlice, sc)
		}
		loadServiceConfigsIntoChannel(channels.GetServiceConfigChan(), serviceConfigSlice)
	}
	return channels
}

// loadServiceConfigsIntoChannel loads *worker.Config objects into a channel, usually for use by a worker, and then closes the channel
func loadServiceConfigsIntoChannel(chanToLoad chan<- *worker.Config, serviceConfigSlice []*worker.Config) {
	defer close(chanToLoad)
	for _, sc := range serviceConfigSlice {
		chanToLoad <- sc
	}
}

// removeFailedServiceConfigs reads the worker.SuccessReporter chan from the passed in worker.ChannelsForWorkers object, and
// removes any *worker.Config objects from the passed in serviceConfigs map.  It returns a slice of the *worker.Configs that
// were removed
func removeFailedServiceConfigs(chans chansForWorkers, serviceConfigs map[string]*worker.Config) []*worker.Config {
	failedConfigs := make([]*worker.Config, 0, len(serviceConfigs))
	for workerSuccess := range chans.GetSuccessChan() {
		if !workerSuccess.GetSuccess() {
			exeLogger.WithField(
				"service", getServiceName(workerSuccess.GetService()),
			).Debug("Removing serviceConfig from list of configs to use")
			failedConfigs = append(failedConfigs, serviceConfigs[getServiceName(workerSuccess.GetService())])
			delete(serviceConfigs, getServiceName(workerSuccess.GetService()))
		}
	}
	return failedConfigs
}

// addServiceToServicesSlice checks to see if, for an experiment and its entry in the configuration, a normal service.Service can be added
// to the services slice, or if an ExperimentOverriddenService should be added.  It then adds the resultant type that implements
// service.Service to the services slice
func addServiceToServicesSlice(services []service.Service, configExperiment, realExperiment, role string) []service.Service {
	var serv service.Service
	serviceName := realExperiment + "_" + role
	if configExperiment != realExperiment {
		serv = newExperimentOverriddenService(serviceName, configExperiment)
	} else {
		serv = service.NewService(serviceName)
	}
	services = append(services, serv)
	return services
}

// getDevEnvironment first checks the environment variable MANAGED_TOKENS_DEV_ENVIRONMENT for the devEnvironment, then the configuration file.
// If it finds neither are set, it returns the default global setting.  This logic is handled by the underlying logic in the
// viper library
func getDevEnvironmentLabel() string {
	// For devs, this variable can be set to differentiate between dev and prod for metrics, for example
	viper.SetDefault("devEnvironmentLabel", devEnvironmentLabelDefault)
	viper.BindEnv("devEnvironmentLabel", "MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL")
	return viper.GetString("devEnvironmentLabel")
}

// getPrometheusJobName gets the job name by parsing the configuration and the devEnvironment
func getPrometheusJobName() string {
	defaultJobName := "managed_tokens"
	jobName := viper.GetString("prometheus.jobname")
	if jobName == "" {
		jobName = defaultJobName
	}
	if devEnvironmentLabel == devEnvironmentLabelDefault {
		return jobName
	}
	return fmt.Sprintf("%s_%s", jobName, devEnvironmentLabel)
}

// chansForWorkers is an interface that defines methods for obtaining channels
// used by worker.Workers. It includes methods to get channels for service
// configurations and success reporting.
type chansForWorkers interface {
	GetServiceConfigChan() chan<- *worker.Config
	GetSuccessChan() <-chan worker.SuccessReporter
}

// resolveDisableNotifications checks each service's configuration to determine if notifications should be disabled.
// It takes a slice of service objects as input and returns a boolean indicating whether admin notifications should be disabled,
// and a slice of strings containing the names of services for which notifications should be disabled.
func resolveDisableNotifications(services []service.Service) (bool, []string) {
	serviceNotificationsToDisable := make([]string, 0, len(services))
	globalDisableNotifications := viper.GetBool("disableNotifications")
	finalDisableAdminNotifications := globalDisableNotifications

	// Check each service's override
	for _, s := range services {
		serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
		disableNotificationsPath, _ := getServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "disableNotifications")
		serviceDisableNotifications := viper.GetBool(disableNotificationsPath)

		// If global setting is to disable notifications (true), but any one of the experiments wants to have notifications sent (false),
		// we need to send admin notifications for that service too, so override the global setting
		if (!serviceDisableNotifications) && globalDisableNotifications {
			finalDisableAdminNotifications = false
		}

		// If the service wants to disable notifications, either through override or from the global setting, add it to the list
		if serviceDisableNotifications {
			serviceNotificationsToDisable = append(serviceNotificationsToDisable, getServiceName(s))
		}
	}

	return finalDisableAdminNotifications, serviceNotificationsToDisable
}
