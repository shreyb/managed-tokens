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

	"github.com/shreyb/managed-tokens/internal/cmdUtils"
	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/environment"
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

// Supported timeouts and their default values
var timeouts = map[string]time.Duration{
	"global":      time.Duration(300 * time.Second),
	"kerberos":    time.Duration(20 * time.Second),
	"vaultstorer": time.Duration(60 * time.Second),
	"ping":        time.Duration(10 * time.Second),
	"push":        time.Duration(30 * time.Second),
}

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
		timeoutKey := strings.TrimSuffix(timeoutKey, "timeout")
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
				"executable":   currentExecutable,
				"timeoutKey":   timeoutKey,
				"timeoutValue": timeout,
			}).Debug("Configured timeout")
			timeouts[timeoutKey] = timeout
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

// openDatabaseAndLoadServices opens a db.ManagedTokensDatabase and loads the configured services into
// the database.  If any of these operations fail, it returns a nil *db.ManagedTokensDatabase and an error.
// Otherwise, it returns the pointer to the db.ManagedTokensDatabase
func openDatabaseAndLoadServices() (*db.ManagedTokensDatabase, error) {
	var dbLocation string
	// Open connection to the SQLite database where notification info will be stored
	if viper.IsSet("dbLocation") {
		dbLocation = viper.GetString("dbLocation")
	} else {
		dbLocation = "/var/lib/managed-tokens/uid.db"
	}
	log.WithField("executable", currentExecutable).Debugf("Using db file at %s", dbLocation)

	database, err := db.OpenOrCreateDatabase(dbLocation)
	if err != nil {
		msg := "Could not open or create ManagedTokensDatabase"
		log.WithField("executable", currentExecutable).Error(msg)
		return nil, err
	}

	servicesToAddToDatabase := make([]string, 0, len(services))
	for _, s := range services {
		servicesToAddToDatabase = append(servicesToAddToDatabase, getServiceName(s))
	}

	if err := database.UpdateServices(context.Background(), servicesToAddToDatabase); err != nil {
		log.WithField("executable", currentExecutable).Error("Could not update database with currently-configured services.  Future database-based operations may fail")
	}

	return database, nil
}

func main() {
	// Global context
	var globalTimeout time.Duration
	var ok bool
	if globalTimeout, ok = timeouts["global"]; !ok {
		log.WithField("executable", currentExecutable).Fatal("Could not obtain global timeout.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Run our actual operation
	if err := run(ctx); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Error running operations to push vault tokens.  Exiting")
	}
	log.Debug("Finished run")
}

func run(ctx context.Context) error {
	// Order of operations:
	// 0. Setup (admin notifications, kerberos cache dir, generate worker.Configs, set up notification listeners)
	// 1. Get kerberos tickets
	// 2. Get and store vault tokens
	// 3. Ping nodes to check their status
	// 4. Push vault tokens to nodes
	var setupWg sync.WaitGroup                  // WaitGroup to keep track of concurrent setup actions
	successfulServices := make(map[string]bool) // Initialize Map of services for which all steps were successful

	sendAdminNotifications := func(adminNotificationsChan chan notifications.Notification, adminNotificationsPtr *[]notifications.SendMessager) error {
		handleNotificationsFinalization()
		close(adminNotificationsChan)
		err := notifications.SendAdminNotifications(
			ctx,
			currentExecutable,
			viper.GetString("templates.adminerrors"),
			viper.GetBool("test"),
			(*adminNotificationsPtr)...,
		)
		if err != nil {
			// We don't want to halt execution at this point
			log.WithField("executable", currentExecutable).Error("Error sending admin notifications")
		}
		return err
	}

	database, databaseErr := openDatabaseAndLoadServices()

	// Send admin notifications at end of run.  Note that if databaseErr != nil, then database = nil.
	adminNotifications, adminNotificationsChan := setupAdminNotifications(ctx, database)
	if databaseErr != nil {
		adminNotificationsChan <- notifications.NewSetupError("Could not open or create ManagedTokensDatabase", currentExecutable)
	} else {
		defer database.Close()
	}

	// We don't check the error here, because we don't want to halt execution if the admin message can't be sent.  Just log it and move on
	defer sendAdminNotifications(adminNotificationsChan, &adminNotifications)

	// All the cleanup actions that should run any time run() returns
	defer func() {
		// Run cleanup actions
		func(successfulServices map[string]bool) { // Cleanup
			if err := reportSuccessesAndFailures(ctx, successfulServices); err != nil {
				log.WithField("executable", currentExecutable).Error("Error aggregating successes and failures")
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

	// Create temporary dir for all kerberos caches to live in
	krb5ccname, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		log.WithField("executable", currentExecutable).Error("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Returning")
		return err
	}
	defer func() {
		// Clear kerberos cache dir
		if err := os.RemoveAll(krb5ccname); err != nil {
			log.WithFields(
				log.Fields{
					"executable":   currentExecutable,
					"kerbCacheDir": krb5ccname,
				}).Error("Could not clear kerberos cache.  Please clean up manually")
			return
		}
		log.WithField("executable", currentExecutable).Info("Cleared kerberos cache")
	}()

	// For all services, initialize success value to false
	initializeSuccessfulServices := make(chan string, len(services))
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		for service := range initializeSuccessfulServices {
			successfulServices[service] = false
		}
	}()

	// Set up our service config collector
	collectServiceConfigs := make(chan *worker.Config, len(services))
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		defer close(initializeSuccessfulServices)
		for serviceConfig := range collectServiceConfigs {
			serviceName := getServiceName(serviceConfig.Service)
			serviceConfigs[serviceName] = serviceConfig
			initializeSuccessfulServices <- serviceName
		}
	}()

	// Set up our serviceConfigs and load them into various collection channels
	var serviceConfigSetupWg sync.WaitGroup
	for _, s := range services {
		serviceConfigSetupWg.Add(1)
		go func(s service.Service) {
			// Setup the configs
			defer serviceConfigSetupWg.Done()
			serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
			uid, err := getDesiredUIByOverrideOrLookup(ctx, serviceConfigPath, database)
			if err != nil {
				log.WithFields(log.Fields{
					"caller":  "token-push.run",
					"service": s.Name(),
				}).Error("Error obtaining UID for service.  Skipping service.")
				return
			}
			userPrincipal, htgettokenopts := cmdUtils.GetUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath, s.Experiment())
			if userPrincipal == "" {
				log.Error("Cannot have a blank userPrincipal.  Skipping service")
				return
			}
			collectorHost := cmdUtils.GetCondorCollectorHostFromConfiguration(serviceConfigPath)
			schedds := cmdUtils.GetScheddsFromConfiguration(serviceConfigPath)
			keytabPath := cmdUtils.GetKeytabOverrideFromConfiguration(serviceConfigPath)
			defaultRoleFileDestinationTemplate := getDefaultRoleFileDestinationTemplate(serviceConfigPath)
			c, err := worker.NewConfig(
				s,
				worker.SetCommandEnvironment(
					func(e *environment.CommandEnvironment) { e.SetKrb5CCName(krb5ccname, environment.DIR) },
					func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collectorHost) },
					func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) },
				),
				worker.SetSchedds(schedds),
				worker.SetUserPrincipal(userPrincipal),
				worker.SetKeytabPath(keytabPath),
				worker.SetDesiredUID(uid),
				worker.SetNodes(viper.GetStringSlice(serviceConfigPath+".destinationNodes")),
				worker.SetAccount(viper.GetString(serviceConfigPath+".account")),
				worker.SetSupportedExtrasKeyValue(worker.DefaultRoleFileTemplate, defaultRoleFileDestinationTemplate),
			)
			if err != nil {
				log.WithFields(log.Fields{
					"experiment": s.Experiment(),
					"role":       s.Role(),
				}).Error("Could not create config for service")
				return
			}
			collectServiceConfigs <- c
			registerServiceNotificationsChan(ctx, s, database)
		}(s)
	}
	serviceConfigSetupWg.Wait()
	close(collectServiceConfigs)
	setupWg.Wait() // Don't move on until our serviceConfigs map is populated and our successfulServices map initialized

	// Add our configured nodes to managed tokens database
	nodesToAddToDatabase := make([]string, 0)
	for _, serviceConfig := range serviceConfigs {
		nodesToAddToDatabase = append(nodesToAddToDatabase, serviceConfig.Nodes...)
	}
	if err := database.UpdateNodes(ctx, nodesToAddToDatabase); err != nil {
		log.WithField("executable", currentExecutable).Error("Could not update database with currently-configured nodes.  Future database-based operations may fail")
	}

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
	kerberosChannels := startServiceConfigWorkerForProcessing(ctx, worker.GetKerberosTicketsWorker, serviceConfigs, "kerberos")

	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	failedKerberosConfigs := removeFailedServiceConfigs(kerberosChannels, serviceConfigs)
	for _, failure := range failedKerberosConfigs {
		log.WithField(
			"service", failure.Service.Name(),
		).Error("Failed to obtain kerberos ticket.  Will not try to obtain or push vault token to service nodes")
	}
	if len(serviceConfigs) == 0 {
		log.WithField("executable", currentExecutable).Info("No more serviceConfigs to operate on.  Cleaning up now")
		return nil
	}

	// 2. Get and store vault tokens
	// Get channels and start worker for getting and storing short-lived vault token (condor_vault_storer)
	condorVaultChans := startServiceConfigWorkerForProcessing(ctx, worker.StoreAndGetTokenWorker, serviceConfigs, "vaultstorer")

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
		return nil
	}

	if len(serviceConfigs) == 0 {
		log.WithField("executable", currentExecutable).Info("No more serviceConfigs to operate on.  Cleaning up now")
		return nil
	}

	// 3. Ping nodes to check their status
	// Get channels and start worker for pinging service nodes
	pingChans := startServiceConfigWorkerForProcessing(ctx, worker.PingAggregatorWorker, serviceConfigs, "ping")

	for pingSuccess := range pingChans.GetSuccessChan() {
		if !pingSuccess.GetSuccess() {
			msg := "Could not ping all nodes for service.  We'll still try to push tokens to all configured nodes, but there may be failures.  See logs for details"
			log.WithField("service", getServiceName(pingSuccess.GetService())).Error(msg)
		}
	}

	// 4. Push vault tokens to nodes
	// Get channels and start worker for pushing tokens to service nodes
	pushChans := startServiceConfigWorkerForProcessing(ctx, worker.PushTokensWorker, serviceConfigs, "push")

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[getServiceName(pushSuccess.GetService())] = true
		}
	}

	return nil
}

func reportSuccessesAndFailures(ctx context.Context, successMap map[string]bool) error {
	startCleanup = time.Now()
	defer func() {
		if prometheusUp {
			promDuration.WithLabelValues(currentExecutable, "cleanup").Set(time.Since(startCleanup).Seconds())
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
