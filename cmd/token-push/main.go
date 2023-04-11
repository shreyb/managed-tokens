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
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/rifflock/lfshook"
	"github.com/spf13/pflag"

	"github.com/shreyb/managed-tokens/internal/metrics"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/vaultToken"
	"github.com/shreyb/managed-tokens/internal/worker"
)

var (
	currentExecutable string
	buildTimestamp    string
	version           string
)

// Timeouts
const globalTimeoutDefaultStr string = "300s"

// Supported timeouts and their default values
var timeouts = map[string]time.Duration{
	"globalTimeout":      time.Duration(300 * time.Second),
	"kerberosTimeout":    time.Duration(20 * time.Second),
	"vaultStorerTimeout": time.Duration(60 * time.Second),
	"pingTimeout":        time.Duration(10 * time.Second),
	"pushTimeout":        time.Duration(30 * time.Second),
}

var adminNotifications = make([]notifications.SendMessager, 0)

var (
	startSetup      time.Time
	startProcessing time.Time
	startCleanup    time.Time
	prometheusUp    = true
)

// Metrics
var (
	promDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "stage_duration_seconds",
		Help:      "The amount of time it took to run a stage (setup|processing|cleanup) of a Managed Tokens Service executable",
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
	services       []service.Service
	serviceConfigs = make(map[string]*worker.Config)
)

// Initial setup.  Read flags, find config file
func init() {
	startSetup = time.Now()

	// Get current executable name
	if exePath, err := os.Executable(); err != nil {
		log.Error("Could not get path of current executable")
	} else {
		currentExecutable = path.Base(exePath)
	}

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Current user is root.  Please run this executable as a non-root user")
	}

	initFlags()
	if viper.GetBool("version") {
		fmt.Printf("Managed tokens libary version %s, build %s\n", version, buildTimestamp)
		os.Exit(0)
	}

	if err := initConfig(); err != nil {
		fmt.Println("Fatal error setting up configuration.  Exiting now")
		os.Exit(1)
	}

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
		os.Exit(0)
	}
	initLogs()
	initServices()
	if err := initTimeouts(); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Fatal error setting up timeouts")
	}
	if err := initMetrics(); err != nil {
		log.WithField("executable", currentExecutable).Error("Error setting up metrics")
	}
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
	pflag.BoolP("verbose", "v", false, "Turn on verbose mode")
	pflag.String("admin", "", "Override the config file admin email")
	pflag.Bool("list-services", false, "List all configured services in config file")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)
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

	log.WithField("executable", currentExecutable).Debugf("Using config file %s", viper.ConfigFileUsed())

	if viper.GetBool("test") {
		log.WithField("executable", currentExecutable).Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func initTimeouts() error {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		// Only save the timeout if it's supported, otherwise ignore it
		if _, ok := timeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				log.WithFields(log.Fields{
					"executable": currentExecutable,
					"timeoutKey": timeoutKey,
				}).Warn("Could not parse configured timeout.  Using default")
			}
			log.WithFields(log.Fields{
				"executable": currentExecutable,
				"timeoutKey": timeoutKey,
			}).Debug("Configured timeout")
			timeouts[timeoutKey] = timeout
		}
	}

	// Verify that individual timeouts don't add to more than total timeout
	now := time.Now()
	timeForComponentCheck := now

	for timeoutKey, timeout := range timeouts {
		if timeoutKey != "globaltimeout" {
			timeForComponentCheck = timeForComponentCheck.Add(timeout)
		}
	}

	timeForGlobalCheck := now.Add(timeouts["globaltimeout"])
	if timeForComponentCheck.After(timeForGlobalCheck) {
		msg := "configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts"
		log.WithField("executable", currentExecutable).Error(msg)
		return errors.New(msg)
	}
	return nil
}

// Set up prometheus metrics
func initMetrics() error {
	// Set up prometheus metrics
	if _, err := http.Get(viper.GetString("prometheus.host")); err != nil {
		log.WithField("executable", currentExecutable).Errorf("Error contacting prometheus pushgateway %s: %s.  The rest of prometheus operations will fail. "+
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

func main() {
	// Order of operations:
	//
	// 0. Setup (global context, generate worker.Configs, set up notification listeners)
	// 1. Get kerberos tickets
	// 2. Get and store vault tokens
	// 3. Ping nodes to check their status
	// 4. Push vault tokens to nodes
	var globalTimeout time.Duration
	var setupWg sync.WaitGroup

	// Global context
	var ok bool
	var err error
	if globalTimeout, ok = timeouts["globaltimeout"]; !ok {
		log.WithField("executable", currentExecutable).Debugf("Global timeout not configured in config file.  Using default global timeout of %s", globalTimeoutDefaultStr)
		if globalTimeout, err = time.ParseDuration(globalTimeoutDefaultStr); err != nil {
			log.WithField("executable", currentExecutable).Fatal("Could not parse default global timeout.")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Set up admin notifications
	adminNotifications = setupAdminNotifications()

	// Set up our service config collector
	// TODO need to check for config key here too Maybe an interface that gets the proper config name?
	collectServiceConfigs := make(chan *worker.Config, len(services))
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		for serviceConfig := range collectServiceConfigs {
			serviceConfigs[getServiceName(serviceConfig.Service)] = serviceConfig
		}
	}()

	// Initialize Map of services for which all steps were successful
	successfulServices := make(map[string]bool)
	initializeSuccessfulServices := make(chan string, len(services))
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		for service := range initializeSuccessfulServices {
			successfulServices[service] = false
		}
	}()

	// Create temporary dir for all kerberos caches to live in
	krb5ccname, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		log.WithField("executable", currentExecutable).Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}

	// All the cleanup actions needed to run any time main() returns
	defer func() {
		// Wait for all notifications to finish before moving onto cleanup
		handleNotificationsFinalization()
		// Clear kerberos cache
		os.RemoveAll(krb5ccname)
		log.WithField("executable", currentExecutable).Info("Cleared kerberos cache")
		// Run cleanup actions
		func(successfulServices map[string]bool) { // Cleanup
			if err := cleanup(ctx, successfulServices); err != nil {
				log.WithField("executable", currentExecutable).Fatal("Error cleaning up")
			}
		}(successfulServices)
		// Push metrics to prometheus pushgateway
		if prometheusUp {
			if err := metrics.PushToPrometheus(); err != nil {
				log.WithField("executable", currentExecutable).Error("Could not push metrics to prometheus pushgateway")
			} else {
				log.WithField("executable", currentExecutable).Info("Finished pushing metrics to prometheus pushgateway")
			}
		}
	}()

	// Set up our serviceConfigs and load them into various collection channels
	func() {
		var serviceConfigSetupWg sync.WaitGroup
		defer close(collectServiceConfigs)
		defer close(initializeSuccessfulServices)
		for _, s := range services {
			// Setup the configs
			serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
			serviceConfigSetupWg.Add(1)
			go func(s service.Service, serviceConfigPath string) {
				defer serviceConfigSetupWg.Done()
				c, err := worker.NewConfig(
					s,
					setkrb5ccname(krb5ccname),
					setCondorCreddHost(serviceConfigPath),
					setSchedds(serviceConfigPath),
					setCondorCollectorHost(serviceConfigPath),
					setUserPrincipalAndHtgettokenopts(serviceConfigPath, s.Experiment()),
					setKeytabOverride(serviceConfigPath),
					setDesiredUIByOverrideOrLookup(ctx, serviceConfigPath),
					destinationNodes(serviceConfigPath),
					account(serviceConfigPath),
					setDefaultRoleFileDestinationTemplate(serviceConfigPath),
				)
				if err != nil {
					log.WithFields(log.Fields{
						"experiment": s.Experiment(),
						"role":       s.Role(),
					}).Fatal("Could not create config for service")
				}
				collectServiceConfigs <- c
				initializeSuccessfulServices <- getServiceName(s)
				registerServiceNotificationsChan(ctx, s, &notificationsManagersWg)
			}(s, serviceConfigPath)
		}
		serviceConfigSetupWg.Wait()
	}()
	setupWg.Wait() // Don't move on until our serviceConfigs map is populated and our successfulServices map initialized

	// Setup done.  Push prometheus metrics
	log.WithField("executable", currentExecutable).Debug("Setup complete")
	if prometheusUp {
		promDuration.WithLabelValues(currentExecutable, "setup").Set(time.Since(startSetup).Seconds())
	}

	// Begin Processing
	startProcessing = time.Now()
	defer func() {
		if prometheusUp {
			promDuration.WithLabelValues(currentExecutable, "processing").Set(time.Since(startProcessing).Seconds())
		}
	}()

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = utils.ContextWithVerbose(ctx)
	}

	// 1. Get kerberos tickets
	// Get channels and start worker for getting kerberos ticekts
	kerberosChannels := startServiceConfigWorkerForProcessing(ctx, worker.GetKerberosTicketsWorker, serviceConfigs, "kerberostimeout")

	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	failedKerberosConfigs := removeFailedServiceConfigs(kerberosChannels, serviceConfigs)
	for _, failure := range failedKerberosConfigs {
		log.WithField(
			"service", failure.Service.Name(),
		).Error("Failed to obtain kerberos ticket.  Will not try to obtain or push vault token to service nodes")
	}
	if len(serviceConfigs) == 0 {
		return
	}

	// 2. Get and store vault tokens
	// Get channels and start worker for getting and storing short-lived vault token (condor_vault_storer)
	condorVaultChans := startServiceConfigWorkerForProcessing(ctx, worker.StoreAndGetTokenWorker, serviceConfigs, "vaultstorertimeout")

	// To avoid kerberos cache race conditions, condor_vault_storer must be run sequentially, so we'll wait until all are done,
	// remove any service configs that we couldn't get tokens for from serviceConfigs, and then begin transferring to nodes
	failedVaultConfigs := removeFailedServiceConfigs(condorVaultChans, serviceConfigs)
	for _, failure := range failedVaultConfigs {
		log.WithField(
			"service", failure.Service.Name(),
		).Error("Failed to obtain vault token.  Will not try to push vault token to service nodes")
	}

	// For any successful services, make sure we remove all the vault tokens when we're done
	for serviceName := range serviceConfigs {
		defer func(serviceName string) {
			if err := vaultToken.RemoveServiceVaultTokens(serviceName); err != nil {
				log.WithField("service", serviceName).Error("Could not remove vault tokens for service")
			}
		}(serviceName)
	}

	// If we're in test mode, stop here
	if viper.GetBool("test") {
		log.Info("Test mode.  Cleaning up now")

		for service := range serviceConfigs {
			successfulServices[service] = true
		}
		return
	}

	if len(serviceConfigs) == 0 {
		log.WithField("executable", currentExecutable).Info("No more serviceConfigs to operate on.  Cleaning up now")
		return
	}

	// 3. Ping nodes to check their status
	// Get channels and start worker for pinging service nodes
	pingChans := startServiceConfigWorkerForProcessing(ctx, worker.PingAggregatorWorker, serviceConfigs, "pingtimeout")

	for pingSuccess := range pingChans.GetSuccessChan() {
		if !pingSuccess.GetSuccess() {
			msg := "Could not ping all nodes for service.  We'll still try to push tokens to all configured nodes, but there may be failures.  See logs for details"
			log.WithField("service", getServiceName(pingSuccess.GetService())).Error(msg)
		}
	}

	// 4. Push vault tokens to nodes
	// Get channels and start worker for pushing tokens to service nodes
	pushChans := startServiceConfigWorkerForProcessing(ctx, worker.PushTokensWorker, serviceConfigs, "pushtimeout")

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[getServiceName(pushSuccess.GetService())] = true
		}
	}

}

func cleanup(ctx context.Context, successMap map[string]bool) error {
	startCleanup = time.Now()
	defer func() {
		if prometheusUp {
			promDuration.WithLabelValues(currentExecutable, "cleanup").Set(time.Since(startCleanup).Seconds())
		}
	}()

	defer func() {
		if err := notifications.SendAdminNotifications(
			ctx,
			"token-push",
			viper.GetString("templates.adminerrors"),
			viper.GetBool("test"),
			adminNotifications...,
		); err != nil {
			log.Error("Error sending admin notifications")
		}
	}()

	successes := make([]string, 0, len(successMap))
	failures := make([]string, 0, len(successMap))

	for service, success := range successMap {
		if success {
			successes = append(successes, service)
		} else {
			failures = append(failures, service)
			servicePushFailureCount.Inc()
		}
	}

	log.Infof("Successes: %s", strings.Join(successes, ", "))
	log.Infof("Failures: %s", strings.Join(failures, ", "))

	return nil
}

// admin notifications setup moved to new func from init
// adminNotifications no longer var
// adminNotifications use this trick to send: https://go.dev/play/p/rww0ORt94pU (pointer)
// cleanup split up - consolidate handlNotificationsFinealization into adminNotifications sending
// initServices channel and serviceConfig channel are consolidated
