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
	"reflect"
	"runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/worker"
)

// chansForWorkers is an interface that defines methods for obtaining channels
// used by worker.Workers. It includes methods to get channels for service
// configurations and success reporting.
type chansForWorkers interface {
	GetServiceConfigChan() chan<- *worker.Config
	GetSuccessChan() <-chan worker.SuccessReporter
}

// startServiceConfigWorkerForProcessing starts up a worker using the provided workerFunc, gives it a set of channels to receive *worker.Configs
// and send notification.Notifications on, and sends *worker.Configs to the worker
func startServiceConfigWorkerForProcessing(ctx context.Context, workerFunc worker.Worker,
	serviceConfigs map[string]*worker.Config, timeoutCheckKey timeoutKey) chansForWorkers {
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "startServiceConfigWorkerForProcessing")
	span.SetAttributes(
		attribute.KeyValue{
			Key:   "workerFunc",
			Value: attribute.StringValue(runtime.FuncForPC(reflect.ValueOf(workerFunc).Pointer()).Name()),
		},
	)
	defer span.End()

	channels := worker.NewChannelsForWorkers(len(serviceConfigs))

	startListenerOnWorkerNotificationChans(ctx, channels.GetNotificationsChan())

	var useCtx context.Context
	if timeout, ok := timeouts[timeoutCheckKey]; ok {
		useCtx = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		useCtx = ctx
	}

	// Start the work!
	go workerFunc(useCtx, channels)

	// Send our serviceConfigs to the worker
	for _, sc := range serviceConfigs {
		channels.GetServiceConfigChan() <- sc
	}
	close(channels.GetServiceConfigChan())

	return channels
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
