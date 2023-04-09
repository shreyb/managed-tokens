package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

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

const globalTimeoutDefaultStr string = "300s"

var (
	timeouts          = make(map[string]time.Duration)
	supportedTimeouts = map[string]struct{}{
		"globaltimeout":      {},
		"kerberostimeout":    {},
		"vaultstorertimeout": {},
	}
)

// Initial setup.  Read flags, find config file
func init() {
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

	if err := initServices(); err != nil {
		fmt.Println("Fatal error in parsing service to run onboarding for")
		os.Exit(1)
	}

	initLogs()
	if err := initTimeouts(); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Fatal error setting up timeouts")
	}
}

func initFlags() {
	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Parse our command-line arguments
	pflag.Usage = onboardingUsage
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")
	pflag.BoolP("verbose", "v", false, "Turn on verbose mode")
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
		log.WithField("executable", currentExecutable).Errorf("Fatal error reading in config file: %v", err)
		return err
	}
	return nil
}

func initServices() error {
	if pflag.NArg() != 0 {
		viper.Set("service", pflag.Arg(0))
	}

	// If no service is set, exit, since we only want to onboard a single service at a time
	if viper.GetString("service") == "" {
		log.WithField("executable", currentExecutable).Error("A service must be given on the command line for run-onboarding")
		onboardingUsage()
		return errors.New("invalid service")
	}
	return nil
}

func initLogs() {
	// Set up logs
	log.SetLevel(log.DebugLevel)
	debugLogConfigLookup := "logs.run-onboarding-managed-tokens.debugfile"
	logConfigLookup := "logs.run-onboarding-managed-tokens.logfile"
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

	// Test flag sets which notifications section from config we want to use.
	if viper.GetBool("test") {
		log.WithField("executable", currentExecutable).Info("Running in test mode")
	}
}

// Setup of timeouts, if they're set
func initTimeouts() error {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		if _, ok := supportedTimeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				log.WithFields(log.Fields{
					"executable": currentExecutable,
					timeoutKey:   timeoutString,
				}).Warn("Could not parse configured timeout duration.  Using default")
				continue
			}
			log.WithFields(log.Fields{
				"executable": currentExecutable,
				timeoutKey:   timeoutString,
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

func main() {
	var globalTimeout time.Duration
	var ok bool
	var err error

	if globalTimeout, ok = timeouts["globaltimeout"]; !ok {
		log.WithField("executable", currentExecutable).Debugf("Global timeout not configured in config file.  Using default global timeout of %s", globalTimeoutDefaultStr)
		if globalTimeout, err = time.ParseDuration(globalTimeoutDefaultStr); err != nil {
			log.WithField("executable", currentExecutable).Fatal("Could not parse default global timeout.")
		}
	}

	// Global context
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Run our actual operation
	if err := run(ctx); err != nil {
		log.WithField("executable", currentExecutable).Fatal("Error running operations to update database from FERRY.  Exiting")
	}
	log.Debug("Finished run")

}

func run(ctx context.Context) error {
	// Order of operations:
	// 1. Generate kerberos principal for service
	// 2. Store (and obtain) vault tokens for service, running in interactive mode so user can authenticate if needed
	var serviceConfig *worker.Config

	// Grab HTGETTOKENOPTS if it's there
	viper.BindEnv("ORIG_HTGETTOKENOPTS", "HTGETTOKENOPTS")

	// Temporary directory for kerberos caches
	krb5ccname, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		log.WithField("executable", currentExecutable).Error("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
		return err
	}
	defer func() {
		os.RemoveAll(krb5ccname)
		log.WithField("executable", currentExecutable).Info("Cleared kerberos cache")
	}()

	// Processing

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = utils.ContextWithVerbose(ctx)
	}

	// Determine what the real experiment name should be
	givenServiceExperiment, givenRole := service.ExtractExperimentAndRoleFromServiceName(viper.GetString("service"))
	experiment := checkExperimentOverride(givenServiceExperiment)

	// If we're reading from an experiment config entry that has an overridden experiment
	// s should be of type ExperimentOverriddenService.  Else, it should use the normal
	// service.NewService constructor
	var s service.Service
	if experiment != givenServiceExperiment {
		serviceName := experiment + "_" + givenRole
		s = newExperimentOverridenService(serviceName, givenServiceExperiment)
	} else {
		s = service.NewService(viper.GetString("service"))
	}

	// Set up service config
	serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
	serviceConfig, err = worker.NewConfig(
		s,
		setkrb5ccname(krb5ccname),
		setCondorCreddHost(serviceConfigPath),
		setCondorCollectorHost(serviceConfigPath),
		setSchedds(serviceConfigPath),
		setUserPrincipalAndHtgettokenopts(serviceConfigPath, s.Experiment()),
		setKeytabOverride(serviceConfigPath),
		account(serviceConfigPath),
	)
	if err != nil {
		log.WithFields(log.Fields{
			"experiment": s.Experiment(),
			"role":       s.Role(),
		}).Error("Could not create config for service")
		return err
	}

	// 1. Get Kerberos ticket
	// Channel, context, and worker for getting kerberos ticket
	var kerberosContext context.Context
	if kerberosTimeout, ok := timeouts["kerberostimeout"]; ok {
		kerberosContext = utils.ContextWithOverrideTimeout(ctx, kerberosTimeout)
	} else {
		kerberosContext = ctx
	}
	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	if err := worker.GetKerberosTicketandVerify(kerberosContext, serviceConfig); err != nil {
		log.WithField(
			"service", getServiceName(serviceConfig.Service),
		).Error("Failed to obtain kerberos ticket. Stopping onboarding")
		return errors.New("could not obtain kerberos ticket")
	}

	// 2.  Get and store vault tokens for service
	var vaultStorerContext context.Context
	if vaultStorerTimeout, ok := timeouts["vaultstorertimeout"]; ok {
		vaultStorerContext = utils.ContextWithOverrideTimeout(ctx, vaultStorerTimeout)
	} else {
		vaultStorerContext = ctx
	}
	if err := worker.StoreAndGetRefreshAndVaultTokens(vaultStorerContext, serviceConfig); err != nil {
		log.WithFields(log.Fields{
			"experiment": serviceConfig.Service.Experiment(),
			"role":       serviceConfig.Service.Role(),
		}).Error("Could not generate refresh tokens and store vault token for service")
		return err
	}
	if err := vaultToken.RemoveServiceVaultTokens(viper.GetString("service")); err != nil {
		log.WithField("service", viper.GetString("service")).Error("Could not remove vault tokens for service.  Please clean up manually")
	}

	log.WithField("service", getServiceName(serviceConfig.Service)).Info("Successfully generated refresh token in vault.  Onboarding complete.")
	return nil

}
