package main

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

// var once sync.Once

// Custom usage function for positional argument.
func onboardingUsage() {
	fmt.Printf("Usage: %s [OPTIONS] service...\n", os.Args[0])
	fmt.Printf("service must be of the form 'experiment_role', e.g. 'dune_production'\n")
	pflag.PrintDefaults()
}

// Functional options for initialization of serviceConfigs
// TODO:  Split these so that we can define the functional options in the worker package, then handle the logic of actually figuring out the values here and plug
// them in.  For example, for setCondorCreddHost:

// func SetCondorCreddHost(condorCreddHost string) *worker.Config {
// 	return func(c *worker.Config) {
// 		c.CommandEnvironment.CondorCreddHost = condorCreddHost
// 	}
// }
//
// And then here,
//
// func getCondorCreddHostFromConfiguration(serviceConfigPath string) string {
// 		addString := "_condor_CREDD_HOST="
// 		overrideVar := serviceConfigPath + ".condorCreddHostOverride"
// 		if viper.IsSet(overrideVar) {
// 			addString = addString + viper.GetString(overrideVar)
// 		} else {
// 			addString = addString + viper.GetString("condorCreddHost")
// 		}
// 		return addString
// }

// This is a bit more code, but it's a bit clearer and more organized

// // getCondorCredHostFromConfiguration gets the _condor_CREDD_HOST environment variable from the Viper configuration
// func getCondorCreddHostFromConfiguration(serviceConfigPath string) string {
// 	overrideVar := serviceConfigPath + ".condorCreddHostOverride"
// 	if viper.IsSet(overrideVar) {
// 		return viper.GetString(overrideVar)
// 	}
// 	return viper.GetString("condorCreddHost")
// }

// // getCondorCollectorHostFromConfiguration gets the _condor_COLLECTOR_HOST environment variable from the Viper configuration
// func getCondorCollectorHostFromConfiguration(serviceConfigPath string) string {
// 	overrideVar := serviceConfigPath + ".condorCollectorHostOverride"
// 	if viper.IsSet(overrideVar) {
// 		return viper.GetString(overrideVar)
// 	}
// 	return viper.GetString("condorCollectorHost")
// }

// // getUserPrincipalFromConfiguration gets the configured kerberos principal
// func getUserPrincipalFromConfiguration(serviceConfigPath, experiment string) string {
// 	userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern"))
// 	if err != nil {
// 		log.Errorf("Error parsing Kerberos Principal Template, %s", err)
// 		return ""
// 	}
// 	userPrincipalOverrideConfigPath := serviceConfigPath + ".userPrincipalOverride"
// 	if viper.IsSet(userPrincipalOverrideConfigPath) {
// 		return viper.GetString(userPrincipalOverrideConfigPath)
// 	} else {
// 		var b strings.Builder
// 		templateArgs := struct{ Account string }{Account: viper.GetString(serviceConfigPath + ".account")}
// 		if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
// 			log.WithField("experiment", experiment).Error("Could not execute kerberos prinicpal template")
// 			return ""
// 		}
// 		return b.String()
// 	}
// }

// // getUserPrincipalAndHtgettokenoptsFromConfiguration gets a worker.Config's kerberos principal and with it, the value for the HTGETTOKENOPTS environment variable
// func getUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath, experiment string) (userPrincipal string, htgettokenOpts string) {
// 	getValueFromPointer := func(stringPtr *string) string {
// 		if stringPtr == nil {
// 			return ""
// 		}
// 		return *stringPtr
// 	}
// 	defer log.Debugf("Final HTGETTOKENOPTS: %s", getValueFromPointer(&htgettokenOpts))

// 	userPrincipal = getUserPrincipalFromConfiguration(serviceConfigPath, experiment)
// 	if userPrincipal == "" {
// 		log.WithField("caller", "setUserPrincipalAndHtgettokenopts").Error("User principal is blank.  Cannot determine credkey and thus HTGETTOKENOPTS.")
// 		return
// 	}

// 	credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")

// 	// Look for HTGETTOKKENOPTS in environment.  If it's given here, take as is, but add credkey if it's absent
// 	if viper.IsSet("ORIG_HTGETTOKENOPTS") {
// 		log.Debugf("Prior to running, HTGETTOKENOPTS was set to %s", viper.GetString("ORIG_HTGETTOKENOPTS"))
// 		// If we have the right credkey in the HTGETTOKENOPTS, leave it be
// 		if strings.Contains(viper.GetString("ORIG_HTGETTOKENOPTS"), credKey) {
// 			htgettokenOpts = viper.GetString("ORIG_HTGETTOKENOPTS")
// 			return
// 		} else {
// 			once.Do(
// 				func() {
// 					log.Warn("HTGETTOKENOPTS was provided in the environment and does not have the proper --credkey specified.  Will add it to the existing HTGETTOKENOPTS")
// 				},
// 			)
// 			htgettokenOpts = viper.GetString("ORIG_HTGETTOKENOPTS") + " --credkey=" + credKey
// 			return
// 		}
// 	}
// 	// Calculate minimum vault token lifetime from config
// 	var lifetimeString string
// 	defaultLifetimeString := "10s"
// 	if viper.IsSet("minTokenLifetime") {
// 		lifetimeString = viper.GetString("minTokenLifetime")
// 	} else {
// 		lifetimeString = defaultLifetimeString
// 	}

// 	htgettokenOpts = "--vaulttokenminttl=" + lifetimeString + " --credkey=" + credKey
// 	return
// }

// // getKeytabOverrideFromConfiguration checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// // If the override does not exist, it uses the configuration to calculate the default path to the keytab
// func getKeytabOverrideFromConfiguration(serviceConfigPath string) string {
// 	keytabConfigPath := serviceConfigPath + ".keytabPathOverride"
// 	if viper.IsSet(keytabConfigPath) {
// 		return viper.GetString(keytabConfigPath)
// 	}
// 	// Default keytab location
// 	keytabDir := viper.GetString("keytabPath")
// 	return path.Join(
// 		keytabDir,
// 		fmt.Sprintf(
// 			"%s.keytab",
// 			viper.GetString(serviceConfigPath+".account"),
// 		),
// 	)
// }

// // getScheddsFromConfiguration gets the schedd names that match the configured constraint by querying the condor collector.  It can be overridden
// // by setting the serviceConfigPath's condorCreddHostOverride field, in which case that value will be set as the schedd
// func getScheddsFromConfiguration(serviceConfigPath string) []string {
// 	schedds := make([]string, 0)

// 	// If condorCreddHostOverride is set, set the schedd slice to that
// 	creddOverrideVar := serviceConfigPath + ".condorCreddHostOverride"
// 	if viper.IsSet(creddOverrideVar) {
// 		schedds = append(schedds, viper.GetString(creddOverrideVar))
// 		return schedds
// 	}

// 	// Otherwise, run condor_status to get schedds.
// 	var collectorHost, constraint string
// 	if c := viper.GetString(serviceConfigPath + ".condorCollectorHostOverride"); c != "" {
// 		collectorHost = c
// 	} else if c := viper.GetString("condorCollectorHost"); c != "" {
// 		collectorHost = c
// 	}
// 	if c := viper.GetString(serviceConfigPath + ".condorScheddConstraintOverride"); c != "" {
// 		constraint = c
// 	} else if c := viper.GetString("condorScheddConstraint"); c != "" {
// 		constraint = c
// 	}

// 	statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithConstraint(constraint).WithArg("-schedd")
// 	classads, err := statusCmd.Run()
// 	if err != nil {
// 		log.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")

// 	}

// 	for _, classad := range classads {
// 		name := classad["Name"].String()
// 		schedds = append(schedds, name)
// 	}

// 	log.WithField("schedds", schedds).Debug("Set schedds successfully")
// 	return schedds
// }

// // // getAccountFromConfiguration sets the account field in the worker.Config object
// // func getAccountFromConfiguration(serviceConfigPath string) string {
// // 		return viper.GetString(serviceConfigPath + ".account")
// // }

// // Functions to set environment.CommandEnvironment inside worker.Config
// // setkrb5ccname returns a function that sets the KRB5CCNAME directory environment variable in an environment.CommandEnvironment
// func setkrb5ccnameInCommandEnvironment(krb5ccname string) func(*environment.CommandEnvironment) {
// 	return func(e *environment.CommandEnvironment) { e.SetKrb5CCName(krb5ccname, environment.DIR) }
// }

// func setCondorCollectorHostInCommandEnvironment(collector string) func(*environment.CommandEnvironment) {
// 	return func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collector) }
// }

// func SetHtgettokenOptsInCommandEnvironment(htgettokenopts string) func(*environment.CommandEnvironment) {
// 	return func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) }
// }
