// cmdUtils provides utilities that are meant to be used by the various executables that the
// managed tokens library provides
package cmdUtils

import (
	"errors"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/google/shlex"
	condor "github.com/retzkek/htcondor-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/environment"
)

// Various sync.Onces to coordinate synchronization
var (
	logHtGettokenOptsOnce      sync.Once
	scheddStoreOnceByCollector map[string]*sync.Once
	// FUTURE TODO:  We might have to make this map store schedds by collector and VO
)

// globalSchedds stores the schedds for use by various goroutines.  The key of the map here is the collector
var globalSchedds map[string]*scheddCollection

func init() {
	globalSchedds = make(map[string]*scheddCollection)
	scheddStoreOnceByCollector = make(map[string]*sync.Once)
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
	collectorHost := GetCondorCollectorHostFromConfiguration(checkServiceConfigPath)
	funcLogger := log.WithField("collector", collectorHost)
	var once *sync.Once
	once, ok := scheddStoreOnceByCollector[collectorHost]
	if !ok {
		once = &sync.Once{}
		scheddStoreOnceByCollector[collectorHost] = once
	}
	var scheddLogSourceMsg string
	once.Do(func() {
		funcLogger.Debug("Querying collector for schedds")
		statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithArg("-schedd")

		if constraintKey, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorScheddConstraint"); viper.IsSet(constraintKey) {
			constraint := viper.GetString(constraintKey)
			funcLogger.WithField("constraint", constraint).Debug("Found constraint for condor collector query (condor_status)")
			statusCmd = statusCmd.WithConstraint(constraint)
		}

		funcLogger.WithField("command", statusCmd.Cmd().String()).Debug("Running condor_status to get cluster schedds")
		classads, err := statusCmd.Run()
		if err != nil {
			funcLogger.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")

		}

		for _, classad := range classads {
			name := classad["Name"].String()
			schedds = append(schedds, name)
		}
		globalSchedds[collectorHost] = newScheddCollection()
		globalSchedds[collectorHost].storeSchedds(schedds) // Store the schedds into the global store
		scheddLogSourceMsg = "collector"
	})

	// Return the globally-stored schedds from cache
	if len(schedds) == 0 {
		schedds = globalSchedds[collectorHost].getSchedds()
		scheddLogSourceMsg = "cache"
	}
	funcLogger.WithField("schedds", schedds).Debugf("Set schedds successfully from %s", scheddLogSourceMsg)
	return schedds
}

// GetVaultServer queries various sources to get the correct vault server or SEC_CREDENTIAL_GETTOKEN_OPTS setting, which condor_vault_storer
// needs to store the refresh token in a vault server.  The order of precedence is:
//
// 1. Environment variable _condor_SEC_CREDENTIAL_GETTOKEN_OPTS
// 2. Configuration file for managed tokens
// 3. Condor configuration file SEC_CREDENTIAL_GETTOKEN_OPTS value
func GetVaultServer(checkServiceConfigPath string) (string, error) {
	// Check environment
	if val := os.Getenv(environment.CondorSecCredentialGettokenOpts.EnvVarKey()); val != "" {
		return parseVaultServerFromEnvSetting(val)
	}

	// Check config
	if vaultServerConfigKey, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "vaultServer"); viper.IsSet(vaultServerConfigKey) {
		return viper.GetString(vaultServerConfigKey), nil
	}

	// Then check condor
	if val, err := getSecCredentialGettokenOptsFromCondor(); err != nil {
		log.Error("Could not get SEC_CREDENTIAL_GETTOKEN_OPTS from HTCondor")
	} else {
		return parseVaultServerFromEnvSetting(val)
	}

	return "", errors.New("could not find setting for SEC_CREDENTIAL_GETTOKEN_OPTS in environment, configuration, or HTCondor")
}

// getSecCredentialGettokenOptsFromCondor checks the condor configuration for the SEC_CREDENTIAL_GETTOKEN_OPTS setting
// and if available, returns it
func getSecCredentialGettokenOptsFromCondor() (string, error) {
	condorVarName := environment.CondorSecCredentialGettokenOpts.EnvVarKey()
	varName := strings.TrimPrefix(condorVarName, "_condor_")

	cmd := exec.Command("condor_config_val", varName)
	out, err := cmd.Output()
	if err != nil {
		log.Errorf("Could not run condor_config_val to get SEC_CREDENTIAL_GETTOKEN_OPTS: %s", err)
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseVaultServerFromEnvSetting takes an environment setting meant for htgettoken to parse (for example "-a vaultserver.domain"),
// and returns the vaultServer from that setting (in the above example, "vaultserver.domain", nil would be returned)
func parseVaultServerFromEnvSetting(envSetting string) (string, error) {
	envSettingArgs, err := shlex.Split(envSetting)
	if err != nil {
		log.Errorf("Could not split environment setting according to shlex rules, %s", err)
		return "", err
	}

	envSettingFlagSet := pflag.NewFlagSet("envSetting", pflag.ContinueOnError)
	envSettingFlagSet.ParseErrorsWhitelist.UnknownFlags = true // We're ok with unknown flags - just skip them
	var vaultServerPtr *string = envSettingFlagSet.StringP("vaultserver", "a", "", "")
	envSettingFlagSet.Parse(envSettingArgs)

	noVaultServerErr := errors.New("no vault server was stored in the environment")

	if vaultServerPtr == nil {
		return "", noVaultServerErr
	}
	if *vaultServerPtr == "" {
		return "", noVaultServerErr
	}

	return *vaultServerPtr, nil
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
// If so, the full configuration path is returned, and the overridden bool is set to true.
// If not, the original key is returned, and the overridden bool is set to false
func GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, key string) (configPath string, overridden bool) {
	configPath = key
	overrideConfigPath := checkServiceConfigPath + "." + key + "Override"
	if viper.IsSet(overrideConfigPath) {
		return overrideConfigPath, true
	}
	return
}
