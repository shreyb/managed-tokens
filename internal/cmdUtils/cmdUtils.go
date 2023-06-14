// cmdUtils provides utilities that are meant to be used by the various executables that the
// managed tokens library provides
package cmdUtils

import (
	"fmt"
	"html/template"
	"path"
	"strings"
	"sync"

	condor "github.com/retzkek/htcondor-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/environment"
)

var once sync.Once

// Functional options for initialization of service Config

// TODO Unit test this whole package

// GetCondorCollectorHostFromConfiguration gets the _condor_COLLECTOR_HOST environment variable from the Viper configuration
func GetCondorCollectorHostFromConfiguration(serviceConfigPath string) string {
	condorCollectorHostPath, _ := GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "condorCollectorHost")
	return viper.GetString(condorCollectorHostPath)
}

// GetUserPrincipalFromConfiguration gets the configured kerberos principal
func GetUserPrincipalFromConfiguration(serviceConfigPath, experiment string) string {
	if userPrincipalOverrideConfigPath, ok := GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "userPrincipal"); ok {
		return viper.GetString(userPrincipalOverrideConfigPath)
	} else {
		var b strings.Builder
		kerberosPrincipalPattern, _ := GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "kerberosPrincipalPattern")
		userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString(kerberosPrincipalPattern))
		if err != nil {
			log.Errorf("Error parsing Kerberos Principal Template, %s", err)
			return ""
		}
		account := viper.GetString(serviceConfigPath + ".account")
		templateArgs := struct{ Account string }{Account: account}
		if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
			log.WithField("account", account).Error("Could not execute kerberos prinicpal template")
			return ""
		}
		return b.String()
	}
}

// GetUserPrincipalAndHtgettokenoptsFromConfiguration gets a worker.Config's kerberos principal and with it, the value for the HTGETTOKENOPTS environment variable
func GetUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath, experiment string) (userPrincipal string, htgettokenOpts string) {
	getValueFromPointer := func(stringPtr *string) string {
		if stringPtr == nil {
			return ""
		}
		return *stringPtr
	}
	// TODO Get this working
	defer log.Debugf("Final HTGETTOKENOPTS: %s", getValueFromPointer(&htgettokenOpts))

	userPrincipal = GetUserPrincipalFromConfiguration(serviceConfigPath, experiment)
	if userPrincipal == "" {
		log.WithField("caller", "setUserPrincipalAndHtgettokenopts").Error("User principal is blank.  Cannot determine credkey and thus HTGETTOKENOPTS.")
		return
	}

	credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")

	// Look for HTGETTOKKENOPTS in environment.  If it's given here, take as is, but add credkey if it's absent
	if viper.IsSet("ORIG_HTGETTOKENOPTS") {
		log.Debugf("Prior to running, HTGETTOKENOPTS was set to %s", viper.GetString("ORIG_HTGETTOKENOPTS"))
		// If we have the right credkey in the HTGETTOKENOPTS, leave it be
		if strings.Contains(viper.GetString("ORIG_HTGETTOKENOPTS"), credKey) {
			htgettokenOpts = viper.GetString("ORIG_HTGETTOKENOPTS")
			return
		} else {
			once.Do(
				func() {
					log.Warn("HTGETTOKENOPTS was provided in the environment and does not have the proper --credkey specified.  Will add it to the existing HTGETTOKENOPTS")
				},
			)
			htgettokenOpts = viper.GetString("ORIG_HTGETTOKENOPTS") + " --credkey=" + credKey
			return
		}
	}
	// Calculate minimum vault token lifetime from config
	var lifetimeString string
	defaultLifetimeString := "10s"
	if viper.IsSet("minTokenLifetime") {
		lifetimeString = viper.GetString("minTokenLifetime")
	} else {
		lifetimeString = defaultLifetimeString
	}

	htgettokenOpts = "--vaulttokenminttl=" + lifetimeString + " --credkey=" + credKey
	return
}

// GetKeytabOverrideFromConfiguration checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab
func GetKeytabOverrideFromConfiguration(serviceConfigPath string) string {
	if keytabConfigPath, ok := GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "keytabPath"); ok {
		return viper.GetString(keytabConfigPath)
	} else {
		// Default keytab location
		return path.Join(
			viper.GetString(keytabConfigPath),
			fmt.Sprintf(
				"%s.keytab",
				viper.GetString(serviceConfigPath+".account"),
			),
		)
	}
}

// getScheddsFromConfiguration gets the schedd names that match the configured constraint by querying the condor collector.  It can be overridden
// by setting the serviceConfigPath's condorCreddHostOverride field, in which case that value will be set as the schedd
func GetScheddsFromConfiguration(serviceConfigPath string) []string {
	schedds := make([]string, 0)

	// If condorCreddHostOverride is set, set the schedd slice to that
	if creddOverrideVar, ok := GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, "condorCreddHost"); ok {
		schedds = append(schedds, viper.GetString(creddOverrideVar))
		return schedds
	}

	// Otherwise, run condor_status to get schedds.
	var collectorHost, constraint string
	if c := viper.GetString(serviceConfigPath + ".condorCollectorHostOverride"); c != "" {
		collectorHost = c
	} else if c := viper.GetString("condorCollectorHost"); c != "" {
		collectorHost = c
	}
	if c := viper.GetString(serviceConfigPath + ".condorScheddConstraintOverride"); c != "" {
		constraint = c
	} else if c := viper.GetString("condorScheddConstraint"); c != "" {
		constraint = c
	}

	statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithConstraint(constraint).WithArg("-schedd")
	classads, err := statusCmd.Run()
	if err != nil {
		log.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")

	}

	for _, classad := range classads {
		name := classad["Name"].String()
		schedds = append(schedds, name)
	}

	log.WithField("schedds", schedds).Debug("Set schedds successfully")
	return schedds
}

// Functions to set environment.CommandEnvironment inside worker.Config
// setkrb5ccname returns a function that sets the KRB5CCNAME directory environment variable in an environment.CommandEnvironment
func Setkrb5ccnameInCommandEnvironment(krb5ccname string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetKrb5ccname(krb5ccname, environment.DIR) }
}

func SetCondorCollectorHostInCommandEnvironment(collector string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collector) }
}

func SetHtgettokenOptsInCommandEnvironment(htgettokenopts string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) }
}

// Utility functions

// GetServiceConfigOverrideKeyOrGlobalKey checks to see if key + "Override" is defined at the serviceConfigPath in the configuration.
// If so, the full configuration path is returned, and the overriden bool is set to true.
// If not, the original key is returned, and the overridden bool is set to false
func GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, key string) (configPath string, overridden bool) {
	configPath = key
	overrideConfigPath := serviceConfigPath + "." + key + "Override"
	if viper.IsSet(overrideConfigPath) {
		return overrideConfigPath, true
	}
	return
}
