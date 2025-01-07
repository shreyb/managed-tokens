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
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/yukitsune/lokirus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/kerberos"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/vaultToken"
	"github.com/fermitools/managed-tokens/internal/worker"
)

const devEnvironmentLabelDefault string = "production"

var (
	currentExecutable       string
	buildTimestamp          string // Should be injected at build time with something like go build -ldflags="-X main.buildTimeStamp=$BUILDTIMESTAMP"
	version                 string // Should be injected at build time with something like go build -ldflags="-X main.version=$VERSION"
	exeLogger               *log.Entry
	notificationsDisabledBy disableNotificationsOption = DISABLED_BY_CONFIGURATION
	debugEnvSetByFlag       bool                       // Set to true if we set MANAGED_TOKENS_DEBUG via flag in resolveDebugAndVerbose()
)

// devEnvironmentLabel can be set via config or environment variable MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL
var devEnvironmentLabel string

// Supported timeouts and their default values
var timeouts = map[timeoutKey]time.Duration{
	timeoutGlobal:      time.Duration(300 * time.Second),
	timeoutKerberos:    time.Duration(20 * time.Second),
	timeoutVaultStorer: time.Duration(60 * time.Second),
	timeoutPing:        time.Duration(10 * time.Second),
	timeoutPush:        time.Duration(30 * time.Second),
}

// Metrics-related variables
var (
	startSetup   time.Time
	startCleanup time.Time
	prometheusUp = true
)

// Metrics
var (
	promDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "stage_duration_seconds",
		Help:      "The amount of time it took to run a stage Managed Tokens Service executable",
	},
		[]string{
			"executable",
			"stage",
		},
	)
	servicePushFailureCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "failed_services_push_count",
		Help:      "The number of services for which pushing tokens failed in the last round",
	})
)

var (
	services []service.Service
)

var errExitOK = errors.New("exit 0")

func main() {
	if err := setup(); err != nil {
		if errors.Is(err, errExitOK) {
			os.Exit(0)
		}
		log.Fatal("Error running setup actions.  Exiting")
	}

	// Global context
	var globalTimeout time.Duration
	var ok bool
	if globalTimeout, ok = timeouts[timeoutGlobal]; !ok {
		exeLogger.Fatal("Could not obtain global timeout.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Tracing has to be initialized here and not in setup because we need our global context to pass to child spans
	if tracingShutdown, err := initTracing(ctx); err == nil {
		defer tracingShutdown(ctx)
	}

	// Run our actual operation
	if err := run(ctx); err != nil {
		exeLogger.Fatal("Error running operations to push vault tokens.  Exiting")
	}
	exeLogger.Debug("Finished run")

	// If we changed the debug environment variable, put it back
	if debugEnvSetByFlag {
		os.Unsetenv("MANAGED_TOKENS_DEBUG")
	}
}

// Initial setup.  Read flags, find config file
func setup() error {
	startSetup = time.Now()

	// Configuration defaults that are not flag/config file specific
	viper.SetDefault("disableNotifications", false)

	// Get current executable name
	if exePath, err := os.Executable(); err != nil {
		log.Error("Could not get path of current executable")
	} else {
		currentExecutable = path.Base(exePath)
	}
	setupLogger := log.WithField("executable", currentExecutable)

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		setupLogger.Error("Current user is root.  Please run this executable as a non-root user")
		return err
	}

	initFlags()

	versionMessage := fmt.Sprintf("Managed tokens libary version %s, build %s\n", version, buildTimestamp)
	if viper.GetBool("version") {
		fmt.Println(versionMessage)
		return errExitOK
	}
	setupLogger.Info(versionMessage)

	if err := initConfig(); err != nil {
		fmt.Println("Fatal error setting up configuration.  Exiting now")
		return err
	}

	initEnvironment()

	// TODO Remove this after bug detailed in initFlags() is fixed upstream
	disableNotifyFlagWorkaround()
	// END TODO

	// If user wants to list all services, do that and exit
	if viper.GetBool("list-services") {
		allServices := make([]string, 0)
		for experiment := range viper.GetStringMap("experiments") {
			roleMap := viper.GetStringMap("experiments." + experiment + ".roles")
			for role := range roleMap {
				allServices = append(allServices, fmt.Sprintf("%s_%s", experiment, role))
			}
		}
		fmt.Println(strings.Join(allServices, "\n"))
		return errExitOK
	}

	if err := checkRunOnboardingFlags(); err != nil {
		setupLogger.Error(err)
		return err
	}

	devEnvironmentLabel = getDevEnvironmentLabel()

	initLogs()

	if viper.GetBool("run-onboarding") {
		setupLogger.Infof("Running onboarding for service %s", viper.GetString("service"))
		setupLogger.Info("Will disable notifications because run-onboarding flag is set")
		viper.Set("disableNotifications", true)
		notificationsDisabledBy = DISABLED_BY_FLAG
	}

	initServices()

	if err := initTimeouts(); err != nil {
		setupLogger.Error("Fatal error setting up timeouts")
		return err
	}
	if err := initMetrics(); err != nil {
		setupLogger.Error("Error setting up metrics. Will still continue")
	}
	return nil
}

func run(ctx context.Context) error {
	// Order of operations:
	// 0. Setup (admin notifications, kerberos cache dir, generate worker.Configs, set up notification listeners)
	// 1. Get kerberos tickets
	// 2. Get and store vault tokens
	// 3. Ping nodes to check their status
	// 4. Push vault tokens to nodes
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "token-push")
	if viper.GetBool("test") {
		span.SetAttributes(attribute.KeyValue{Key: "test", Value: attribute.BoolValue(true)})
	}
	defer span.End()

	successfulServices := make(map[string]bool) // Initialize Map of services for which all steps were successful

	database, databaseErr := openDatabaseAndLoadServices(ctx)

	// Determine what notifications should be sent
	var blockAdminNotifications bool
	blockServiceNotificationsSlice := make([]string, 0, len(services))

	// If we have disabled the notifications via a flag, we need to always block the notifications irrespective of the configuration
	if notificationsDisabledBy == DISABLED_BY_FLAG {
		blockAdminNotifications = true
		for _, s := range services {
			blockServiceNotificationsSlice = append(blockServiceNotificationsSlice, getServiceName(s))
		}
	} else {
		// Otherwise, look at the configuration to see if we should block notifications
		blockAdminNotifications, blockServiceNotificationsSlice = resolveDisableNotifications(services)
	}

	noServiceNotifications := make(map[string]struct{})
	for _, s := range blockServiceNotificationsSlice {
		noServiceNotifications[s] = struct{}{}
	}
	if blockAdminNotifications {
		exeLogger.Debugf("Admin notifications disabled by %s", notificationsDisabledBy.String())
	}
	if len(blockServiceNotificationsSlice) > 0 {
		exeLogger.WithField(
			"services", strings.Join(blockServiceNotificationsSlice, ", ")).Debug(
			"No service notifications will be sent for these services")
	}

	// Send admin notifications at end of run.  Note that if databaseErr != nil, then database = nil.
	var admNotMgr *notifications.AdminNotificationManager
	var aReceiveChan chan<- notifications.SourceNotification
	var adminNotifications []notifications.SendMessager
	if !blockAdminNotifications {
		admNotMgr, aReceiveChan, adminNotifications = setupAdminNotifications(ctx, database)
		if databaseErr != nil {
			msg := "Could not open or create ManagedTokensDatabase"
			span.SetStatus(codes.Error, msg)
			aReceiveChan <- notifications.SourceNotification{
				Notification: notifications.NewSetupError(msg, currentExecutable),
			}
		} else {
			defer database.Close()
		}
	}

	// All the cleanup actions that should run any time run() returns
	defer func() {
		// Run cleanup actions
		// Cleanup
		if err := reportSuccessesAndFailures(successfulServices); err != nil {
			// We don't return the error here, because we don't want to halt execution if the admin message can't be sent.  Just log it and move on
			logErrorWithTracing(exeLogger, span, fmt.Errorf("error aggregating successes and failures: %w", err))
		}
		// Push metrics to prometheus pushgateway
		if prometheusUp {
			if err := metrics.PushToPrometheus(viper.GetString("prometheus.host"), getPrometheusJobName()); err != nil {
				exeLogger.Error("Could not push metrics to prometheus pushgateway")
			} else {
				exeLogger.Info("Finished pushing metrics to prometheus pushgateway")
			}
		}
		if blockAdminNotifications {
			exeLogger.Debugf("Admin notifications disabled by %s. Not sending admin notifications", notificationsDisabledBy.String())
		} else {
			close(aReceiveChan)
			// We don't check the error here, because we don't want to halt execution if the admin message can't be sent.  Just log it and move on
			sendAdminNotifications(ctx, admNotMgr, &adminNotifications)
		}
	}()

	// Create temporary dir for all kerberos caches to live in
	var kerbCacheDir string
	kerbCacheDir, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		exeLogger.Error("Cannot create temporary dir for kerberos cache. Will just use os.TempDir")
		kerbCacheDir = os.TempDir()
	} else {
		defer func() {
			// Clear kerberos cache dir
			if err := os.RemoveAll(kerbCacheDir); err != nil {
				exeLogger.WithField("kerbCacheDir", kerbCacheDir).Error("Could not clear kerberos cache directory.  Please clean up manually")
				return
			}
			exeLogger.WithField("kerbCacheDir", kerbCacheDir).Info("Cleared kerberos cache directory")
		}()
	}

	// Concurrently collect service configs, and after collection is done, signal to run() that it
	// can proceed with processing
	serviceConfigs := make(map[string]*worker.Config) // Running map of service configs to pass to workers
	// Since our worker configs are getting set up concurrently, protect our service maps with these mutexes
	var _serviceConfigsMux sync.Mutex
	var _successfulServicesMux sync.Mutex

	// Set up our serviceConfigs and load them into various collection channels
	// Execution of this program is blocked until the serviceConfigSetupWg waitgroup reaches zero.
	var serviceConfigSetupWg sync.WaitGroup
	for _, s := range services {
		serviceConfigSetupWg.Add(1)
		go func(s service.Service) {
			funcLogger := exeLogger.WithFields(log.Fields{
				"caller":  "token-push.run",
				"service": getServiceName(s),
			})

			// Setup the configs
			defer serviceConfigSetupWg.Done()

			ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "serviceConfigSetup_anonFunc")
			span.SetAttributes(attribute.KeyValue{Key: "service", Value: attribute.StringValue(s.Name())})
			defer span.End()

			// Add every service to our successfulServices map.  The value will be set to true later if we
			// successfully complete all the operations for that service
			_successfulServicesMux.Lock()
			successfulServices[getServiceName(s)] = false
			_successfulServicesMux.Unlock()

			// Create kerberos cache for this service
			krb5ccCache, err := os.CreateTemp(kerbCacheDir, fmt.Sprintf("managed-tokens-krb5ccCache-%s", s.Name()))
			if err != nil {
				logErrorWithTracing(funcLogger, span, fmt.Errorf("cannot create kerberos cache. Subsequent operations will fail. Skipping service: %w", err))
				return
			}

			// All required service-level configuration items
			serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
			uid, err := getDesiredUIDByOverrideOrLookup(ctx, serviceConfigPath, database)
			if err != nil {
				logErrorWithTracing(funcLogger, span, fmt.Errorf("error obtaining UID for service. Skipping service: %w", err))
				return
			}
			userPrincipal, htgettokenopts := getUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath)
			if userPrincipal == "" {
				logErrorWithTracing(funcLogger, span, errors.New("userPrincipal cannot be blank. Skipping service"))
				return
			}
			vaultServer, err := getVaultServer(serviceConfigPath)
			if err != nil {
				logErrorWithTracing(funcLogger, span, fmt.Errorf("error obtaining vault server for service. Cannot proceed. Returning now: %w", err))
				return
			}
			collectorHost, schedds, err := getScheddsAndCollectorHostFromConfiguration(ctx, serviceConfigPath)
			if err != nil {
				logErrorWithTracing(funcLogger, span, fmt.Errorf("error obtaining collector host and schedds for service. Cannot proceed. Returning now: %w", err))
				return
			}

			// Service-level configuration items that can be defined either in configuration file or on system/environment or have library defaults
			keytabPath := getKeytabFromConfiguration(serviceConfigPath)
			defaultRoleFileDestinationTemplate := getDefaultRoleFileDestinationTemplate(serviceConfigPath)
			serviceCreddVaultTokenPathRoot := getServiceCreddVaultTokenPathRoot(serviceConfigPath)
			vaultTokenStoreHoldoffFunc := getVaultTokenStoreHoldoffFuncOpt(s)
			fileCopierOptions := getFileCopierOptionsFromConfig(serviceConfigPath)
			extraPingOpts := getPingOptsFromConfig(serviceConfigPath)
			sshOpts := getSSHOptsFromConfig(serviceConfigPath)

			// Worker-specific config to be passed to the worker.Config constructor
			getKerberosTicketsRetries := getWorkerConfigInt("getKerberosTickets", "numRetries")
			getKerberosTicketsRetrySleep := getWorkerConfigTimeDuration("getKerberosTickets", "retrySleep")
			storeAndGetTokenRetries := getWorkerConfigInt("storeAndGetToken", "numRetries")
			storeAndGetTokenRetrySleep := getWorkerConfigTimeDuration("storeAndGetToken", "retrySleep")
			storeAndGetTokenInteractiveRetries := getWorkerConfigInt("storeAndGetTokenInteractive", "numRetries")
			storeAndGetTokenInteractiveRetrySleep := getWorkerConfigTimeDuration("storeAndGetTokenInteractive", "retrySleep")
			pingAggregatorRetries := getWorkerConfigInt("pingAggregator", "numRetries")
			pingAggregatorRetrySleep := getWorkerConfigTimeDuration("pingAggregator", "retrySleep")
			pushTokensRetries := getWorkerConfigInt("pushTokens", "numRetries")
			pushTokensRetrySleep := getWorkerConfigTimeDuration("pushTokens", "retrySleep")

			c, err := worker.NewConfig(
				s,
				worker.SetCommandEnvironment(
					func(e *environment.CommandEnvironment) { e.SetKrb5ccname(krb5ccCache.Name(), environment.FILE) },
					func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collectorHost) },
					func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) },
				),
				worker.SetSchedds(schedds),
				worker.SetVaultServer(vaultServer),
				worker.SetServiceCreddVaultTokenPathRoot(serviceCreddVaultTokenPathRoot),
				worker.SetUserPrincipal(userPrincipal),
				worker.SetKeytabPath(keytabPath),
				worker.SetDesiredUID(uid),
				worker.SetNodes(viper.GetStringSlice(serviceConfigPath+".destinationNodes")),
				worker.SetAccount(viper.GetString(serviceConfigPath+".account")),

				worker.SetWorkerNumRetriesValue(worker.GetKerberosTicketsWorkerType, uint(getKerberosTicketsRetries)),
				worker.SetWorkerRetrySleepValue(worker.GetKerberosTicketsWorkerType, getKerberosTicketsRetrySleep),

				worker.SetWorkerNumRetriesValue(worker.StoreAndGetTokenWorkerType, uint(storeAndGetTokenRetries)),
				worker.SetWorkerRetrySleepValue(worker.StoreAndGetTokenWorkerType, storeAndGetTokenRetrySleep),

				worker.SetWorkerNumRetriesValue(worker.StoreAndGetTokenInteractiveWorkerType, uint(storeAndGetTokenInteractiveRetries)),
				worker.SetWorkerRetrySleepValue(worker.StoreAndGetTokenInteractiveWorkerType, storeAndGetTokenInteractiveRetrySleep),

				worker.SetWorkerNumRetriesValue(worker.PingAggregatorWorkerType, uint(pingAggregatorRetries)),
				worker.SetWorkerRetrySleepValue(worker.PingAggregatorWorkerType, pingAggregatorRetrySleep),

				worker.SetWorkerNumRetriesValue(worker.PushTokensWorkerType, uint(pushTokensRetries)),
				worker.SetWorkerRetrySleepValue(worker.PushTokensWorkerType, pushTokensRetrySleep),

				worker.SetSupportedExtrasKeyValue(worker.DefaultRoleFileDestinationTemplate, defaultRoleFileDestinationTemplate),
				worker.SetSupportedExtrasKeyValue(worker.FileCopierOptions, fileCopierOptions),
				worker.SetSupportedExtrasKeyValue(worker.PingOptions, extraPingOpts),
				worker.SetSupportedExtrasKeyValue(worker.SSHOptions, sshOpts),
				vaultTokenStoreHoldoffFunc,
			)
			if err != nil {
				logErrorWithTracing(funcLogger, span, fmt.Errorf("could not create config for service: %w", err))
				return
			}

			_serviceConfigsMux.Lock()
			serviceConfigs[getServiceName(s)] = c
			_serviceConfigsMux.Unlock()

			// If notifications are not disabled for this service, register the service for notifications
			if _, ok := noServiceNotifications[getServiceName(s)]; !ok && (admNotMgr != nil) {
				registerServiceNotificationsChan(ctx, s, admNotMgr)
			} else {
				// If notifications are disabled for this service, register a dummy channel so that notifications get thrown away
				registerDummyServiceNotificationsChan(ctx, s)
			}

			span.SetStatus(codes.Ok, "Service config setup")
		}(s)
	}
	serviceConfigSetupWg.Wait()

	if len(serviceConfigs) == 0 {
		err := errors.New("no serviceConfigs to operate on")
		logErrorWithTracing(exeLogger, span, err)
		return err
	}

	span.AddEvent("Service configs setup complete")

	// Add our configured nodes to managed tokens database
	nodesToAddToDatabase := make([]string, 0)
	for _, serviceConfig := range serviceConfigs {
		nodesToAddToDatabase = append(nodesToAddToDatabase, serviceConfig.Nodes...)
	}
	if err := database.UpdateNodes(ctx, nodesToAddToDatabase); err != nil {
		exeLogger.Error("Could not update database with currently-configured nodes.  Future database-based operations may fail")
	}

	// Setup done.  Push prometheus metrics
	msg := "Setup complete"
	span.AddEvent(msg)
	exeLogger.Debug(msg)
	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "setup").Set(time.Since(startSetup).Seconds())
	}

	// Begin Processing

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = contextStore.WithVerbose(ctx)
	}

	// 1. Get kerberos tickets
	// Get channels and start worker for getting kerberos ticekts
	startKerberos := time.Now()
	span.AddEvent("Starting get kerberos tickets")
	kerberosChannels := startServiceConfigWorkerForProcessing(ctx, worker.GetKerberosTicketsWorker, serviceConfigs, timeoutKerberos)

	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	failedKerberosConfigs := removeFailedServiceConfigs(kerberosChannels, serviceConfigs)
	for _, failure := range failedKerberosConfigs {
		exeLogger.WithField("service", failure.Service.Name()).Error("Failed to obtain kerberos ticket.  Will not try to obtain or push vault token to service nodes")
	}
	if len(serviceConfigs) == 0 {
		exeLogger.Info("No more serviceConfigs to operate on.  Cleaning up now")
		return nil
	}
	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "getKerberosTickets").Set(time.Since(startKerberos).Seconds())
	}
	span.AddEvent("End get kerberos tickets")

	// 2. Get and store vault tokens
	// Get channels and start worker for getting and storing short-lived vault token (condor_vault_storer)
	startCondorVault := time.Now()
	span.AddEvent("Start obtain and store vault tokens")

	var w worker.Worker
	w = worker.StoreAndGetTokenWorker
	if viper.GetBool("run-onboarding") {
		w = worker.StoreAndGetTokenInteractiveWorker
	}
	condorVaultChans := startServiceConfigWorkerForProcessing(ctx, w, serviceConfigs, timeoutVaultStorer)

	// Wait until all workers are done, remove any service configs that we couldn't get tokens for from Configs,
	// and then begin transferring to nodes
	failedVaultConfigs := removeFailedServiceConfigs(condorVaultChans, serviceConfigs)
	for _, failure := range failedVaultConfigs {
		exeLogger.WithField("service", failure.Service.Name()).Error("Failed to obtain vault token.  Will not try to push vault token to service nodes")
	}

	// For any successful services, make sure we remove all the vault tokens when we're done
	for serviceName := range serviceConfigs {
		defer func(serviceName string) {
			if err := vaultToken.RemoveServiceVaultTokens(serviceName); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					exeLogger.WithField("service", serviceName).Debug("No vault tokens to remove")
					return
				}
				exeLogger.WithField("service", serviceName).Errorf("Could not remove vault tokens for service: %s", err)
			}
		}(serviceName)
	}

	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "storeAndGetTokens").Set(time.Since(startCondorVault).Seconds())
	}
	span.AddEvent("End obtain and store vault tokens")

	// If we're in test mode, stop here
	if viper.GetBool("test") {
		exeLogger.Info("Test mode.  Cleaning up now")
		for service := range serviceConfigs {
			successfulServices[service] = true
		}
		return nil
	}

	// If we're onboarding a service but not pushing their tokens, stop here
	if viper.GetBool("run-onboarding") && !viper.GetBool("push-tokens") {
		exeLogger.Info("Onboarding mode.  Cleaning up now")
		for service := range serviceConfigs {
			successfulServices[service] = true
		}
		return nil
	}

	if len(serviceConfigs) == 0 {
		exeLogger.Info("No more serviceConfigs to operate on.  Cleaning up now")
		return nil
	}

	// 3. Ping nodes to check their status
	// Get channels and start worker for pinging service nodes
	startPing := time.Now()
	span.AddEvent("Start ping nodes")
	pingChans := startServiceConfigWorkerForProcessing(ctx, worker.PingAggregatorWorker, serviceConfigs, timeoutPing)

	for pingSuccess := range pingChans.GetSuccessChan() {
		if !pingSuccess.GetSuccess() {
			msg := "Could not ping all nodes for service.  We'll still try to push tokens to all configured nodes, but there may be failures.  See logs for details"
			exeLogger.WithField("service", getServiceName(pingSuccess.GetService())).Error(msg)
		}
	}

	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "pingNodes").Set(time.Since(startPing).Seconds())
	}
	span.AddEvent("End ping nodes")

	// 4. Push vault tokens to nodes
	// Get channels and start worker for pushing tokens to service nodes
	startPush := time.Now()
	span.AddEvent("Start push tokens")
	pushChans := startServiceConfigWorkerForProcessing(ctx, worker.PushTokensWorker, serviceConfigs, timeoutPush)

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[getServiceName(pushSuccess.GetService())] = true
		}
	}

	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "pushTokens").Set(time.Since(startPush).Seconds())
	}
	span.AddEvent("End push tokens")

	return nil
}

// Setup helper functions

func initFlags() {
	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Flags
	pflag.String("admin", "", "Override the config file admin email")
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.Bool("disable-notifications", false, "Turn off all notifications for this run")
	pflag.Bool("dont-notify", false, "Same as --disable-notifications")
	pflag.StringP("experiment", "e", "", "Name of single experiment to push tokens")
	pflag.Bool("list-services", false, "List all configured services in config file")
	pflag.BoolP("push-tokens", "p", false, "Push tokens to nodes after onboarding a service. If -r/--run-onboarding is set, this flag must be set to push tokens.  Otherwise, it is ignored")
	pflag.BoolP("run-onboarding", "r", false, "Run onboarding for a given service.  Must be used with -s/--service, optionally can be used with -p/--push-tokens")
	pflag.StringP("service", "s", "", "Service to obtain and push vault tokens for.  Must be of the form experiment_role, e.g. dune_production")
	pflag.BoolP("test", "t", false, "Test mode.  Obtain vault tokens but don't push them to nodes")
	pflag.BoolP("verbose", "v", false, "Turn on verbose mode")
	pflag.BoolP("debug", "d", false, "Turn on debug mode.  This implies -v/--verbose.")
	pflag.Bool("version", false, "Version of Managed Tokens library")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	resolveDebugAndVerboseFromFlags()

	// Aliases
	// TODO There's a possible bug in viper, where pflags don't get affected by registering aliases.  The following should work, at least for one alias:
	//  viper.RegisterAlias("dont-notify", "disableNotifications")
	//  viper.RegisterAlias("disable-notifications", "disableNotifications")
	// Instead, we have to work around this after we read in the config file (see setup())
}

func initConfig() error {
	// Get config file
	configFileName := "managedTokens"
	// Check for override
	if config := viper.GetString("configfile"); config != "" {
		viper.SetConfigFile(config)
	} else {
		viper.SetConfigName(configFileName)
	}

	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		log.WithField("executable", currentExecutable).Errorf("Error reading in config file: %v", err)
		return err
	}
	return nil
}

// NOTE See initFlags().  This workaround will be removed when the possible viper bug referred to there is fixed.
func disableNotifyFlagWorkaround() {
	if viper.GetBool("disable-notifications") || viper.GetBool("dont-notify") {
		viper.Set("disableNotifications", true)
		notificationsDisabledBy = DISABLED_BY_FLAG
	}
}

func checkRunOnboardingFlags() error {
	if viper.GetBool("run-onboarding") && viper.GetString("service") == "" {
		return errors.New("run-onboarding flag set without a service for which to run onboarding")
	}
	return nil
}

// Environment variables to read into Viper config
// Note: In keeping with best practices, these can be overridden by command line flags or direct
// in-code overrides.  This only sets the initial state of the given viper keys.
func initEnvironment() {
	viper.BindEnv("collectorHost", environment.CondorCollectorHost.EnvVarKey())
	viper.BindEnv("debug", "MANAGED_TOKENS_DEBUG")
}

// Set up logs
func initLogs() {
	if viper.GetBool("debug") {
		log.SetLevel(log.DebugLevel)
		debugLogConfigLookup := "logs.token-push.debugfile"

		// Debug log
		log.AddHook(lfshook.NewHook(lfshook.PathMap{
			log.DebugLevel: viper.GetString(debugLogConfigLookup),
			log.InfoLevel:  viper.GetString(debugLogConfigLookup),
			log.WarnLevel:  viper.GetString(debugLogConfigLookup),
			log.ErrorLevel: viper.GetString(debugLogConfigLookup),
			log.FatalLevel: viper.GetString(debugLogConfigLookup),
			log.PanicLevel: viper.GetString(debugLogConfigLookup),
		}, &log.TextFormatter{FullTimestamp: true}))

		// Set package-level debug loggers
		db.SetDebugLogger(log.StandardLogger())
		kerberos.SetDebugLogger(log.StandardLogger())

	}

	logConfigLookup := "logs.token-push.logfile"
	// Info log file
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.InfoLevel:  viper.GetString(logConfigLookup),
		log.WarnLevel:  viper.GetString(logConfigLookup),
		log.ErrorLevel: viper.GetString(logConfigLookup),
		log.FatalLevel: viper.GetString(logConfigLookup),
		log.PanicLevel: viper.GetString(logConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	// Loki.  Example here taken from README: https://github.com/YuKitsune/lokirus/blob/main/README.md
	lokiOpts := lokirus.NewLokiHookOptions().
		// Grafana doesn't have a "panic" level, but it does have a "critical" level
		// https://grafana.com/docs/grafana/latest/explore/logs-integration/
		WithLevelMap(lokirus.LevelMap{log.PanicLevel: "critical"}).
		WithFormatter(&log.JSONFormatter{}).
		WithStaticLabels(lokirus.Labels{
			"app":         "managed-tokens",
			"command":     currentExecutable,
			"environment": devEnvironmentLabel,
		})
	lokiHook := lokirus.NewLokiHookWithOpts(
		viper.GetString("loki.host"),
		lokiOpts,
		log.InfoLevel,
		log.WarnLevel,
		log.ErrorLevel,
		log.FatalLevel)

	log.AddHook(lokiHook)

	exeLogger = log.WithField("executable", currentExecutable)
	exeLogger.Debugf("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		exeLogger.Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func initTimeouts() error {
	// Save supported timeouts into timeouts map
	for key, timeoutString := range viper.GetStringMapString("timeouts") {
		tKey := strings.TrimSuffix(key, "timeout")
		// Only save the timeout if it's supported, otherwise ignore it
		if validKey, ok := getTimeoutKeyFromString(tKey); ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				exeLogger.WithField("timeoutKey", validKey.String()).Warn("Could not parse configured timeout.  Using default")
				continue
			}
			timeouts[validKey] = timeout
			exeLogger.WithField(validKey.String(), timeoutString).Debug("Configured timeout")
		}
	}

	// Verify that individual timeouts don't add to more than total timeout
	now := time.Now()
	timeForComponentCheck := now

	for timeoutKey, timeout := range timeouts {
		if timeoutKey != timeoutGlobal {
			timeForComponentCheck = timeForComponentCheck.Add(timeout)
		}
	}

	timeForGlobalCheck := now.Add(timeouts[timeoutGlobal])
	if timeForComponentCheck.After(timeForGlobalCheck) {
		msg := "configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts"
		exeLogger.Error(msg)
		return errors.New(msg)
	}
	return nil
}

// Set up prometheus metrics
func initMetrics() error {
	// Set up prometheus metrics
	if _, err := http.Get(viper.GetString("prometheus.host")); err != nil {
		exeLogger.Errorf("Error contacting prometheus pushgateway %s: %s.  The rest of prometheus operations will fail. "+
			"To limit error noise, "+
			"these failures at the experiment level will be registered as warnings in the log, "+
			"and not be sent in any notifications.", viper.GetString("prometheus.host"), err.Error())
		prometheusUp = false
		return err
	}
	metrics.MetricsRegistry.MustRegister(promDuration)
	metrics.MetricsRegistry.MustRegister(servicePushFailureCount)
	return nil
}

// Setup of services
func initServices() {
	// Grab HTGETTOKENOPTS if it's there
	viper.BindEnv("ORIG_HTGETTOKENOPTS", "HTGETTOKENOPTS")
	// If experiment or service is passed in on command line, ONLY generate and push tokens for that experiment/service
	switch {
	case viper.GetString("experiment") != "":
		// Running on a single experiment and all its roles
		experimentConfigPath := "experiments." + viper.GetString("experiment")
		experiment := checkExperimentOverride(viper.GetString("experiment"))
		for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
			services = addServiceToServicesSlice(services, viper.GetString("experiment"), experiment, role)
		}
	case viper.GetString("service") != "":
		// Running on a single service
		serviceExperiment, role := service.ExtractExperimentAndRoleFromServiceName(viper.GetString("service"))
		experiment := checkExperimentOverride(serviceExperiment)
		services = addServiceToServicesSlice(services, serviceExperiment, experiment, role)
	default:
		// Running on every configured experiment and role
		for configExperiment := range viper.GetStringMap("experiments") {
			experimentConfigPath := "experiments." + configExperiment
			experiment := checkExperimentOverride(configExperiment)
			for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
				services = addServiceToServicesSlice(services, configExperiment, experiment, role)
			}
		}
	}
}

// initTracing initializes the tracing configuration and returns a function to shutdown the
// initialized TracerProvider and an error, if any.
func initTracing(ctx context.Context) (func(context.Context), error) {
	url := viper.GetString("tracing.url")
	if url == "" {
		msg := "no tracing url configured.  Continuing without tracing"
		exeLogger.Error(msg)
		return nil, errors.New(msg)
	}
	tp, shutdown, err := tracing.NewOTLPHTTPTraceProvider(ctx, url, devEnvironmentLabel)
	if err != nil {
		exeLogger.Error("Could not obtain a TraceProvider.  Continuing without tracing")
		return nil, err
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{}) // In case any downstream services want to use this trace context
	exeLogger.Debug("Sending OTLP traces to ", url)
	return shutdown, nil
}

// Cleanup helper functions
func reportSuccessesAndFailures(successMap map[string]bool) error {
	startCleanup = time.Now()
	defer func() {
		if prometheusUp {
			promDuration.WithLabelValues(currentExecutable, "cleanup").Set(time.Since(startCleanup).Seconds())
		}
	}()

	successes := make([]string, 0, len(successMap))
	failures := make([]string, 0, len(successMap))

	var failCount float64 = 0
	for service, success := range successMap {
		if success {
			successes = append(successes, service)
		} else {
			failures = append(failures, service)
			failCount++
		}
	}
	servicePushFailureCount.Set(failCount)

	exeLogger.Infof("Successes: %s", strings.Join(successes, ", "))
	exeLogger.Infof("Failures: %s", strings.Join(failures, ", "))

	return nil
}

// General helper functions

// openDatabaseAndLoadServices opens a db.ManagedTokensDatabase and loads the configured services into
// the database.  If any of these operations fail, it returns a nil *db.ManagedTokensDatabase and an error.
// Otherwise, it returns the pointer to the db.ManagedTokensDatabase
func openDatabaseAndLoadServices(ctx context.Context) (*db.ManagedTokensDatabase, error) {
	var dbLocation string

	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "openDatabaseAndLoadService")
	defer span.End()

	// Open connection to the SQLite database where notification info will be stored
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}
	exeLogger.Debugf("Using db file at %s", dbLocation)

	database, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		err = fmt.Errorf("could not open or create ManagedTokensDatabase: %w", err)
		logErrorWithTracing(exeLogger, span, err)
		return nil, err
	}

	servicesToAddToDatabase := make([]string, 0, len(services))
	for _, s := range services {
		servicesToAddToDatabase = append(servicesToAddToDatabase, getServiceName(s))
	}

	if err := database.UpdateServices(ctx, servicesToAddToDatabase); err != nil {
		exeLogger.Error("Could not update database with currently-configured services.  Future database-based operations may fail")
	}

	logSuccessWithTracing(exeLogger, span, "Successfully opened database and loaded services")
	return database, nil
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

func resolveDebugAndVerboseFromFlags() {
	if viper.GetBool("debug") {
		viper.Set("verbose", true)
		// Only set the environment if MANAGED_TOKENS_DEBUG is UNSET.  If it is set to
		// "" or anything, we don't want to override it
		if val, ok := os.LookupEnv("MANAGED_TOKENS_DEBUG"); val == "" && !ok {
			os.Setenv("MANAGED_TOKENS_DEBUG", "1")
			debugEnvSetByFlag = true
		}
	}
}
