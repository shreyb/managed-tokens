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

// Various sync.Onces to coordinate synchronization
var (
	logHtGettokenOptsOnce sync.Once
	scheddStoreOnce       sync.Once
)

// globalSchedds stores the schedds for use by various goroutines
var globalSchedds *scheddCollection

func init() {
	globalSchedds = newScheddCollection()
}

// Functional options for initialization of service Config

// GetCondorCollectorHostFromConfiguration gets the _condor_COLLECTOR_HOST environment variable from the Viper configuration
func GetCondorCollectorHostFromConfiguration(checkServiceConfigPath string) string {
	condorCollectorHostPath, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorCollectorHost")
	return viper.GetString(condorCollectorHostPath)
}

// GetUserPrincipalFromConfiguration gets the configured kerberos principal
func GetUserPrincipalFromConfiguration(checkServiceConfigPath string) string {
	if userPrincipalOverrideConfigPath, ok := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "userPrincipal"); ok {
		return viper.GetString(userPrincipalOverrideConfigPath)
	} else {
		var b strings.Builder
		kerberosPrincipalPattern, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "kerberosPrincipalPattern")
		userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString(kerberosPrincipalPattern))
		if err != nil {
			log.Errorf("Error parsing Kerberos Principal Template, %s", err)
			return ""
		}
		account := viper.GetString(checkServiceConfigPath + ".account")
		templateArgs := struct{ Account string }{Account: account}
		if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
			log.WithField("account", account).Error("Could not execute kerberos prinicpal template")
			return ""
		}
		return b.String()
	}
}

// GetUserPrincipalAndHtgettokenoptsFromConfiguration gets a worker.Config's kerberos principal and with it, the value for the HTGETTOKENOPTS environment variable
func GetUserPrincipalAndHtgettokenoptsFromConfiguration(checkServiceConfigPath string) (userPrincipal string, htgettokenOpts string) {
	htgettokenOptsPtr := &htgettokenOpts
	defer func() {
		if htgettokenOptsPtr != nil {
			log.Debugf("Final HTGETTOKENOPTS: %s", *htgettokenOptsPtr)
		}
	}()

	userPrincipal = GetUserPrincipalFromConfiguration(checkServiceConfigPath)
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
			logHtGettokenOptsOnce.Do(
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

// GetKeytabFromConfiguration checks the configuration at the checkServiceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab
func GetKeytabFromConfiguration(checkServiceConfigPath string) string {
	if keytabConfigPath, ok := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "keytabPath"); ok {
		return viper.GetString(keytabConfigPath)
	} else {
		// Default keytab location
		return path.Join(
			viper.GetString(keytabConfigPath),
			fmt.Sprintf(
				"%s.keytab",
				viper.GetString(checkServiceConfigPath+".account"),
			),
		)
	}
}

// GetScheddsFromConfiguration gets the schedd names that match the configured constraint by querying the condor collector.  It can be overridden
// by setting the checkServiceConfigPath's condorCreddHostOverride field, in which case that value will be set as the schedd
func GetScheddsFromConfiguration(checkServiceConfigPath string) []string {
	schedds := make([]string, 0)

	// If condorCreddHostOverride is set either globally or at service level, set the schedd slice to that
	if creddOverrideVar, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorCreddHost"); viper.IsSet(creddOverrideVar) {
		schedds = append(schedds, viper.GetString(creddOverrideVar))
		log.WithField("schedds", schedds).Debug("Set schedds successfully from override")
		return schedds
	}

	// Otherwise, if we haven't run condor_status to get schedds, do that
	var scheddLogSourceMsg string
	scheddStoreOnce.Do(func() {
		log.Debug("Querying collector for schedds")
		collectorHost := GetCondorCollectorHostFromConfiguration(checkServiceConfigPath)
		statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithArg("-schedd")

		if constraintKey, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorScheddConstraint"); viper.IsSet(constraintKey) {
			constraint := viper.GetString(constraintKey)
			log.WithField("constraint", constraint).Debug("Found constraint for condor collector query (condor_status)")
			statusCmd = statusCmd.WithConstraint(constraint)
		}

		log.WithField("command", statusCmd.Cmd().String()).Debug("Running condor_status to get cluster schedds")
		classads, err := statusCmd.Run()
		if err != nil {
			log.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")

		}

		for _, classad := range classads {
			name := classad["Name"].String()
			schedds = append(schedds, name)
		}
		globalSchedds.storeSchedds(schedds) // Store the schedds into the global store
		scheddLogSourceMsg = "collector"
	})

	// Return the globally-stored schedds
	if len(schedds) == 0 {
		schedds = globalSchedds.getSchedds()
		scheddLogSourceMsg = "cache"
	}
	log.WithField("schedds", schedds).Debugf("Set schedds successfully from %s", scheddLogSourceMsg)
	return schedds
}

// Functions to set environment.CommandEnvironment inside worker.Config
// Setkrb5ccname returns a function that sets the KRB5CCNAME directory environment variable in an environment.CommandEnvironment
func Setkrb5ccnameInCommandEnvironment(krb5ccname string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetKrb5ccname(krb5ccname, environment.DIR) }
}

// SetCondorCollectorHostInCommandEnvironment returns a function that sets the _condor_COLLECTOR_HOST environment variable in an environment.CommandEnvironment
func SetCondorCollectorHostInCommandEnvironment(collector string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetCondorCollectorHost(collector) }
}

// SetHtgettokenOptsInCommandEnvironment returns a function that sets the HTGETTOKENOPTS environment variable in an environment.CommandEnvironment
func SetHtgettokenOptsInCommandEnvironment(htgettokenopts string) func(*environment.CommandEnvironment) {
	return func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) }
}

// Utility functions

// GetServiceConfigOverrideKeyOrGlobalKey checks to see if key + "Override" is defined at the checkServiceConfigPath in the configuration.
// If so, the full configuration path is returned, and the overriden bool is set to true.
// If not, the original key is returned, and the overridden bool is set to false
func GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, key string) (configPath string, overridden bool) {
	configPath = key
	overrideConfigPath := checkServiceConfigPath + "." + key + "Override"
	if viper.IsSet(overrideConfigPath) {
		return overrideConfigPath, true
	}
	return
}
