package cmdUtils_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/cmdUtils"
	"github.com/shreyb/managed-tokens/internal/testUtils"
)

// Helper funcs
var serviceConfigPath string = "experiments.myexperiment.roles.myrole"

func serviceOverrideKey(key string) string { return serviceConfigPath + "." + key + "Override" }

// TestGetServiceConfigOverrideKeyOrGlobalKey checks that we properly return the configuration path for a given set of configuration
// entries.  We want to ensure that if there is a service-level override of a global key, that we properly return that service-level
// override path
func TestGetServiceConfigOverrideKeyOrGlobalKey(t *testing.T) {
	randomKey := func() string { return fmt.Sprintf("key%d", rand.Intn(2^32-1)) }
	checkOverriddenAndGetKey := func(overridden bool, key string) string {
		if overridden {
			return serviceOverrideKey(key)
		}
		return key
	}
	type testCase struct {
		description            string
		setupTestFunc          func() string
		expectedOverridden     bool
		expectedConfigPathFunc func(bool, string) string
	}

	testCases := []testCase{
		{
			"Valid service-level override",
			func() string {
				returnKey := randomKey()
				viper.Set(returnKey, "foo")
				viper.Set(serviceOverrideKey(returnKey), "bar")
				return returnKey
			},
			true,
			checkOverriddenAndGetKey,
		},
		{
			"No service-level override",
			func() string {
				returnKey := randomKey()
				viper.Set(returnKey, "foo")
				return returnKey
			},
			false,
			checkOverriddenAndGetKey,
		},
		{
			"Service-level configuration, but not an override",
			func() string {
				returnKey := randomKey()
				viper.Set(returnKey, "foo")
				viper.Set(serviceOverrideKey(returnKey)+"ButDifferent", "bar")
				return returnKey
			},
			false,
			checkOverriddenAndGetKey,
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				testKey := test.setupTestFunc()
				expectedConfigPath := test.expectedConfigPathFunc(test.expectedOverridden, testKey)

				configPath, overridden := cmdUtils.GetServiceConfigOverrideKeyOrGlobalKey(serviceConfigPath, testKey)
				viper.Reset()
				if overridden != test.expectedOverridden {
					t.Errorf("Got unexpected overridden bool.  Expected %t, got %t", test.expectedOverridden, overridden)
				}
				if configPath != expectedConfigPath {
					t.Errorf("Got unexpected configPath.  Expected %s, got %s", expectedConfigPath, configPath)
				}
			})
	}

}

func TestGetCondorCollectorHostFromConfiguration(t *testing.T) {
	type testCase struct {
		description   string
		setupTestFunc func()
		expectedValue string
	}

	testCases := []testCase{
		{
			"Global level configuration, no override",
			func() { viper.Set("condorCollectorHost", "foo") },
			"foo",
		},
		{
			"Global level configuration, service-level override",
			func() {
				viper.Set("condorCollectorHost", "foo")
				viper.Set(serviceOverrideKey("condorCollectorHost"), "bar")
			},
			"bar",
		},
		{
			"No global level configuration, service-level override",
			func() {
				viper.Set(serviceOverrideKey("condorCollectorHost"), "bar")
			},
			"bar",
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				test.setupTestFunc()
				value := cmdUtils.GetCondorCollectorHostFromConfiguration(serviceConfigPath)
				viper.Reset()
				if value != test.expectedValue {
					t.Errorf("Got wrong value for condorCollectorHost.  Expected %s, got %s", test.expectedValue, value)
				}
			})
	}

}

func TestGetUserPrincipalFromConfiguration(t *testing.T) {
	type testCase struct {
		description   string
		setupTestFunc func()
		expectedValue string
	}

	testCases := []testCase{
		{
			"Global level configuration, no override",
			func() {
				viper.Set("kerberosPrincipalPattern", "foobar{{.Account}}")
				viper.Set(serviceConfigPath+".account", "myaccount")
			},
			"foobarmyaccount",
		},
		{
			"Invalid global-level configuration",
			func() {
				viper.Set("kerberosPrincipalPattern", "foobar{{.DifferentField}}")
				viper.Set(serviceConfigPath+".account", "myaccount")
			},
			"",
		},
		{
			"Service-level override",
			func() {
				viper.Set("kerberosPrincipalPattern", "foobar{{.Account}}")
				viper.Set(serviceConfigPath+".account", "myaccount")
				viper.Set(serviceConfigPath+".userPrincipalOverride", "mykerberosprincipal")
			},
			"mykerberosprincipal",
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				test.setupTestFunc()

				value := cmdUtils.GetUserPrincipalFromConfiguration(serviceConfigPath)
				viper.Reset()
				if value != test.expectedValue {
					t.Errorf("Got wrong user principal.  Expected %s, got %s", test.expectedValue, value)
				}
			})
	}
}

func TestGetUserPrincipalAndHtgettokenoptsFromConfiguration(t *testing.T) {
	type testCase struct {
		description              string
		kerberosPrincipalSetting string
		htgettokenOptsInEnv      string
		minTokenLifetimeSetting  string
		expectedUserPrincipal    string
		expectedHtgettokenopts   string
	}

	testCases := []testCase{
		{
			"No user principal:  SHould give blank userPrincipal and Htgettokenopts",
			"",
			"",
			"",
			"",
			"",
		},
		{
			"OK User principal with @FNAL.GOV, HTGETTOKENOPTS set in env, credkey in HTGETTOKENOPTS",
			"{{.Account}}/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"--credkey=myaccount/managedtokenstest/test.fnal.gov",
			"",
			"myaccount/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"--credkey=myaccount/managedtokenstest/test.fnal.gov",
		},
		{
			"OK User principal with @FNAL.GOV, HTGETTOKENOPTS set in env, credkey not in HTGETTOKENOPTS",
			"{{.Account}}/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"someotherstuffbutnotcredkey",
			"",
			"myaccount/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"someotherstuffbutnotcredkey --credkey=myaccount/managedtokenstest/test.fnal.gov",
		},
		{
			"OK User prinicpal with @FNAL.GOV, HTGETTOKENOPTS not set in env, have minTokenLifetime set",
			"{{.Account}}/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"",
			"12345h",
			"myaccount/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"--vaulttokenminttl=12345h --credkey=myaccount/managedtokenstest/test.fnal.gov",
		},
		{
			"OK User prinicpal with @FNAL.GOV, HTGETTOKENOPTS not set in env,  No minTokenLifetime set",
			"{{.Account}}/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"",
			"",
			"myaccount/managedtokenstest/test.fnal.gov@FNAL.GOV",
			"--vaulttokenminttl=10s --credkey=myaccount/managedtokenstest/test.fnal.gov",
		},
		{
			"OK User principal with no @FNAL.GOV, no HTGETTOKENOPTS set, no minTokenLifetime set",
			"{{.Account}}-principal",
			"",
			"",
			"myaccount-principal",
			"--vaulttokenminttl=10s --credkey=myaccount-principal",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				setFunc := func(key, value string) {
					if value != "" {
						viper.Set(key, value)
					}
				}
				viper.Set(serviceConfigPath+".account", "myaccount")
				setFunc("kerberosPrincipalPattern", test.kerberosPrincipalSetting)
				setFunc("ORIG_HTGETTOKENOPTS", test.htgettokenOptsInEnv)
				setFunc("minTokenLifetime", test.minTokenLifetimeSetting)

				userPrincipal, htgettokenOpts := cmdUtils.GetUserPrincipalAndHtgettokenoptsFromConfiguration(serviceConfigPath)
				viper.Reset()
				if userPrincipal != test.expectedUserPrincipal {
					t.Errorf("Got wrong user principal.  Expected %s, got %s", test.expectedUserPrincipal, userPrincipal)
				}
				if htgettokenOpts != test.expectedHtgettokenopts {
					t.Errorf("Got wrong HTGETTOKENOPTS.  Expected %s, got %s", test.expectedHtgettokenopts, htgettokenOpts)
				}
			},
		)
	}
}

func TestGetKeytabOverrideFromConfiguration(t *testing.T) {
	type testCase struct {
		description        string
		setupTestFunc      func()
		expectedKeytabPath string
	}

	testCases := []testCase{
		{
			"Overridden keytab location",
			func() { viper.Set(serviceConfigPath+".keytabPathOverride", "overridden_location") },
			"overridden_location",
		},
		{
			"Default Keytab Location",
			func() {
				viper.Set("keytabPath", "/path/to/keytab")
				viper.Set(serviceConfigPath+".account", "myaccount")
			},
			"/path/to/keytab/myaccount.keytab",
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				test.setupTestFunc()
				keytabPath := cmdUtils.GetKeytabFromConfiguration(serviceConfigPath)
				viper.Reset()

				if keytabPath != test.expectedKeytabPath {
					t.Errorf("Got wrong keytab path.  Expected %s, got %s", test.expectedKeytabPath, keytabPath)
				}
			},
		)
	}
}

// TestGetScheddsFromConfigurationOverride only tests the override case, since GetScheddsFromConfiguration has a fallthrough that
// relies on running in a condor cluster.  So here we just make sure that the override works right
func TestGetScheddsFromConfigurationOverride(t *testing.T) {
	type testCase struct {
		description     string
		setupTestFunc   func()
		expectedSchedds []string
	}

	testCases := []testCase{

		{
			"Global override",
			func() {
				viper.Set("condorCreddHost", "mycreddhost")
			},
			[]string{"mycreddhost"},
		},
		{
			"Service-level override",
			func() {
				viper.Set("condorCreddHost", "mycreddhost")
				viper.Set(serviceConfigPath+".condorCreddHostOverride", "myservicecreddhost")
			},
			[]string{"myservicecreddhost"},
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				test.setupTestFunc()
				schedds := cmdUtils.GetScheddsFromConfiguration(serviceConfigPath)
				viper.Reset()

				if !testUtils.SlicesHaveSameElements(test.expectedSchedds, schedds) {
					t.Errorf("Returned schedd slices are not the same.  Expected %v, got %v", test.expectedSchedds, schedds)

				}
			},
		)
	}
}