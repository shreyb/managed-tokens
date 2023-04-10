package main

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/worker"
)

var once sync.Once

// startServiceConfigWorkerForProcessing starts up a worker using the provided workerFunc, gives it a set of channels to receive *worker.Configs
// and send notification.Notifications on, and sends *worker.Configs to the worker
func startServiceConfigWorkerForProcessing(ctx context.Context, workerFunc func(context.Context, worker.ChannelsForWorkers),
	serviceConfigs map[string]*worker.Config, timeoutCheckKey string) worker.ChannelsForWorkers {
	// Channels, context, and worker for getting kerberos tickets
	var useCtx context.Context
	channels := worker.NewChannelsForWorkers(len(serviceConfigs))
	startListenerOnWorkerNotificationChans(ctx, channels.GetNotificationsChan())
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
	for _, sc := range serviceConfigs {
		chanToLoad <- sc
	}
}

// removeFailedServiceConfigs reads the worker.SuccessReporter chan from the passed in worker.ChannelsForWorkers object, and
// removes any *worker.Config objects from the passed in serviceConfigs map.  It returns a slice of the *worker.Configs that
// were removed
func removeFailedServiceConfigs(chans worker.ChannelsForWorkers, serviceConfigs map[string]*worker.Config) []*worker.Config {
	failedConfigs := make([]*worker.Config, 0, len(serviceConfigs))
	for workerSuccess := range chans.GetSuccessChan() {
		if !workerSuccess.GetSuccess() {
			log.WithField(
				"service", getServiceName(workerSuccess.GetService()),
			).Debug("Removing serviceConfig from list of configs to use")
			failedConfigs = append(failedConfigs, serviceConfigs[getServiceName(workerSuccess.GetService())])
			delete(serviceConfigs, getServiceName(workerSuccess.GetService()))
		}
	}
	return failedConfigs
}

// checkExperimentOverride checks the configuration for a given experiment to see if it has an "experimentOverride" key defined.
// If it does, it will return that override value.  Else, it will return the passed in experiment string
func checkExperimentOverride(experiment string) string {
	if override := viper.GetString("experiments." + experiment + ".experimentOverride"); override != "" {
		return override
	}
	return experiment
}

// addServiceToServicesSlice checks to see if, for an experiment and its entry in the configuration, a normal service.Service can be added
// to the services slice, or if an ExperimentOverriddenService should be added.  It then adds the resultant type that implements
// service.Service to the services slice
func addServiceToServicesSlice(services []service.Service, configExperiment, realExperiment, role string) []service.Service {
	var serv service.Service
	serviceName := realExperiment + "_" + role
	if configExperiment != realExperiment {
		serv = newExperimentOverridenService(serviceName, configExperiment)
	} else {
		serv = service.NewService(serviceName)
	}
	services = append(services, serv)
	return services
}
