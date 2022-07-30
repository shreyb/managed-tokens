package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/rifflock/lfshook"
	"github.com/spf13/pflag"

	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
)

var (
	services       []service.Service
	serviceConfigs = make(map[string]*worker.ServiceConfig)
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
	if viper.GetString("configfile") != "" {
		viper.SetConfigFile(viper.GetString("configfile"))
	} else {
		viper.SetConfigName(configFile)
	}

	viper.SetConfigName(configFile)
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
			service, err := service.NewService(serviceName)
			if err != nil {
				log.WithField(
					"service",
					viper.GetString("service"),
				).Fatal("Could not parse service properly.  Please ensure that the service follows the format laid out in the help text.")
			}
			services = append(services, service)
		}
	case viper.GetString("service") != "":
		// Running on a single service
		service, err := service.NewService(viper.GetString("service"))
		if err != nil {
			log.WithField(
				"service",
				viper.GetString("service"),
			).Fatal("Could not parse service properly.  Please ensure that the service follows the format laid out in the help text.")
		}
		services = append(services, service)
	default:
		// Running on every configured experiment and role
		for experiment := range viper.GetStringMap("experiments") {
			experimentConfigPath := "experiments." + experiment
			for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
				// Setup the configs
				serviceName := experiment + "_" + role
				service, err := service.NewService(serviceName)
				if err != nil {
					log.WithField(
						"service",
						viper.GetString("service"),
					).Fatal("Could not parse service properly.  Please ensure that the service follows the format laid out in the help text.")
				}
				services = append(services, service)
			}
		}
	}
}

func main() {
	// TODO RPM should create /etc/managed-tokens, /var/lib/managed-tokens, /etc/cron.d/managed-tokens, /etc/logrotate.d/managed-tokens
	// TODO Go through all errors, and decide where we want to Error, Fatal, or perhaps return early
	successfulServices := make(map[string]bool)

	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}

	defer func() {
		os.RemoveAll(krb5ccname)
		log.Info("Cleared kerberos cache")
	}()

	defer func(successfulServices map[string]bool) {
		if err := cleanup(successfulServices); err != nil {
			log.Fatal("Error cleaning up")
		}

	}(successfulServices)

	// Channels and worker for getting kerberos tickets
	kerberosChannels := worker.NewChannelsForWorkers(len(services))
	go worker.GetKerberosTicketsWorker(kerberosChannels)

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

				sc, err := worker.NewServiceConfig(
					s,
					serviceConfigViperPath(serviceConfigPath),
					setkrb5ccname(krb5ccname),
					setCondorCreddHost(serviceConfigPath),
					setCondorCollectorHost(serviceConfigPath),
					setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, s.Experiment()),
					setKeytabOverride(serviceConfigPath),
					setDesiredUIByOverrideOrLookup(serviceConfigPath),
					destinationNodes(serviceConfigPath),
					account(serviceConfigPath),
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

	// Store tokens in vault and get short-lived vault token (condor_vault_storer)

	// Channels and worker for getting/storing vault token
	// TODO Implement ChannelGroup for storeToken and pushToken workers
	condorVaultChans := worker.NewChannelsForWorkers(len(serviceConfigs))
	go worker.StoreAndGetTokenWorker(condorVaultChans)
	LoadServiceConfigsIntoChannel(condorVaultChans.GetServiceConfigChan(), serviceConfigs)

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

	// TODO Ping all nodes concurrently, receive status in notifications Manager, and don't start pushing
	// Tokens until all of those are done

	// Send to nodes

	// Channels and worker for pushing tokens
	pushChans := worker.NewChannelsForWorkers(len(serviceConfigs))
	go worker.PushTokensWorker(pushChans)
	LoadServiceConfigsIntoChannel(pushChans.GetServiceConfigChan(), serviceConfigs)

	// Aggregate the successes
	for pushSuccess := range pushChans.GetSuccessChan() {
		if pushSuccess.GetSuccess() {
			successfulServices[pushSuccess.GetServiceName()] = true
		}
	}

	fmt.Println("I guess we did something")

	// Notifications
}

func cleanup(successMap map[string]bool) error {
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
