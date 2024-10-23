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
	"os"
	"path"
	"strings"
	"time"

	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"

	"github.com/fermitools/managed-tokens/internal/cmdUtils"
	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/service"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
	"github.com/fermitools/managed-tokens/internal/vaultToken"
	"github.com/fermitools/managed-tokens/internal/worker"
)

var (
	currentExecutable string
	buildTimestamp    string // Should be injected at build time with something like go build -ldflags="-X main.buildTimeStamp=$BUILDTIMESTAMP"
	version           string // Should be injected at build time with something like go build -ldflags="-X main.version=$VERSION"
	exeLogger         *log.Entry
)

var devEnvironmentLabel string

const devEnvironmentLabelDefault string = "production"

// Supported timeouts that can be read in from configuration file and their default values
var timeouts = map[string]time.Duration{
	"global":   time.Duration(300 * time.Second),
	"kerberos": time.Duration(20 * time.Second),
}

var errExitOK = errors.New("exit 0") // Error to return when a function wants to indicate to caller to exit with code 0

// Initial setup.  Read flags, find config file
func setup() error {
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

	versionMessage := fmt.Sprintf("Managed tokens libary version %s, build %s", version, buildTimestamp)
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

	if err := initServices(); err != nil {
		setupLogger.Error("Fatal error in parsing service to run onboarding for")
		return err
	}

	initLogs()

	// Test flag sets which notifications section from config we want to use.
	if viper.GetBool("test") {
		setupLogger.Info("Running in test mode")
	}

	if err := initTimeouts(); err != nil {
		setupLogger.Error("Fatal error setting up timeouts")
		return err
	}

	return nil
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
	pflag.String("timeout", "60s", "Timeout for vault_storer portion of run")
	pflag.StringP("service", "s", "", "Service (experiment_role) to onboard")

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

// Environment variables to read into Viper config
// Note: In keeping with best practices, these can be overridden by command line flags or direct
// in-code overrides.  This only sets the initial state of the given viper keys.
func initEnvironment() {
	viper.BindEnv("collectorHost", environment.CondorCollectorHost.EnvVarKey())
}

func initServices() error {
	if pflag.NArg() != 0 && viper.GetString("service") == "" {
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

// Custom usage function for positional argument.
func onboardingUsage() {
	fmt.Printf("Usage: %s [OPTIONS] service...\n", os.Args[0])
	fmt.Printf("service must be of the form 'experiment_role', e.g. 'dune_production'\n")
	pflag.PrintDefaults()
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

	exeLogger = log.WithField("executable", currentExecutable)
	exeLogger.Debugf("Using config file %s", viper.ConfigFileUsed())
}

// Setup of timeouts, if they're set
func initTimeouts() error {
	// Save supported timeouts into global timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		timeoutKey := strings.TrimSuffix(timeoutKey, "timeout")
		// Only save the timeout if it's supported, otherwise ignore it
		if _, ok := timeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				exeLogger.WithField(timeoutKey, timeoutString).Warn("Could not parse configured timeout duration.  Using default")
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

	// If we have a timeout from the command line, override global setting, save timeout in vaultstorer key
	if timeout := viper.GetString("timeout"); timeout != "" {
		vaultStorerTimeout, err := time.ParseDuration(timeout)
		if err != nil {
			exeLogger.WithField("timeout", timeout).Error("Could not parse timeout duration from command line")
			return err
		}
		timeouts["vaultstorer"] = vaultStorerTimeout

		if vaultStorerTimeout > timeouts["global"] {
			exeLogger.Info("Command-line vault_storer timeout exceeds global timeout.  Overriding global timeout")
			timeouts["global"] = vaultStorerTimeout
		}
	}
	return nil
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
	return shutdown, nil
}

func main() {
	if err := setup(); err != nil {
		if errors.Is(err, errExitOK) {
			os.Exit(0)
		}
		log.Fatal("Error running setup actions.  Exiting")
	}

	var globalTimeout time.Duration
	var ok bool

	if globalTimeout, ok = timeouts["global"]; !ok {
		exeLogger.Fatal("Could not obtain global timeout.")
	}

	// Global context
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Tracing has to be initialized here and not in setup because we need our global context to pass to child spans
	if tracingShutdown, err := initTracing(ctx); err == nil {
		defer tracingShutdown(ctx)
	}

	// Run our actual operation
	if err := run(ctx); err != nil {
		exeLogger.Fatal("Error running onboarding.  Exiting")
	}
	exeLogger.Debug("Finished run")

}

func run(ctx context.Context) error {
	// Order of operations:
	// 1. Generate kerberos principal for service
	// 2. Store (and obtain) vault tokens for service, running in interactive mode so user can authenticate if needed
	ctx, span := otel.GetTracerProvider().Tracer("run-onboarding-managed-tokens").Start(ctx, "run-onboarding-managed-tokens")
	span.SetAttributes(attribute.KeyValue{Key: "service", Value: attribute.StringValue(viper.GetString("service"))})
	defer span.End()

	var serviceConfig *worker.Config

	// Grab HTGETTOKENOPTS if it's there
	viper.BindEnv("ORIG_HTGETTOKENOPTS", "HTGETTOKENOPTS")

	// Temporary directory for kerberos caches
	krb5ccname, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		tracing.LogErrorWithTrace(span, exeLogger, "Could not create temporary directory for kerberos cache. This will cause a fatal race condition.  Exiting")
		return err
	}
	defer func() {
		os.RemoveAll(krb5ccname)
		exeLogger.Info("Cleared kerberos cache")
	}()

	// Processing

	// Add verbose to the global context
	if viper.GetBool("verbose") {
		ctx = utils.ContextWithVerbose(ctx)
	}

	// Determine what the real experiment name should be
	givenServiceExperiment, givenRole := service.ExtractExperimentAndRoleFromServiceName(viper.GetString("service"))
	experiment := cmdUtils.CheckExperimentOverride(givenServiceExperiment)
	span.SetAttributes(attribute.KeyValue{Key: "experiment", Value: attribute.StringValue(experiment)})

	// If we're reading from an experiment config entry that has an overridden experiment
	// s should be of type ExperimentOverriddenService.  Else, it should use the normal
	// service.NewService constructor
	var s service.Service
	if experiment != givenServiceExperiment {
		serviceName := experiment + "_" + givenRole
		s = cmdUtils.NewExperimentOverriddenService(serviceName, givenServiceExperiment)
	} else {
		s = service.NewService(viper.GetString("service"))
	}
	funcLogger := exeLogger.WithField("service", s.Name())

	// Set up service config
	serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
	userPrincipal, htgettokenopts := cmdUtils.GetUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath)
	if userPrincipal == "" {
		tracing.LogErrorWithTrace(span, funcLogger, "Cannot have a blank userPrincipal.  Exiting")
		return errors.New("blank userPrincipal")
	}
	vaultServer, err := cmdUtils.GetVaultServer(serviceConfigPath)
	if err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Cannot proceed without vault server.  Exiting")
		return err
	}
	collectorHost, schedds, err := cmdUtils.GetScheddsAndCollectorHostFromConfiguration(ctx, serviceConfigPath)
	if err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Cannot proceed without schedds.  Exiting")
		return err
	}

	keytabPath := cmdUtils.GetKeytabFromConfiguration(serviceConfigPath)
	serviceCreddVaultTokenPathRoot := cmdUtils.GetServiceCreddVaultTokenPathRoot(serviceConfigPath)
	serviceConfig, err = worker.NewConfig(
		s,
		worker.SetCommandEnvironment(
			func(e *environment.CommandEnvironment) { e.SetKrb5ccname(krb5ccname, environment.DIR) },
			func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collectorHost) },
			func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) },
		),
		worker.SetAccount(viper.GetString(serviceConfigPath+".account")),
		worker.SetSchedds(schedds),
		worker.SetVaultServer(vaultServer),
		worker.SetUserPrincipal(userPrincipal),
		worker.SetKeytabPath(keytabPath),
		worker.SetServiceCreddVaultTokenPathRoot(serviceCreddVaultTokenPathRoot),
	)
	if err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Could not create config for service")
		return err
	}

	// 1. Get Kerberos ticket
	span.AddEvent("Start get kerberos ticket")
	// Channel, context, and worker for getting kerberos ticket
	var kerberosContext context.Context
	if kerberosTimeout, ok := timeouts["kerberos"]; ok {
		kerberosContext = utils.ContextWithOverrideTimeout(ctx, kerberosTimeout)
	} else {
		kerberosContext = ctx
	}
	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	if err := worker.GetKerberosTicketandVerify(kerberosContext, serviceConfig); err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Could not obtain kerberos ticket.  Stopping onboarding")
		return err
	}
	span.AddEvent("End get kerberos ticket")

	// 2.  Get and store vault tokens for service
	span.AddEvent("Start store vault tokens")
	var vaultStorerContext context.Context
	if vaultStorerTimeout, ok := timeouts["vaultstorer"]; ok {
		vaultStorerContext = utils.ContextWithOverrideTimeout(ctx, vaultStorerTimeout)
	} else {
		vaultStorerContext = ctx
	}

	defer func() {
		if err := vaultToken.RemoveServiceVaultTokens(viper.GetString("service")); err != nil {
			funcLogger.Error("Could not remove vault tokens for service.  Please clean up manually")
		}
	}()
	if err := worker.StoreAndGetRefreshAndVaultTokens(vaultStorerContext, serviceConfig); err != nil {
		tracing.LogErrorWithTrace(span, funcLogger, "Could not generate refresh tokens and store vault token for service")
		return err
	}
	span.AddEvent("End store vault tokens")

	tracing.LogSuccessWithTrace(span, funcLogger, "Successfully generated refresh token in vault. Onboarding complete")
	return nil
}
