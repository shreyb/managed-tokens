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

	"github.com/fermitools/managed-tokens/internal/cmdUtils"
	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/vaultToken"
	"github.com/fermitools/managed-tokens/internal/worker"
)

var (
	currentExecutable       string
	buildTimestamp          string // Should be injected at build time with something like go build -ldflags="-X main.buildTimeStamp=$BUILDTIMESTAMP"
	version                 string // Should be injected at build time with something like go build -ldflags="-X main.version=$VERSION"
	exeLogger               *log.Entry
	notificationsDisabledBy cmdUtils.DisableNotificationsOption = cmdUtils.DISABLED_BY_CONFIGURATION
)

// devEnvironmentLabel can be set via config or environment variable MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL
var devEnvironmentLabel string

const devEnvironmentLabelDefault string = "production"

// Supported timeouts and their default values
var timeouts = map[string]time.Duration{
	"global":      time.Duration(300 * time.Second),
	"kerberos":    time.Duration(20 * time.Second),
	"vaultstorer": time.Duration(60 * time.Second),
	"ping":        time.Duration(10 * time.Second),
	"push":        time.Duration(30 * time.Second),
}

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

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.WithField("executable", currentExecutable).Error("Current user is root.  Please run this executable as a non-root user")
		return err
	}

	initFlags()
	if viper.GetBool("version") {
		fmt.Printf("Managed tokens libary version %s, build %s\n", version, buildTimestamp)
		return errExitOK
	}

	if err := initConfig(); err != nil {
		fmt.Println("Fatal error setting up configuration.  Exiting now")
		return err
	}
	// TODO Remove this after bug detailed in initFlags() is fixed upstream
	disableNotifyFlagWorkaround()
	// END TODO

	devEnvironmentLabel = getDevEnvironmentLabel()

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
	initLogs()
	initServices()
	if err := initTimeouts(); err != nil {
		log.WithField("executable", currentExecutable).Error("Fatal error setting up timeouts")
		return err
	}
	if err := initMetrics(); err != nil {
		log.WithField("executable", currentExecutable).Error("Error setting up metrics")
	}
	return nil
}

func initFlags() {
	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Flags
	pflag.StringP("experiment", "e", "", "Name of single experiment to push tokens")
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.StringP("service", "s", "", "Service to obtain and push vault tokens for.  Must be of the form experiment_role, e.g. dune_production")
	pflag.BoolP("test", "t", false, "Test mode.  Obtain vault tokens but don't push them to nodes")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.Bool("disable-notifications", false, "Turn off all notifications for this run")
	pflag.Bool("dont-notify", false, "Same as --disable-notifications")
	pflag.BoolP("verbose", "v", false, "Turn on verbose mode")
	pflag.String("admin", "", "Override the config file admin email")
	pflag.Bool("list-services", false, "List all configured services in config file")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

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
		notificationsDisabledBy = cmdUtils.DISABLED_BY_FLAG
	}
}

// Set up logs
func initLogs() {
	log.SetLevel(log.DebugLevel)
	debugLogConfigLookup := "logs.token-push.debugfile"
	logConfigLookup := "logs.token-push.logfile"
	// Debug log
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.DebugLevel: viper.GetString(debugLogConfigLookup),
		log.InfoLevel:  viper.GetString(debugLogConfigLookup),
		log.WarnLevel:  viper.GetString(debugLogConfigLookup),
		log.ErrorLevel: viper.GetString(debugLogConfigLookup),
		log.FatalLevel: viper.GetString(debugLogConfigLookup),
		log.PanicLevel: viper.GetString(debugLogConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

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
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		timeoutKey := strings.TrimSuffix(timeoutKey, "timeout")
		// Only save the timeout if it's supported, otherwise ignore it
		if _, ok := timeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				exeLogger.WithField("timeoutKey", timeoutKey).Warn("Could not parse configured timeout.  Using default")
				continue
			}
			timeouts[timeoutKey] = timeout
			exeLogger.WithField(timeoutKey, timeoutString).Debug("Configured timeout")
		}
	}

	// Verify that individual timeouts don't add to more than total timeout
	now := time.Now()
	timeForComponentCheck := now

	for timeoutKey, timeout := range timeouts {
		if timeoutKey != "global" {
			timeForComponentCheck = timeForComponentCheck.Add(timeout)
		}
	}

	timeForGlobalCheck := now.Add(timeouts["global"])
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
		tracing.LogErrorWithTrace(span, exeLogger, "Could not open or create ManagedTokensDatabase")
		return nil, err
	}

	servicesToAddToDatabase := make([]string, 0, len(services))
	for _, s := range services {
		servicesToAddToDatabase = append(servicesToAddToDatabase, cmdUtils.GetServiceName(s))
	}

	if err := database.UpdateServices(ctx, servicesToAddToDatabase); err != nil {
		exeLogger.Error("Could not update database with currently-configured services.  Future database-based operations may fail")
	}

	tracing.LogSuccessWithTrace(span, exeLogger, "Successfully opened database and loaded services")
	return database, nil
}

// initTracing initializes the tracing configuration and returns a function to shutdown the
// initialized TracerProvider and an error, if any.
func initTracing() (func(context.Context), error) {
	url := viper.GetString("tracing.url")
	if url == "" {
		msg := "no tracing URL configured.  Continuing without tracing"
		exeLogger.Error(msg)
		return nil, errors.New(msg)
	}
	tp, shutdown, err := tracing.JaegerTraceProvider(url, devEnvironmentLabel)
	if err != nil {
		exeLogger.Error("Could not obtain a TraceProvider.  Continuing without tracing")
		return nil, err
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{}) // In case any downstream services want to use this trace context
	return shutdown, nil
}

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
	if globalTimeout, ok = timeouts["global"]; !ok {
		exeLogger.Fatal("Could not obtain global timeout.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Tracing has to be initialized here and not in setup because we need our global context to pass to child spans
	if tracingShutdown, err := initTracing(); err == nil {
		defer tracingShutdown(ctx)
	}

	// Run our actual operation
	if err := run(ctx); err != nil {
		exeLogger.Fatal("Error running operations to push vault tokens.  Exiting")
	}
	exeLogger.Debug("Finished run")
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
	if notificationsDisabledBy == cmdUtils.DISABLED_BY_FLAG {
		blockAdminNotifications = true
		for _, s := range services {
			blockServiceNotificationsSlice = append(blockServiceNotificationsSlice, cmdUtils.GetServiceName(s))
		}
	} else {
		// Otherwise, look at the configuration to see if we should block notifications
		blockAdminNotifications, blockServiceNotificationsSlice = cmdUtils.ResolveDisableNotifications(services)
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
	var adminNotifications []notifications.SendMessager
	if !blockAdminNotifications {
		admNotMgr, adminNotifications = setupAdminNotifications(ctx, database)
		if databaseErr != nil {
			msg := "Could not open or create ManagedTokensDatabase"
			span.SetStatus(codes.Error, msg)
			admNotMgr.GetReceiveChan() <- notifications.NewSetupError(msg, currentExecutable)
		} else {
			defer database.Close()
		}
	}

	// All the cleanup actions that should run any time run() returns
	defer func() {
		// Run cleanup actions
		// Cleanup
		if err := reportSuccessesAndFailures(successfulServices); err != nil {
			tracing.LogErrorWithTrace(span, exeLogger, "Error aggregating successes and failures")
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

	serviceConfigs := make(map[string]*worker.Config) // Running map of the service configurations to pass to workers

	// This block launches a goroutine that listens for successfully-setup service configs (*worker.Config) objects
	// and then populates a map of those Config objects and a map to store the overall success status of each
	// service's token-push operations.  We also concurrently listen for failed service setups and populate the same
	// map so they can be reported at the end of the run as failures.
	//
	// It is synchronized by the collectServiceConfigs and serviceInitDone chans and execution of the program
	// is blocked until the serviceInitDone chan is closed.
	collectServiceConfigs := make(chan *worker.Config, len(services))
	collectFailedServiceSetups := make(chan string, len(services))
	serviceInitDone := make(chan struct{})
	go func() {
		var _wg sync.WaitGroup
		var _mux sync.Mutex

		defer close(serviceInitDone)

		_, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "collectServiceConfigs_anonFunc")
		defer span.End()

		_wg.Add(1)
		go func() {
			defer _wg.Done()
			for serviceConfig := range collectServiceConfigs {
				serviceName := cmdUtils.GetServiceName(serviceConfig.Service)
				serviceConfigs[serviceName] = serviceConfig
				_mux.Lock()
				successfulServices[serviceName] = false
				_mux.Unlock()
			}
		}()

		_wg.Add(1)
		go func() {
			defer _wg.Done()
			for failedService := range collectFailedServiceSetups {
				_mux.Lock()
				successfulServices[failedService] = false // These services will remain false throughout the main() run, and will be reported as failures at the end
				_mux.Unlock()
			}
		}()

		_wg.Wait()
	}()

	// Set up our serviceConfigs and load them into various collection channels
	// Execution of this program is blocked until the serviceConfigSetupWg waitgroup reaches zero.
	var serviceConfigSetupWg sync.WaitGroup
	for _, s := range services {
		serviceConfigSetupWg.Add(1)
		go func(s service.Service) {
			funcLogger := exeLogger.WithFields(log.Fields{
				"caller":  "token-push.run",
				"service": s.Name(),
			})

			// Setup the configs
			defer serviceConfigSetupWg.Done()

			ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "serviceConfigSetup_anonFunc")
			span.SetAttributes(attribute.KeyValue{Key: "service", Value: attribute.StringValue(s.Name())})
			defer span.End()

			// Create kerberos cache for this service
			krb5ccCache, err := os.CreateTemp(kerbCacheDir, fmt.Sprintf("managed-tokens-krb5ccCache-%s", s.Name()))
			if err != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "Cannot create kerberos cache. Subsequent operations will fail. Skipping service.")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}

			// All required service-level configuration items
			serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
			uid, err := getDesiredUIDByOverrideOrLookup(ctx, serviceConfigPath, database)
			if err != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "Error obtaining UID for service. Skipping service.")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}
			userPrincipal, htgettokenopts := cmdUtils.GetUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath)
			if userPrincipal == "" {
				tracing.LogErrorWithTrace(span, funcLogger, "Cannot have a blank userPrincipal. Skipping service")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}
			vaultServer, err := cmdUtils.GetVaultServer(serviceConfigPath)
			if err != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "Cannot proceed without vault server. Returning now.")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}
			schedds, err := cmdUtils.GetScheddsFromConfiguration(ctx, serviceConfigPath)
			if err != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "Cannot proceed without schedds. Returning now")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}

			// Service-level configuration items that can be defined either in configuration file or on system/environment or have library defaults
			collectorHost := cmdUtils.GetCondorCollectorHostFromConfiguration(serviceConfigPath)
			keytabPath := cmdUtils.GetKeytabFromConfiguration(serviceConfigPath)
			defaultRoleFileDestinationTemplate := getDefaultRoleFileDestinationTemplate(serviceConfigPath)
			serviceCreddVaultTokenPathRoot := cmdUtils.GetServiceCreddVaultTokenPathRoot(serviceConfigPath)
			vaultTokenStoreHoldoffFunc := getVaultTokenStoreHoldoffFuncOpt(s)
			fileCopierOptions := cmdUtils.GetFileCopierOptionsFromConfig(serviceConfigPath)
			extraPingOpts := cmdUtils.GetPingOptsFromConfig(serviceConfigPath)
			sshOpts := cmdUtils.GetSSHOptsFromConfig(serviceConfigPath)

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
				worker.SetSupportedExtrasKeyValue(worker.DefaultRoleFileDestinationTemplate, defaultRoleFileDestinationTemplate),
				worker.SetSupportedExtrasKeyValue(worker.FileCopierOptions, fileCopierOptions),
				worker.SetSupportedExtrasKeyValue(worker.PingOptions, extraPingOpts),
				worker.SetSupportedExtrasKeyValue(worker.SSHOptions, sshOpts),
				vaultTokenStoreHoldoffFunc,
			)
			if err != nil {
				tracing.LogErrorWithTrace(span, funcLogger, "Could not create config for service")
				collectFailedServiceSetups <- cmdUtils.GetServiceName(s)
				return
			}
			collectServiceConfigs <- c

			// If notifications are not disabled for this service, register the service for notifications
			if _, ok := noServiceNotifications[cmdUtils.GetServiceName(s)]; !ok && (admNotMgr != nil) {
				registerServiceNotificationsChan(ctx, s, admNotMgr)
			}

			span.SetStatus(codes.Ok, "Service config setup")
		}(s)
	}
	serviceConfigSetupWg.Wait()
	close(collectServiceConfigs)
	close(collectFailedServiceSetups)
	<-serviceInitDone // Don't move on until our serviceConfigs map is populated and our successfulServices map initialized

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
		ctx = utils.ContextWithVerbose(ctx)
	}

	// 1. Get kerberos tickets
	// Get channels and start worker for getting kerberos ticekts
	startKerberos := time.Now()
	span.AddEvent("Starting get kerberos tickets")
	kerberosChannels := startServiceConfigWorkerForProcessing(ctx, worker.GetKerberosTicketsWorker, serviceConfigs, "kerberos")

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
	condorVaultChans := startServiceConfigWorkerForProcessing(ctx, worker.StoreAndGetTokenWorker, serviceConfigs, "vaultstorer")

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
				exeLogger.WithField("service", serviceName).Error("Could not remove vault tokens for service")
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

	if len(serviceConfigs) == 0 {
		exeLogger.Info("No more serviceConfigs to operate on.  Cleaning up now")
		return nil
	}

	// 3. Ping nodes to check their status
	// Get channels and start worker for pinging service nodes
	startPing := time.Now()
	span.AddEvent("Start ping nodes")
	pingChans := startServiceConfigWorkerForProcessing(ctx, worker.PingAggregatorWorker, serviceConfigs, "ping")

	for pingSuccess := range pingChans.GetSuccessChan() {
		if !pingSuccess.GetSuccess() {
			msg := "Could not ping all nodes for service.  We'll still try to push tokens to all configured nodes, but there may be failures.  See logs for details"
			exeLogger.WithField("service", cmdUtils.GetServiceName(pingSuccess.GetService())).Error(msg)
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
	pushChans := startServiceConfigWorkerForProcessing(ctx, worker.PushTokensWorker, serviceConfigs, "push")

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[cmdUtils.GetServiceName(pushSuccess.GetService())] = true
		}
	}

	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "pushTokens").Set(time.Since(startPush).Seconds())
	}
	span.AddEvent("End push tokens")

	return nil
}

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
