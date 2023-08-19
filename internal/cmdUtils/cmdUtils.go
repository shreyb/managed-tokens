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
	logHtGettokenOptsOnce sync.Once
	// Map of Mutexes per collector to ensure that only one goroutine will attempt to write schedds to globalSchedds for a given collector at a time
	// Without this extra set of mutexes, since all writers act before all readers for a sync.Map, multiple goroutines will attempt to write to globalSchedds.
	// While this isn't a data race, it's inefficient.
	collectorMutexes sync.Map
	globalSchedds    sync.Map // globalSchedds stores the schedds for use by various goroutines.  The key of the map here is the collector
	// FUTURE TODO:  We might have to make this map store schedds by collector and VO
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
//
// A note on the mutexes.  Originally, this func was implemented by using a map of *sync.Once objects, where each Once was called to query the
// condor collector.  This works for concurrent access, but the correct way to make it work puts the collector-calling code BEFORE the cache
// reading code, which I think is confusing for the reader.  The fallthrough logic to obtain the schedds is really
// override --> cache --> condor collector, and having the condor collector-querying code appear before the cache code obscures this fact.
//
// The next possible solution was to possibly store in the global cache the *scheddCollections alongside mutexes to access them, wrapped in a struct,
// all in one map.  This introduces a race condition, though not one that threatens data integrity, just efficiency.  If the first goroutine
// to run this method runs a sync.Map.Load (or LoadAndStore) on the cache map and sees that no value was returned, it will go ahead and query
// the condor collector.  Before the first one is finished storing the schedd values in the cache map, if the SECOND goroutine tries to read
// from the map, it will ALSO see that no value is yet stored, and query the condor collector.
//
// By decoupling the cache data and the cache mutexes, we can take advantage of LoadAndStore on the cache mutex map, and if a mutex doesn't exist
// for a collector, quickly store a new mutex there and acquire the Lock.  If the mutex does exist, we only want to read the data, so we can
// acquire the RLock, which will wait for our first goroutine to give up the Lock (presumably after it writes the schedds to cache)
// to proceed with the read.  This is the basic logic of the locks here.
func GetScheddsFromConfiguration(checkServiceConfigPath string) []string {
	funcLogger := log.WithField("serviceConfigPath", checkServiceConfigPath)
	schedds := make([]string, 0)

	// 1. Try override
	// If condorCreddHostOverride is set either globally or at service level, set the schedd slice to that
	if creddOverrideVar, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorCreddHost"); viper.IsSet(creddOverrideVar) {
		schedds = append(schedds, viper.GetString(creddOverrideVar))
		funcLogger.WithField("schedds", schedds).Debug("Set schedds successfully from override")
		return schedds
	}

	// 2.  Try cache
	// Get our collector so we can see if the schedds are in cache
	collectorHost := GetCondorCollectorHostFromConfiguration(checkServiceConfigPath)
	collectorLogger := funcLogger.WithField("collector", collectorHost)
	collectorTryRWMutex := &sync.RWMutex{} // We'll store this mutex in collectorsQueriedForSchedds if that map doesn't already have a mutex
	// stored for this collectorHost

	if val, loaded := collectorMutexes.LoadOrStore(collectorHost, collectorTryRWMutex); loaded {
		// If we've queried this collector before, the schedds should be in cache.  Return those schedds
		// Acquire the read lock on this collector
		if valAsRWMutex, isRWMutex := val.(*sync.RWMutex); isRWMutex {
			valAsRWMutex.RLock()
			defer valAsRWMutex.RUnlock()
			if scheddCol, scheddOk := globalSchedds.Load(collectorHost); scheddOk {
				if scheddColVal, isScheddColPtr := scheddCol.(*scheddCollection); isScheddColPtr {
					schedds = scheddColVal.getSchedds()
					collectorLogger.WithField("schedds", schedds).Debug("Set schedds successfully from cache")
					return schedds
				}
			}
		}
	}

	// 3.  Query collector
	// At this point, we haven't queried this collector yet.  Do so, and store its schedds in the global store/cache, then return the schedds
	// Use the mutex we just stored and lock it for writing
	collectorTryRWMutex.Lock()
	defer collectorTryRWMutex.Unlock()

	// Get constraint, if it's set
	var constraint string
	constraintKey, _ := GetServiceConfigOverrideKeyOrGlobalKey(checkServiceConfigPath, "condorScheddConstraint")
	if viper.IsSet(constraintKey) {
		constraint = viper.GetString(constraintKey)
		collectorLogger.WithField("constraint", constraint).Debug("Found constraint for condor collector query (condor_status)")
	}

	schedds = getScheddsFromCondor(collectorHost, constraint)

	// Add schedds to cache
	scheddCollectionToStore := newScheddCollection()
	scheddCollectionToStore.storeSchedds(schedds)
	globalSchedds.Store(collectorHost, scheddCollectionToStore) // Store the schedds into the global store

	collectorLogger.WithField("schedds", schedds).Debug("Set schedds successfully from collector")
	return schedds

}

// getScheddsFromCondor queries the condor collector for the schedds in the cluster that satisfy the constraint
func getScheddsFromCondor(collectorHost, constraint string) []string {
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

	}

	schedds := make([]string, 0)
	for _, classad := range classads {
		name := classad["Name"].String()
		schedds = append(schedds, name)
	}
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
