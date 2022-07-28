package main

import (

	// "os/user"

	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/shreyb/managed-tokens/worker"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	// "github.com/rifflock/lfshook"
)

var experiment string

func init() {
	const configFile string = "managedTokens"
	// Defaults

	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Parse our command-line arguments
	pflag.StringP("experiment", "e", "", "Name of single experiment to push proxies")
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.BoolP("test", "t", false, "Test mode")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	// If no experiment is set, exit, since we only want to onboard a single experiment at a time
	if viper.GetString("experiment") == "" {
		log.Fatal("An experiment must be set with the -e flag for run-onboarding")
	} else {
		experiment = viper.GetString("experiment")
	}

	// Get config file

	// Check for override
	if viper.GetString("configfile") != "" {
		viper.SetConfigFile(viper.GetString("configfile"))
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
	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

	// Test flag sets which notifications section from config we want to use.
	if viper.GetBool("test") {
		log.Info("Running in test mode")
	}

	// TODO Take care of overrides:  keytabPath, condorCreddHost, condorCollectorHost, userPrincipalOverride
	// TODO should not run as root ALL executables

	// TODO Logfile setup

}

func main() {
	// TODO delete any generated vault token
	serviceConfigs := make([]*worker.ServiceConfig, 0)
	// Get servicename
	// Run condor_vault_storer worker, which passes cmd.out to tty
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

	// Get Kerberos tickets
	go worker.GetKerberosTicketsWorker(serviceConfigsForKinit, kerberosTicketsDone)

	experimentConfigPath := "experiments." + experiment
	roles := make([]string, 0, len(viper.GetStringMap(experimentConfigPath+".roles")))
	for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
		roles = append(roles, role)
	}

	func() {
		var setupWg sync.WaitGroup
		defer close(serviceConfigsForKinit)
		for _, role := range roles {
			// Setup the configs
			serviceConfigPath := experimentConfigPath + ".roles." + role
			setupWg.Add(1)
			go func(role, serviceConfigPath string) {
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

			}(role, serviceConfigPath)
		}
		setupWg.Wait()
	}()
	<-kerberosTicketsDone
	log.Debug("All kerberos tickets generated and verified")

	serviceConfigSuccess := make(map[string]bool)

	for _, serviceConfig := range serviceConfigs {
		serviceConfigSuccess[serviceConfig.Service] = false
		if err := worker.StoreAndGetRefreshAndVaultTokens(serviceConfig); err != nil {
			log.WithFields(log.Fields{
				"experiment": serviceConfig.Experiment,
				"role":       serviceConfig.Role,
			}).Error("Could not generate refresh tokens and store vault token for service")
		} else {
			serviceConfigSuccess[serviceConfig.Service] = true
		}
	}
	if err := cleanup(serviceConfigSuccess); err != nil {
		log.Fatalf("Error running cleanup: %w", err)
	}
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
