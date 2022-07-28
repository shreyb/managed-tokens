package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	// "os/user"

	// "github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	// "github.com/rifflock/lfshook"
	// "github.com/spf13/pflag"
	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"

	"github.com/shreyb/managed-tokens/worker"
)

func init() {
	// Get config file
	viper.SetConfigName("managedTokens")
	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Panicf("Fatal error reading in config file: %w", err)
	}
	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

	// TODO Take care of overrides:  keytabPath, desiredUid, condorCreddHost, condorCollectorHost, userPrincipalOverride
	// TODO should not run as root ALL executables

	// TODO Flags to override config

	// TODO Logfile setup

}

func main() {
	// TODO RPM should create /etc/managed-tokens, /var/lib/managed-tokens, /etc/cron.d/managed-tokens, /etc/logrotate.d/managed-tokens
	// TODO Go through all errors, and decide where we want to Error, Fatal, or perhaps return early
	// Check for executables
	// TODO Move this stuff to init function, or wherever is appropriate
	serviceConfigs := make([]*worker.ServiceConfig, 0)

	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}
	defer func() {
		os.RemoveAll(krb5ccname)
		log.Info("Cleared kerberos cache")
	}()

	// All my channels
	serviceConfigsForKinit := make(chan *worker.ServiceConfig)
	kerberosTicketsDone := make(chan struct{})
	serviceConfigsForCondor := make(chan *worker.ServiceConfig)
	condorDone := make(chan struct{})
	serviceConfigsForPush := make(chan *worker.ServiceConfig)
	pushDone := make(chan struct{})

	// Start workers up
	go worker.GetKerberosTicketsWorker(serviceConfigsForKinit, kerberosTicketsDone)
	go worker.StoreAndGetTokenWorker(serviceConfigsForCondor, condorDone)
	go worker.PushTokensWorker(serviceConfigsForPush, pushDone)

	// TODO Ping all nodes concurrently, receive status in notifications Manager, and don't start pushing
	// Tokens until all of those are done

	// Set up service configs
	experiments := make([]string, 0, len(viper.GetStringMap("experiments")))

	// Get experiments from config.
	// TODO Handle case where experiment is passed in
	// TODO Handle case where service can be passed in
	// TODO dryRun
	// TODO set up logger to always include experiment field
	// TODO Maybe put this in a second init function here (until chan wait)?  A lot of clutter

	for experiment := range viper.GetStringMap("experiments") {
		experiments = append(experiments, experiment)
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
					serviceConfigs = append(serviceConfigs, sc)
					serviceConfigsForKinit <- sc

				}(experiment, role, serviceConfigPath)
			}
		}
		setupWg.Wait()
	}()
	<-kerberosTicketsDone
	log.Debug("All kerberos tickets generated and verified")

	// Store tokens in vault and get short-lived vault token (condor_vault_storer)
	LoadServiceConfigsIntoChannel(serviceConfigsForCondor, serviceConfigs)

	// To avoid kerberos cache race conditions, condor_vault_storer must be run sequentially, so we'll wait until all are done
	// before transferring to nodes
	<-condorDone

	// Send to nodes
	LoadServiceConfigsIntoChannel(serviceConfigsForPush, serviceConfigs)
	<-pushDone

	fmt.Println("I guess we did something")

	// Notifications
	// Cleanup
}
