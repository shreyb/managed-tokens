package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/rifflock/lfshook"
	"github.com/spf13/pflag"

	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"

	"github.com/shreyb/managed-tokens/notifications"
	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
)

const globalTimeoutDefaultStr string = "300s"

var (
	services          []service.Service
	serviceConfigs    = make(map[string]*service.Config)
	timeouts          = make(map[string]time.Duration)
	supportedTimeouts = map[string]struct{}{
		"globaltimeout":      {},
		"kerberostimeout":    {},
		"vaultstorertimeout": {},
		"pingtimeout":        {},
		"pushtimeout":        {},
	}
	adminNotifications = make([]notifications.SendMessager, 0)
)

// Initial setup.  Read flags, find config file, setup logs
func init() {
	const configFile string = "managedTokens"

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.Fatal("Current user is root.  Please run this executable as a non-root user")
	}

	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Flags
	pflag.StringP("experiment", "e", "", "Name of single experiment to push tokens")
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.StringP("service", "s", "", "Service to obtain and push vault tokens for.  Must be of the form experiment_role, e.g. dune_production")
	pflag.BoolP("test", "t", false, "Test mode.  Obtain vault tokens but don't push them to nodes")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	// Get config file
	// Check for override
	if config := viper.GetString("configfile"); config != "" {
		viper.SetConfigFile(config)
	} else {
		viper.SetConfigName(configFile)
	}

	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Panicf("Fatal error reading in config file: %w", err)
	}

	// Set up logs
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

	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

}

// Prep admin notifications
func init() {
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
		"",
	)
	slackMessage := notifications.NewSlackMessage(viper.GetString(prefix + "slack_alerts_url"))
	adminNotifications = append(adminNotifications, email, slackMessage)
}

// Setup of timeouts, if they're set
func init() {
	// Save supported timeouts into timeouts map
	for timeoutKey, timeoutString := range viper.GetStringMapString("timeouts") {
		if _, ok := supportedTimeouts[timeoutKey]; ok {
			timeout, err := time.ParseDuration(timeoutString)
			if err != nil {
				log.WithField("timeoutKey", timeoutKey).Warn("Configured timeout not supported by this utility")
			}
			log.WithField(timeoutKey, timeoutString).Info("Configured timeout") // TODO Make a debug
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
		log.Fatal("Configured component timeouts exceed the total configured global timeout.  Please check all configured timeouts: ", timeouts)
	}
}

// Setup of services
func init() {
	// If experiment or service is passed in on command line, ONLY generate and push tokens for that experiment/service
	switch {
	case viper.GetString("experiment") != "":
		// Running on a single experiment and all its roles
		experimentConfigPath := "experiments." + viper.GetString("experiment")
		for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
			// Setup the configs
			serviceName := viper.GetString("experiment") + "_" + role
			service := service.NewService(serviceName)
			services = append(services, service)
		}
	case viper.GetString("service") != "":
		// Running on a single service
		service := service.NewService(viper.GetString("service"))
		services = append(services, service)
	default:
		// Running on every configured experiment and role
		for experiment := range viper.GetStringMap("experiments") {
			experimentConfigPath := "experiments." + experiment
			for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
				// Setup the configs
				serviceName := experiment + "_" + role
				service := service.NewService(serviceName)
				services = append(services, service)
			}
		}
	}
}

func main() {
	// The general order of operations is this:
	//
	// 1. Get kerberos tickets
	// 2. Get and store vault tokens
	// 3. Ping nodes to check their status
	// 4. Push vault tokens to nodes

	// TODO RPM should create /etc/managed-tokens, /var/lib/managed-tokens, /etc/cron.d/managed-tokens, /etc/logrotate.d/managed-tokens
	// TODO Go through all errors, and decide where we want to Error, Fatal, or perhaps return early
	var globalTimeout time.Duration
	var notificationsWg sync.WaitGroup
	var ok bool
	var err error

	if globalTimeout, ok = timeouts["globaltimeout"]; !ok {
		log.Debugf("Global timeout not configured in config file.  Using default global timeout of %s", globalTimeoutDefaultStr)
		if globalTimeout, err = time.ParseDuration(globalTimeoutDefaultStr); err != nil {
			log.Fatal("Could not parse default global timeout.")
		}
	}

	// Global context
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	successfulServices := make(map[string]bool) // Map of services for which all processes were successful

	defer func(successfulServices map[string]bool) {
		if err := cleanup(ctx, successfulServices); err != nil {
			log.Fatal("Error cleaning up")
		}

	}(successfulServices)

	// Create temporary dir for all kerberos caches to live in
	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}

	defer func() {
		os.RemoveAll(krb5ccname)
		log.Info("Cleared kerberos cache")
	}()

	defer notificationsWg.Wait()

	// 1. Get kerberos tickets
	// Get channels and start worker for getting kerberos ticekts
	kerberosChannels := startServiceConfigWorkerForProcessing(ctx, worker.GetKerberosTicketsWorker, serviceConfigs, "kerberostimeout")

	// Set up our serviceConfigs and get kerberos tickets for each
	func() {
		defer close(kerberosChannels.GetServiceConfigChan())
		var serviceConfigSetupWg sync.WaitGroup
		for _, s := range services {
			// Setup the configs
			serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
			serviceConfigSetupWg.Add(1)
			go func(s service.Service, serviceConfigPath string) {
				defer serviceConfigSetupWg.Done()
				defer notificationsWg.Add(1)

				sc, err := service.NewConfig(
					s,
					serviceConfigViperPath(serviceConfigPath),
					setkrb5ccname(krb5ccname),
					setCondorCreddHost(serviceConfigPath),
					setCondorCollectorHost(serviceConfigPath),
					setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, s.Experiment()),
					setKeytabOverride(serviceConfigPath),
					setDesiredUIByOverrideOrLookup(ctx, serviceConfigPath),
					destinationNodes(serviceConfigPath),
					account(serviceConfigPath),
					setNotificationsChan(ctx, serviceConfigPath, s, &notificationsWg),
				)
				if err != nil {
					// Something more descriptive
					log.WithFields(log.Fields{
						"experiment": s.Experiment(),
						"role":       s.Role(),
					}).Fatal("Could not create config for service")
				}
				serviceConfigs[s.Name()] = sc
				successfulServices[s.Name()] = false
				kerberosChannels.GetServiceConfigChan() <- sc

			}(s, serviceConfigPath)
		}
		serviceConfigSetupWg.Wait()
	}()

	defer func() {
		for _, sc := range serviceConfigs {
			close(sc.NotificationsChan)
		}
	}()

	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	for kerberosTicketSuccess := range kerberosChannels.GetSuccessChan() {
		if !kerberosTicketSuccess.GetSuccess() {
			log.WithField(
				"service", kerberosTicketSuccess.GetServiceName(),
			).Error("Failed to obtain kerberos ticket.  Will not try to obtain or push vault token to service nodes")
			delete(serviceConfigs, kerberosTicketSuccess.GetServiceName())
		}
	}

	// 2. Get and store vault tokens
	// Get channels and start worker for getting and storing short-lived vault token (condor_vault_storer)
	condorVaultChans := startServiceConfigWorkerForProcessing(ctx, worker.StoreAndGetTokenWorker, serviceConfigs, "vaultstorertimeout")

	// To avoid kerberos cache race conditions, condor_vault_storer must be run sequentially, so we'll wait until all are done,
	// remove any service configs that we couldn't get tokens for from serviceConfigs, and then begin transferring to nodes
	for vaultStorerSuccess := range condorVaultChans.GetSuccessChan() {
		if !vaultStorerSuccess.GetSuccess() {
			log.WithField(
				"service", vaultStorerSuccess.GetServiceName(),
			).Info("Failed to obtain vault token.  Will not try to push vault token to service nodes")
			delete(serviceConfigs, vaultStorerSuccess.GetServiceName())
		}
	}

	if viper.GetBool("test") {
		log.Info("Test mode.  Cleaning up now")

		for service := range serviceConfigs {
			successfulServices[service] = true
		}
		return
	}

	// 3. Ping nodes to check their status
	// Get channels and start worker for pinging service nodes
	pingChans := startServiceConfigWorkerForProcessing(ctx, worker.PingAggregatorWorker, serviceConfigs, "pingtimeout")

	for pingSuccess := range pingChans.GetSuccessChan() {
		if !pingSuccess.GetSuccess() {
			msg := "Could not ping all nodes for service.  We'll still try to push tokens to all configured nodes, but there may be failures.  See logs for details"
			log.WithField("service", pingSuccess.GetServiceName()).Error(msg)
		}
	}

	// 4. Push vault tokens to nodes
	// Get channels and start worker for pushing tokens to service nodes
	pushChans := startServiceConfigWorkerForProcessing(ctx, worker.PushTokensWorker, serviceConfigs, "pushtimeout")

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[pushSuccess.GetServiceName()] = true
		}
	}

	fmt.Println("I guess we did something")

}

func cleanup(ctx context.Context, successMap map[string]bool) error {

	defer func() {
		if err := notifications.SendAdminNotifications(
			ctx,
			"token-push",
			viper.GetString("templates.adminerrors"),
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
		}
	}

	log.Infof("Successes: %s", strings.Join(successes, ", "))
	log.Infof("Failures: %s", strings.Join(failures, ", "))

	return nil
}
