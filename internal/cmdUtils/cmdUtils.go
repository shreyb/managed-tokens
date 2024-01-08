// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

	"github.com/fermitools/managed-tokens/internal/environment"
)

var (
	logHtGettokenOptsOnce sync.Once   // Only log our environment's HTGETTOKENOPTS once
	globalScheddCache     scheddCache // Global cache for the schedds, sorted by collector host
)

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
		kerberosPrincipalPattern, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "kerberosPrincipalPattern")
		userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString(kerberosPrincipalPattern))
		if err != nil {
			log.Errorf("Error parsing Kerberos Principal Template, %s", err)
			return ""
		}
		account := viper.GetString(checkServiceConfigPath + ".account")
		templateArgs := struct{ Account string }{Account: account}

		var b strings.Builder
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
		htgettokenOpts = resolveHtgettokenOptsFromConfig(credKey)
		return
	}

	// HTGETTOKENOPTS was not in the environment.  Use our defaults.
	// Calculate minimum vault token lifetime from config
	lifetimeString := getTokenLifetimeStringFromConfiguration()
	htgettokenOpts = "--vaulttokenminttl=" + lifetimeString + " --credkey=" + credKey

	return
}

// resolveHtgettokenOptsFromConfig checks the config for the "ORIG_HTGETTOKENOPTS" key.  If that is set, check the ORIG_HTGETTOKENOPTS value for the
// given credKey.  If the credKey is present, return the ORIG_HTGETTOKENOPTS value.  Otherwise, return the ORIG_HTGETTOKENOPTS value with the credKey
// appended
func resolveHtgettokenOptsFromConfig(credKey string) string {
	origHtgettokenOpts := viper.GetString("ORIG_HTGETTOKENOPTS")

	// ORIG_HTGETTOKENOPTS not set in config
	if origHtgettokenOpts == "" {
		return "--credkey=" + credKey
	}

	log.Debugf("Prior to running, HTGETTOKENOPTS was set to %s", origHtgettokenOpts)
	// If we have the right credkey in the HTGETTOKENOPTS, leave it be
	if strings.Contains(origHtgettokenOpts, credKey) {
		return origHtgettokenOpts
	}
	logHtGettokenOptsOnce.Do(
		func() {
			log.Warn("HTGETTOKENOPTS was provided in the environment and does not have the proper --credkey specified.  Will add it to the existing HTGETTOKENOPTS")
		},
	)
	htgettokenOpts := origHtgettokenOpts + " --credkey=" + credKey
	return htgettokenOpts
}

// getTokenLifetimeStringFromConfiguration checks the configuration for the "minTokenLifetime" key.  If it is set, the value is returned.  Otherwise,
// a default is returned.
func getTokenLifetimeStringFromConfiguration() string {
	defaultLifetimeString := "10s"
	if viper.IsSet("minTokenLifetime") {
		return viper.GetString("minTokenLifetime")
	}
	return defaultLifetimeString
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
func GetScheddsFromConfiguration(checkServiceConfigPath string) ([]string, error) {
	funcLogger := log.WithField("serviceConfigPath", checkServiceConfigPath)

	// 1. Try override
	// If condorCreddHostOverride is set either globally or at service level, set the schedd slice to that
	schedds, found := checkScheddsOverride(checkServiceConfigPath)
	if found {
		return schedds, nil
	}

	// 2.  Try globalScheddCache
	// See if we already have created a cacheEntry in the globalScheddCache for the collectorHost
	scheddSourceForLog := "cache"
	collectorHost := GetCondorCollectorHostFromConfiguration(checkServiceConfigPath)
	cacheEntry, _ := globalScheddCache.cache.LoadOrStore(
		collectorHost,
		&scheddCacheEntry{
			newScheddCollection(),
			&sync.Once{},
		},
	)

	// Now that we have our *scheddCacheEntry (either new or preexisting), if its *sync.Once has not been run, do so now to populate the entry.
	// If the Once has already been run, it will wait until the first Once has completed before resuming execution.
	// This way we are guaranteed that the cache will always be populated.
	var err error
	cacheEntryVal, ok := cacheEntry.(*scheddCacheEntry)
	if ok {
		cacheEntryVal.once.Do(
			func() {
				// 3.  Query collector
				// At this point, we haven't queried this collector yet.  Do so, and store its schedds in the global store/cache
				scheddSourceForLog = "collector"
				constraint := getConstraintFromConfiguration(checkServiceConfigPath)
				err = cacheEntryVal.populateFromCollector(collectorHost, constraint)
			},
		)
		if err != nil {
			return nil, err
		}
	}

	// Load schedds from cache, which we either just populated, or are only reading from; then return those schedds
	schedds = cacheEntryVal.scheddCollection.getSchedds()
	funcLogger.WithFields(log.Fields{
		"schedds":       schedds,
		"collectorHost": collectorHost,
	}).Debugf("Set schedds successfully from %s", scheddSourceForLog)
	return schedds, nil
}

// checkScheddsOverride checks the global and service-level configurations for the condorCreddHost key.  If that key exists, the value
// is returned, along with a bool indicating that the key was found in the configuration.
func checkScheddsOverride(checkServiceConfigPath string) (schedds []string, found bool) {
	creddOverrideVar, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorCreddHost")
	if viper.IsSet(creddOverrideVar) {
		schedds = append(schedds, viper.GetString(creddOverrideVar))
		log.WithFields(log.Fields{
			"serviceConfigPath": checkServiceConfigPath,
			"schedds":           schedds,
		}).Debugf("Set schedds successfully from override")
		return schedds, true
	}
	return
}

// getConstraintFromConfiguration checks the configuration at the checkServiceConfigPath for an override for the path to a condor constraint
// If the override does not exist, it returns the globally-configured condor constraint.
func getConstraintFromConfiguration(checkServiceConfigPath string) string {
	var constraint string
	constraintKey, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorScheddConstraint")
	if viper.IsSet(constraintKey) {
		constraint = viper.GetString(constraintKey)
		log.WithField("constraint", constraint).Debug("Found constraint for condor collector query (condor_status)")
	}
	return constraint
}

// getScheddsFromCondor queries the condor collector for the schedds in the cluster that satisfy the constraint
func getScheddsFromCondor(collectorHost, constraint string) ([]string, error) {
	funcLogger := log.WithField("collector", collectorHost)

	funcLogger.Debug("Querying collector for schedds")
	statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithArg("-schedd")
	if constraint != "" {
		statusCmd = statusCmd.WithConstraint(constraint)
	}

	funcLogger.WithField("command", statusCmd.Cmd().String()).Debug("Running condor_status to get cluster schedds")
	classads, err := statusCmd.Run()
	if err != nil {
		funcLogger.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")
		return nil, err
	}

	schedds := make([]string, 0)
	for _, classad := range classads {
		name := classad["Name"].String()
		schedds = append(schedds, name)
	}
	return schedds, nil
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

// GetKeytabFromConfiguration checks the configuration at the checkServiceConfigPath for an override for the path to the directory
// where the condorVaultStorer worker should look for and store service/credd-specific vault tokens.  If the override does not exist,
// it uses the configuration to calculate the default path to the relevant directory
func GetServiceCreddVaultTokenPathRoot(checkServiceConfigPath string) string {
	serviceCreddVaultTokenPathRootPath, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "serviceCreddVaultTokenPathRoot")
	return viper.GetString(serviceCreddVaultTokenPathRootPath)
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
