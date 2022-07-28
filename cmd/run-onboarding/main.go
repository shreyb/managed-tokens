package main

import (

	// "os/user"

	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/shreyb/managed-tokens/worker"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	// "github.com/rifflock/lfshook"
)

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

	experimentConfigPath := "experiments." + viper.GetString("experiment")
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
			go func(experiment, role string) {
				defer setupWg.Done()

				// Functional options for service configs
				//TODO Can I put these funcs ANYWHERE else?  It's really cluttering up the main() space
				serviceConfigViperPath := func(sc *worker.ServiceConfig) error {
					// TODO See if there's a way of getting around repetition
					sc.ServiceConfigPath = serviceConfigPath
					return nil
				}

				// TODO Experimentname override

				// Krb5ccname
				setkrb5ccname := func(sc *worker.ServiceConfig) error {
					sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
					return nil
				}

				// CREDD_HOST
				condorCreddHost := func(sc *worker.ServiceConfig) error {
					addString := "_condor_CREDD_HOST="
					overrideVar := serviceConfigPath + ".condorCreddHostOverride"
					if viper.IsSet(overrideVar) {
						addString = addString + viper.GetString(overrideVar)
					} else {
						addString = addString + viper.GetString("condorCreddHost")
					}
					sc.CommandEnvironment.CondorCreddHost = addString
					return nil
				}

				// COLLECTOR_HOST
				condorCollectorHost := func(sc *worker.ServiceConfig) error {
					addString := "_condor_COLLECTOR_HOST="
					overrideVar := serviceConfigPath + ".condorCollectorHostOverride"
					if viper.IsSet(overrideVar) {
						addString = addString + viper.GetString(overrideVar)
					} else {
						addString = addString + viper.GetString("condorCollectorHost")
					}
					sc.CommandEnvironment.CondorCollectorHost = addString
					return nil
				}

				// User Principal override and httokengetopts
				setUserPrincipalOverride := func(sc *worker.ServiceConfig) error {
					userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern")) // TODO Maybe move this out so it's not evaluated every experiment
					if err != nil {
						log.Error("Error parsing Kerberos Principal Template")
						log.Error(err)
						return err
					}
					userPrincipalOverrideConfigPath := serviceConfigPath + ".userPrincipalOverride"
					if viper.IsSet(userPrincipalOverrideConfigPath) {
						sc.UserPrincipal = viper.GetString(userPrincipalOverrideConfigPath)
					} else {
						var b strings.Builder
						templateArgs := struct{ Account string }{Account: viper.GetString(serviceConfigPath + ".account")}
						if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
							log.WithField("experiment", experiment).Error("Could not execute kerberos prinicpal template")
							return err
						}
						sc.UserPrincipal = b.String()
					}

					credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")
					// TODO Make htgettokenopts configurable
					htgettokenOptsRaw := []string{
						fmt.Sprintf("--credkey=%s", credKey),
					}
					sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""
					return nil
				}

				// Keytab override
				setKeytab := func(sc *worker.ServiceConfig) error {
					keytabConfigPath := serviceConfigPath + ".keytabPath"
					if viper.IsSet(keytabConfigPath) {
						sc.KeytabPath = viper.GetString(keytabConfigPath)
					} else {
						// Default keytab location
						keytabDir := viper.GetString("keytabPath")
						sc.KeytabPath = path.Join(
							keytabDir,
							fmt.Sprintf(
								"%s.keytab",
								viper.GetString(serviceConfigPath+".account"),
							),
						)
					}
					return nil
				}

				sc, err := worker.NewServiceConfig(
					experiment,
					role,
					serviceConfigViperPath,
					setkrb5ccname,
					condorCreddHost,
					condorCollectorHost,
					setUserPrincipalOverride,
					setKeytab,
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

			}(viper.GetString("experiment"), role)
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
