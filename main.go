package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"sync"

	// "os/user"
	"path"
	"strings"

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

	// TODO Flags to override config

	// TODO Logfile setup

}

func main() {
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

	// Start workers up
	go worker.GetKerberosTicketsWorker(serviceConfigsForKinit, kerberosTicketsDone)
	go worker.StoreAndGetTokenWorker(serviceConfigsForCondor, condorDone)

	// Set up service configs
	experiments := make([]string, 0, len(viper.GetStringMap("experiments")))

	// Get experiments from config.
	// TODO Handle case where experiment is passed in
	// TODO set up logger to always include experiment field
	// TODO Maybe put this in a second init function here (until chan wait)?  A lot of clutter

	for experiment := range viper.GetStringMap("experiments") {
		experiments = append(experiments, experiment)
	}

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
				// TODO IMPORTANT:  This fails under concurrency because maps are not concurrnent safe See commandEnvironment.  Need to use a Sync Map, or better yet, a struct
				// Setup the configs
				serviceConfigPath := experimentConfigPath + ".roles." + role
				setupWg.Add(1)
				go func(experiment, role string) {
					defer setupWg.Done()

					// Functional options for service configs
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

					// Desired UID lookup/override
					setDesiredUID := func(sc *worker.ServiceConfig) error {
						if viper.IsSet(serviceConfigPath + ".desiredUIDOverride") {
							sc.DesiredUID = viper.GetUint32(serviceConfigPath + ".desiredUIDOverride")
						} else {
							// FERRY STUFF
							// TODO The desired UIDs should come from FERRY.  Need to write a service that writes out a config file in /tmp, loads it in setup, and checks that config right now.  SQLite DB?
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
						setDesiredUID,
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

				}(experiment, role)
			}
		}
		setupWg.Wait()
	}()
	<-kerberosTicketsDone
	log.Debug("All kerberos tickets generated and verified")

	// Store tokens in vault and get short-lived vault token (condor_vault_storer)
	// Load serviceConfigs into channel for condor
	func() {
		defer close(serviceConfigsForCondor)
		for _, sc := range serviceConfigs {
			serviceConfigsForCondor <- sc
		}
	}()
	// To avoid kerberos cache race conditions, condor_vault_storer must be run sequentially, so we'll wait until all are done
	// before transferring to nodes
	<-condorDone
	fmt.Println("I guess we did something")
}

// LEFT OFF HERE.
// 	 	func(experimentCommandEnvironment *CommandEnvironment) {
// 	 		// go func(experimentCommandEnvironment *CommandEnvironment) {
// 	 		// defer wg.Done()
// 	 		// defer os.Remove(krb5ccname.Name())

// 	 		// TODO maybe make this a type/struct?

// 	 		// Setup done

// 	 		// Get kerberos ticket, check principal

// 	 		// Lock
// 	 		// createKerberosTicket := exec.Command(kerberosExecutables["kinit"], "-k", "-t", keytabPath, userPrincipal)
// 	 		// createKerberosTicket = appendtoEnv(createKerberosTicket, experimentCommandEnvironment, "krb5ccname")
// 	 		// log.Info("Now creating new kerberos ticket with keytab")
// 	 		// if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
// 	 		// 	log.Fatalf("%s", stdoutstdErr)
// 	 		// }

// 	 		// // Lock
// 	 		// checkForKerberosTicket := exec.Command(kerberosExecutables["klist"])
// 	 		// checkForKerberosTicket = appendtoEnv(checkForKerberosTicket, experimentCommandEnvironment, "krb5ccname")
// 	 		// log.Info("Checking user principal against configured principal")
// 	 		// if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
// 	 		// 	log.Fatal(err)
// 	 		// } else {
// 	 		// 	log.Infof("%s", stdoutStderr)
// 	 		// 	matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
// 	 		// 	if len(matches) != 2 {
// 	 		// 		log.Fatal("Could not find principal in kinit output")
// 	 		// 	}
// 	 		// 	principal := string(matches[1])
// 	 		// 	log.Infof("Found principal: %s", principal)
// 	 		// 	if principal != userPrincipal {
// 	 		// 		log.Fatal("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", userPrincipal, principal)
// 	 		// 	}

// 	 		// }

// 	 		// Store token in vault and get new vault token
// 	 		condorVaultStorerExe, err := exec.LookPath("condor_vault_storer")
// 	 		if err != nil {
// 	 			log.Fatal("Could not find path to condor_vault_storer executable")
// 	 		}

// 	 		service := experiment + "_" + role

// 	 		//TODO if verbose, add the -v flag here
// 	 		// Lock
// 	 		getTokensAndStoreInVaultCmd := exec.Command(condorVaultStorerExe, service)
// 	 		getTokensAndStoreInVaultCmd = appendtoEnv(getTokensAndStoreInVaultCmd, experimentCommandEnvironment)
// 	 		// if err := getTokensAndStoreInVaultCmd.Run(); err != nil {
// 	 		if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
// 	 			log.Fatalf("%s", stdoutStderr)
// 	 		} else {
// 	 			log.Infof("%s", stdoutStderr)
// 	 		}

// 	 		// TODO Verify token scopes with scitokens lib

// 	 		// TODO Hopefully we won't need this bit with the current UID if I can get htgettoken to write out vault tokens to a random tempfile
// 	 		// TODO Delete the source file.  Like with a defer os.Remove or something like that
// 	 		currentUser, err := user.Current()
// 	 		if err != nil {
// 	 			log.Fatal(err)
// 	 		}
// 	 		currentUID := currentUser.Uid

// 	 		sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, service)
// 	 		destinationFilenames := []string{
// 	 			fmt.Sprintf("/tmp/vt_u%d", desiredUID),
// 	 			fmt.Sprintf("/tmp/vt_u%d-%s", desiredUID, service),
// 	 		}

// 	 		// Send to nodes

// 	 		// Import rsync.go (maybe a utils package?)
// 	 		for _, destinationNode := range viper.GetStringSlice(experimentRoleConfigPath + ".destinationNodes") {
// 	 			for _, destinationFilename := range destinationFilenames {
// 	 				rsyncConfig := utils.NewRsyncSetup(
// 	 					viper.GetString(experimentRoleConfigPath+".account"),
// 	 					destinationNode,
// 	 					destinationFilename,
// 	 					"",
// 	 				)

// 	 				if err := rsyncConfig.CopyToDestination(sourceFilename); err != nil {
// 	 					log.Errorf("Could not copy file %s to destination %s", sourceFilename, destinationFilename)
// 	 					log.Fatal(err)
// 	 				}
// 	 			}
// 	 		}
// 	 		log.WithFields(log.Fields{
// 	 			"experiment": experiment,
// 	 			"role":       role,
// 	 		}).Info("Success")
// 	 	}(experimentCommandEnvironment)
// 	 	// wg.Wait()
// }

//  func appendtoEnv(cmd *exec.Cmd, environmentMap *CommandEnvironment, keys ...string) *exec.Cmd {
//  	//TODO Maybe have all commands wrapped in this, where krbcc is set.  New type of Cmd with env set?
//  	toAdd := make([]string, 0)
//  	if len(keys) == 0 {
//  		// Add all keys to env
//  		//FIGURE THIS OUT TODO
//  	}

//  	toAdd = append(toAdd, environmentMap.krb5ccname)
//  	toAdd = append(toAdd, environmentMap.condorCollectorHost)
//  	toAdd = append(toAdd, environmentMap.condorCreddHost)
//  	toAdd = append(toAdd, environmentMap.htgettokenOpts)

//  	cmd.Env = append(
//  		os.Environ(),
//  		toAdd...,
//  	)
//  	return cmd
//  }
