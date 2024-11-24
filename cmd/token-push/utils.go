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
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/cmdUtils"
	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/worker"
)

// Prep admin notifications

// setupAdminNotifications prepares a notifications.AdminNotificationManager, and returns the following:
// 1. A pointer to the AdminNotificationsManager that was set up
// 2. A channel that the caller will send its notifications to for the AdminNotificationManager to process.
// 3. A slice of notifications.SendMessagers that will be populated by the errors the AdminNotificationManager collects
func setupAdminNotifications(ctx context.Context, database *db.ManagedTokensDatabase) (*notifications.AdminNotificationManager, chan<- notifications.SourceNotification, []notifications.SendMessager) {
	var adminNotifications []notifications.SendMessager

	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "setupAdminNotifications")
	defer span.End()

	var prefix string
	if viper.GetBool("test") {
		prefix = "notifications_test."
	} else {
		prefix = "notifications."
	}

	now := time.Now().Format(time.RFC822)
	email := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice(prefix+"admin_email"),
		"Managed Tokens Errors "+now,
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
	)
	slackMessage := notifications.NewSlackMessage(viper.GetString(prefix + "slack_alerts_url"))
	adminNotifications = append(adminNotifications, email, slackMessage)

	// Functional options for AdminNotificationManager
	funcOpts := make([]notifications.AdminNotificationManagerOption, 0)
	setNotificationMinimum := func(a *notifications.AdminNotificationManager) error {
		exeLogger.Debug("Setting AdminNotificationManager NotificationMinimum")
		a.NotificationMinimum = viper.GetInt("errorCountToSendMessage")
		return nil
	}
	funcOpts = append(funcOpts, setNotificationMinimum)

	if database != nil {
		setDB := func(a *notifications.AdminNotificationManager) error {
			exeLogger.Debug("Setting AdminNotificationManager Database")
			a.Database = database
			return nil
		}
		funcOpts = append(funcOpts, setDB)
	}

	a := notifications.NewAdminNotificationManager(ctx, funcOpts...)
	c := a.RegisterNotificationSource(ctx)
	return a, c, adminNotifications
}

func sendAdminNotifications(ctx context.Context, a *notifications.AdminNotificationManager, adminNotificationsPtr *[]notifications.SendMessager) error {
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "sendAdminNotifications")
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"executable": currentExecutable,
		"func":       "sendAdminNotifications",
	})

	// Make sure that all of our Service Email Managers have finished sending their notifications
	handleNotificationsFinalization()
	a.RequestToCloseReceiveChan(ctx)

	if err := notifications.SendAdminNotifications(
		ctx,
		currentExecutable,
		viper.GetBool("test"),
		(*adminNotificationsPtr)...,
	); err != nil {
		msg := "Error sending admin notifications"
		span.RecordError(err)
		span.SetStatus(codes.Error, msg)
		funcLogger.Error(msg)
		return err
	}
	span.SetStatus(codes.Ok, "Admin notifications sent successfully")
	return nil
}

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

// ResolveDisableNotifications checks each service's configuration to determine if notifications should be disabled.
// It takes a slice of service objects as input and returns a boolean indicating whether admin notifications should be disabled,
// and a slice of strings containing the names of services for which notifications should be disabled.
func ResolveDisableNotifications(services []service.Service) (bool, []string) {
	serviceNotificationsToDisable := make([]string, 0, len(services))
	globalDisableNotifications := viper.GetBool("disableNotifications")
	finalDisableAdminNotifications := globalDisableNotifications

	// Check each service's override
	for _, s := range services {
		serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
		disableNotificationsPath, _ := cmdUtils.GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "disableNotifications")
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
