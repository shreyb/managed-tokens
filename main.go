package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strings"

	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	// "github.com/rifflock/lfshook"
	// "github.com/spf13/pflag"
	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"
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
	// var wg sync.WaitGroup

	// Check for executables
	// TODO Move this stuff to init function, or wherever is appropriate
	// Check for condor_store_cred executable
	if _, err := exec.LookPath("condor_store_cred"); err != nil {
		log.Warn("Could not find condor_store_cred.  Adding /usr/sbin to $PATH")
		os.Setenv("PATH", "/usr/sbin:$PATH")
	}

	// Get kerberos keytab and verify
	var kerberosExecutables = map[string]string{
		"kinit":    "",
		"klist":    "",
		"kdestroy": "",
	}

	for kExe := range kerberosExecutables {
		kPath, err := exec.LookPath(kExe)
		if err != nil {
			log.Errorf("Could not find executable %s.  Please ensure it exists on your system", kExe)
			os.Exit(1)
		}
		kerberosExecutables[kExe] = kPath
		log.Infof("Using %s executable: %s", kExe, kPath)

	}

	// Experiment-specific stuff
	experiments := make([]string, 0, len(viper.GetStringMap("experiments")))

	// Get experiments from config.
	// TODO Handle case where experiment is passed in
	// TODO set up logger to always include experiment field
	for experiment := range viper.GetStringMap("experiments") {
		experiments = append(experiments, experiment)
	}

	for _, experiment := range experiments {
		// Setup
		var keytabPath, userPrincipal string
		experimentConfigPath := "experiments." + experiment
		roles := make([]string, 0, len(viper.GetStringMap(experimentConfigPath+".roles")))
		for role := range viper.GetStringMap(experimentConfigPath + ".roles") {
			roles = append(roles, role)
		}

		for _, role := range roles {
			// TODO IMPORTANT:  This fails under concurrency because maps are not concurrnent safe See commandEnvironment.  Need to use a Sync Map, or better yet, a struct
			// wg.Add(1)
			experimentRoleConfigPath := experimentConfigPath + ".roles." + role

			// TODO make all these functional options
			experimentCommandEnvironment := new(commandEnvironment)
			// Krb5ccname
			krb5ccname, err := ioutil.TempFile("", fmt.Sprintf("managed-tokens-%s", experiment))
			if err != nil {
				log.WithField("experiment", experiment).Fatal("Cannot create temporary file for kerberos cache.  This will cause a fatal race condition.  Exiting")
			}
			experimentCommandEnvironment.krb5ccname = "KRB5CCNAME=FILE:" + krb5ccname.Name()
			// experimentCommandEnvironment.krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname.Name()

			// CREDD_HOST and COLLECTOR_HOST overrides
			// TODO make this a function or a functional option
			addString := "_condor_CREDD_HOST="
			overrideVar := experimentRoleConfigPath + ".condorCreddHostOverride"
			if viper.IsSet(overrideVar) {
				addString = addString + viper.GetString(overrideVar)
				experimentCommandEnvironment.condorCreddHost = addString
			} else {
				addString = addString + viper.GetString("condorCreddHost")
				experimentCommandEnvironment.condorCreddHost = addString
			}
			addString = "_condor_COLLECTOR_HOST="
			overrideVar = experimentRoleConfigPath + ".condorCollectorHostOverride"
			if viper.IsSet(overrideVar) {
				addString = addString + viper.GetString(overrideVar)
				experimentCommandEnvironment.condorCollectorHost = addString
			} else {
				addString = addString + viper.GetString("condorCollectorHost")
				experimentCommandEnvironment.condorCollectorHost = addString
			}

			// User Principal override
			userPrincipalTemplate := template.Must(template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern"))) // TODO Maybe move this out so it's not evaluated every experiment
			userPrincipalOverrideConfigPath := experimentRoleConfigPath + ".userPrincipalOverride"
			if viper.IsSet(userPrincipalOverrideConfigPath) {
				userPrincipal = viper.GetString(userPrincipalOverrideConfigPath)
			} else {
				var b strings.Builder
				templateArgs := struct{ Account string }{Account: viper.GetString(experimentRoleConfigPath + ".account")}
				if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
					log.WithField("experiment", experiment).Fatal("Could not execute kerberos prinicpal template")
				}
				userPrincipal = b.String()
			}

			// HTGETTOKENOPTS
			credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")
			htgettokenOptsRaw := []string{
				fmt.Sprintf("--credkey=%s", credKey),
			}
			experimentCommandEnvironment.htgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""

			func(experimentCommandEnvironment *commandEnvironment) {
				// go func(experimentCommandEnvironment *commandEnvironment) {
				// defer wg.Done()
				defer os.Remove(krb5ccname.Name())

				// TODO maybe make this a type/struct?

				// Keytab override
				keytabConfigPath := experimentRoleConfigPath + ".keytabPath"
				if viper.IsSet(keytabConfigPath) {
					keytabPath = viper.GetString(keytabConfigPath)
				} else {
					// Default keytab location
					keytabDir := viper.GetString("keytabPath")
					keytabPath = path.Join(
						keytabDir,
						fmt.Sprintf(
							"%s.keytab",
							viper.GetString(experimentRoleConfigPath+".account"),
						),
					)
				}

				// Desired UID lookup/override
				var desiredUID uint32
				if viper.IsSet(experimentRoleConfigPath + ".desiredUIDOverride") {
					desiredUID = viper.GetUint32(experimentRoleConfigPath + ".desiredUIDOverride")
				} else {
					// FERRY STUFF
					// TODO The desired UIDs should come from FERRY.  Need to write a service that writes out a config file in /tmp, loads it in setup, and checks that config right now.  SQLite DB?
				}

				// Setup done

				// Get kerberos ticket, check principal
				principalCheckRegexp := regexp.MustCompile("Default principal: (.+)")

				// Lock
				createKerberosTicket := exec.Command(kerberosExecutables["kinit"], "-k", "-t", keytabPath, userPrincipal)
				createKerberosTicket = appendtoEnv(createKerberosTicket, experimentCommandEnvironment, "krb5ccname")
				log.Info("Now creating new kerberos ticket with keytab")
				if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
					log.Fatalf("%s", stdoutstdErr)
				}

				// Lock
				checkForKerberosTicket := exec.Command(kerberosExecutables["klist"])
				checkForKerberosTicket = appendtoEnv(checkForKerberosTicket, experimentCommandEnvironment, "krb5ccname")
				log.Info("Checking user principal against configured principal")
				if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
					log.Fatal(err)
				} else {
					log.Infof("%s", stdoutStderr)
					matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
					if len(matches) != 2 {
						log.Fatal("Could not find principal in kinit output")
					}
					principal := string(matches[1])
					log.Infof("Found principal: %s", principal)
					if principal != userPrincipal {
						log.Fatal("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", userPrincipal, principal)
					}

				}

				// Store token in vault and get new vault token
				condorVaultStorerExe, err := exec.LookPath("condor_vault_storer")
				if err != nil {
					log.Fatal("Could not find path to condor_vault_storer executable")
				}

				service := experiment + "_" + role

				//TODO if verbose, add the -v flag here
				// Lock
				getTokensAndStoreInVaultCmd := exec.Command(condorVaultStorerExe, service)
				getTokensAndStoreInVaultCmd = appendtoEnv(getTokensAndStoreInVaultCmd, experimentCommandEnvironment)
				// if err := getTokensAndStoreInVaultCmd.Run(); err != nil {
				if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
					log.Fatalf("%s", stdoutStderr)
				} else {
					log.Infof("%s", stdoutStderr)
				}

				// TODO Verify token scopes with scitokens lib

				// TODO Hopefully we won't need this bit with the current UID if I can get htgettoken to write out vault tokens to a random tempfile
				// TODO Delete the source file.  Like with a defer os.Remove or something like that
				currentUser, err := user.Current()
				if err != nil {
					log.Fatal(err)
				}
				currentUID := currentUser.Uid

				sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, service)
				destinationFilenames := []string{
					fmt.Sprintf("/tmp/vt_u%d", desiredUID),
					fmt.Sprintf("/tmp/vt_u%d-%s", desiredUID, service),
				}

				// Send to nodes

				// Import rsync.go (maybe a utils package?)
				for _, destinationNode := range viper.GetStringSlice(experimentRoleConfigPath + ".destinationNodes") {
					for _, destinationFilename := range destinationFilenames {
						rsyncConfig := utils.NewRsyncSetup(
							viper.GetString(experimentRoleConfigPath+".account"),
							destinationNode,
							destinationFilename,
							"",
						)

						if err := rsyncConfig.CopyToDestination(sourceFilename); err != nil {
							log.Errorf("Could not copy file %s to destination %s", sourceFilename, destinationFilename)
							log.Fatal(err)
						}
					}
				}
				log.WithFields(log.Fields{
					"experiment": experiment,
					"role":       role,
				}).Info("Success")
			}(experimentCommandEnvironment)
		}
	}
	// wg.Wait()
	fmt.Println("I guess we did something")
}

func appendtoEnv(cmd *exec.Cmd, environmentMap *commandEnvironment, keys ...string) *exec.Cmd {
	//TODO Maybe have all commands wrapped in this, where krbcc is set.  New type of Cmd with env set?
	toAdd := make([]string, 0)
	if len(keys) == 0 {
		// Add all keys to env
		//FIGURE THIS OUT TODO
	}

	toAdd = append(toAdd, environmentMap.krb5ccname)
	toAdd = append(toAdd, environmentMap.condorCollectorHost)
	toAdd = append(toAdd, environmentMap.condorCreddHost)
	toAdd = append(toAdd, environmentMap.htgettokenOpts)

	cmd.Env = append(
		os.Environ(),
		toAdd...,
	)
	return cmd
}

// Maybe consider having commandEnvironment implement Ranger

type commandEnvironment struct {
	krb5ccname          string
	condorCreddHost     string
	condorCollectorHost string
	htgettokenOpts      string
}
