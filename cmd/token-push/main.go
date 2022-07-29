package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	// "os/user"

	// "github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	// "github.com/rifflock/lfshook"
	"github.com/spf13/pflag"
	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"

	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
)

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
	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

	// TODO implement test flag behavior

	// TODO Logfile setup

}

func main() {
	// TODO RPM should create /etc/managed-tokens, /var/lib/managed-tokens, /etc/cron.d/managed-tokens, /etc/logrotate.d/managed-tokens
	// TODO Go through all errors, and decide where we want to Error, Fatal, or perhaps return early
	// Check for executables
	// TODO Move this stuff to init function, or wherever is appropriate
	//serviceConfigs := make([]*worker.ServiceConfig, 0)
	serviceConfigs := make(map[string]*worker.ServiceConfig)
	successfulServices := make(map[string]bool)

	defer func(successfulServices map[string]bool) {
		if err := cleanup(successfulServices); err != nil {
			log.Fatal("Error cleaning up")
		}

	}(successfulServices)

	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}
	defer func() {
		os.RemoveAll(krb5ccname)
		log.Info("Cleared kerberos cache")
	}()

	// Channels and worker for getting kerberos tickets
	serviceConfigsForKinit := make(chan *worker.ServiceConfig)
	kerberosTicketsDone := make(chan struct{})
	go worker.GetKerberosTicketsWorker(serviceConfigsForKinit, kerberosTicketsDone)

	// TODO Ping all nodes concurrently, receive status in notifications Manager, and don't start pushing
	// Tokens until all of those are done

	// Set up service configs
	experiments := make([]string, 0, len(viper.GetStringMap("experiments")))

	// Get experiments from config.
	// TODO Handle case where service can be passed in
	// TODO set up logger to always include experiment field
	// TODO Maybe put this in a second init function here (until chan wait)?  A lot of clutter

	// If experiment is passed in on command line, ONLY generate and push tokens for that experiment
	if exp := viper.GetString("experiment"); exp != "" {
		experiments = append(experiments, exp)
	} else {
		for experiment := range viper.GetStringMap("experiments") {
			experiments = append(experiments, experiment)
		}
	}

	// TODO - Try to move this to mainUtils and call from here?
	func() {
		var setupWg sync.WaitGroup
		defer close(serviceConfigsForKinit)
		for _, experiment := range experiments {
			// Setup
			// var keytabPath, userPrincipal string
			experimentConfigPath := "experiments." + experiment
			roles := make([]string, 0, len(viper.GetStringMap(experimentConfigPath+".roles")))
			for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
				roles = append(roles, role)
			}

			for _, role := range roles {
				// Setup the configs
				serviceConfigPath := experimentConfigPath + ".roles." + role
				setupWg.Add(1)
				go func(experiment, role, serviceConfigPath string) {
					defer setupWg.Done()

					// Functional options for service configs
					experimentOverride := func(sc *worker.ServiceConfig) error {
						if viper.IsSet("experiments." + experiment + "experimentNameOverride") {
							sc.Experiment = viper.GetString("experiments." + experiment + "experimentNameOverride")
						}
						return nil
					}

					serviceConfigViperPath := func(sc *worker.ServiceConfig) error {
						sc.ServiceConfigPath = serviceConfigPath
						return nil
					}

					// Krb5ccname
					setkrb5ccname := func(sc *worker.ServiceConfig) error {
						sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
						return nil
					}

					condorCreddHostFunc := setCondorCreddHost(serviceConfigPath)
					condorCollectorHostFunc := setCondorCollectorHost(serviceConfigPath)
					userPrincipalAndHtgettokenoptsFunc := setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, experiment)
					setKeytabFunc := setKeytabOverride(serviceConfigPath)
					setDesiredUIDFunc := setDesiredUIByOverrideOrLookup(serviceConfigPath)

					destinationNodes := func(sc *worker.ServiceConfig) error {
						sc.Nodes = viper.GetStringSlice(serviceConfigPath + ".destinationNodes")
						return nil
					}

					account := func(sc *worker.ServiceConfig) error {
						sc.Account = viper.GetString(serviceConfigPath + ".account")
						return nil
					}

					sc, err := worker.NewServiceConfig(
						experiment,
						role,
						experimentOverride,
						serviceConfigViperPath,
						setkrb5ccname,
						condorCreddHostFunc,
						condorCollectorHostFunc,
						userPrincipalAndHtgettokenoptsFunc,
						setKeytabFunc,
						setDesiredUIDFunc,
						destinationNodes,
						account,
					)
					if err != nil {
						// Something more descriptive
						log.WithFields(log.Fields{
							"experiment": experiment,
							"role":       role,
						}).Fatal("Could not create config for service")
					}
					serviceConfigs[sc.Service] = sc
					successfulServices[sc.Service] = false
					serviceConfigsForKinit <- sc

				}(experiment, role, serviceConfigPath)
			}
		}
		setupWg.Wait()
	}()
	<-kerberosTicketsDone
	log.Debug("All kerberos tickets generated and verified")

	// Store tokens in vault and get short-lived vault token (condor_vault_storer)

	// Channels and worker for getting/storing vault token
	serviceConfigsForCondor := make(chan *worker.ServiceConfig, len(serviceConfigs))
	condorDone := make(chan *worker.VaultStorerSuccess, len(serviceConfigs))
	go worker.StoreAndGetTokenWorker(serviceConfigsForCondor, condorDone)

	LoadServiceConfigsIntoChannel(serviceConfigsForCondor, serviceConfigs)

	// To avoid kerberos cache race conditions, condor_vault_storer must be run sequentially, so we'll wait until all are done,
	// remove any service configs that we couldn't get tokens for from serviceConfigs, and then begin transferring to nodes
	for vaultStorerSuccess := range condorDone {
		if !vaultStorerSuccess.Success {
			log.WithField(
				"service", vaultStorerSuccess.Service,
			).Info("Failed to obtain vault token.  Will not try to push vault token to service nodes")
			delete(serviceConfigs, vaultStorerSuccess.Service)
		}
	}

	if viper.GetBool("test") {
		log.Info("Test mode.  Cleaning up now")

		for service := range serviceConfigs {
			successfulServices[service] = true
		}
		return
	}

	// Send to nodes

	// Channels and worker for pushing tokens
	serviceConfigsForPush := make(chan *worker.ServiceConfig)
	pushDone := make(chan struct{})
	go worker.PushTokensWorker(serviceConfigsForPush, pushDone)

	LoadServiceConfigsIntoChannel(serviceConfigsForPush, serviceConfigs)
	<-pushDone

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
