package environment

import (
	"testing"
)

const (
	krb5ccnameTestValue          string = "krb5ccname_test"
	condorCreddHostTestValue     string = "condor_credd_host_setting"
	condorCollectorHostTestValue string = "condor_credd_host_setting"
	htgettokenoptsTestValue      string = "htgettokenopts_setting"
)

var cmdEnvFull CommandEnvironment = CommandEnvironment{
	Krb5ccname:          "KRB5CCNAME=krb5ccname_setting",
	CondorCreddHost:     "_condor_CREDD_HOST=condor_credd_host_setting",
	CondorCollectorHost: "_condor_COLLECTOR_HOST=condor_collector_host_setting",
	HtgettokenOpts:      "HTGETTOKENOPTS=htgettokenopts_setting",
}

var cmdEnvPartial CommandEnvironment = CommandEnvironment{
	Krb5ccname:      "KRB5CCNAME=krb5ccname_setting",
	CondorCreddHost: "_condor_CREDD_HOST=condor_credd_host_setting",
}

// TestSetKrb5ccname ensures that SetKrb5ccname properly handles the passed value and kerberosCCacheType and stores the proper string in the
// CommandEnvironment
func TestSetKrb5ccname(t *testing.T) {
	type testCase struct {
		description    string
		cacheType      kerberosCCacheType
		expectedResult string
	}

	testCases := []testCase{
		{
			"Use DIR",
			DIR,
			"KRB5CCNAME=DIR:" + krb5ccnameTestValue,
		},
		{
			"Use FILE",
			FILE,
			"KRB5CCNAME=FILE:" + krb5ccnameTestValue,
		},
	}

	for _, test := range testCases {
		t.Run(test.description, func(t *testing.T) {
			c := &CommandEnvironment{}
			c.SetKrb5ccname(krb5ccnameTestValue, test.cacheType)
			result := string(c.Krb5ccname)

			if test.expectedResult != result {
				t.Errorf("Set wrong value for KRB5CCNAME env.  Expected %s, got %s", test.expectedResult, result)
			}
		},
		)
	}
}

// TestSetCondorCreddHost checks that SetCondorCreddHost properly sets the CondorCreddHost field in the CommandEnvironment
func TestSetCondorCreddHost(t *testing.T) {
	c := &CommandEnvironment{}
	c.SetCondorCreddHost(condorCreddHostTestValue)
	expected := "_condor_CREDD_HOST=" + condorCreddHostTestValue
	result := string(c.CondorCreddHost)
	if expected != result {
		t.Errorf("Set wrong value for _condor_CREDD_HOST env.  Expected %s, got %s", expected, result)
	}
}

// TestSetCondorCollectorHost checks that SetCondorCollectorHost properly sets the CondorCollectorHost field in the CommandEnvironment
func TestSetCondorCollectorHost(t *testing.T) {
	c := &CommandEnvironment{}
	c.SetCondorCollectorHost(condorCollectorHostTestValue)
	expected := "_condor_COLLECTOR_HOST=" + condorCollectorHostTestValue
	result := string(c.CondorCollectorHost)
	if expected != result {
		t.Errorf("Set wrong value for _condor_COLLECTOR_HOST env.  Expected %s, got %s", expected, result)
	}
}

// TestSetHtgettokenopts checks that SetHtgettokenopts properly sets the Htgettokenopts field in the CommandEnvironment
func TestSetHtgettokenopts(t *testing.T) {
	c := &CommandEnvironment{}
	c.SetHtgettokenOpts(htgettokenoptsTestValue)
	expected := "HTGETTOKENOPTS=" + htgettokenoptsTestValue
	result := string(c.HtgettokenOpts)
	if expected != result {
		t.Errorf("Set wrong value for HTGETTOKENOPTS env.  Expected %s, got %s", expected, result)
	}
}

// TestGetSetting checks that we properly get values from the CommandEnvironment and handle the case of an unsupported CommandEnvironment field
func TestGetSetting(t *testing.T) {
	type testCase struct {
		description string
		*CommandEnvironment
		key            supportedCommandEnvironmentField
		expectedResult string
	}

	testCases := []testCase{
		{
			"Full CommandEnvironment, get _condor_CREDD_HOST",
			&cmdEnvFull,
			CondorCreddHost,
			"_condor_CREDD_HOST=" + condorCreddHostTestValue,
		},
		{
			"Partial CommandEnvironment, get valid key (_condor_CREDD_HOST)",
			&cmdEnvPartial,
			CondorCreddHost,
			"_condor_CREDD_HOST=" + condorCreddHostTestValue,
		},
		{
			"Partial CommandEnvironment, get missing key (_condor_COLLECTOR_HOST)",
			&cmdEnvPartial,
			CondorCollectorHost,
			"",
		},
		{
			"Full CommandEnvironment, get invalid key (should be impossible/difficult, but let's make sure it's handled correctly)",
			&cmdEnvFull,
			supportedCommandEnvironmentField(45678),
			"unsupported CommandEnvironment field",
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				result := test.CommandEnvironment.GetSetting(test.key)
				if result != test.expectedResult {
					t.Errorf("Got unexpected result when retrieving value from CommandEnvironment.  Expected %s, got %s", test.expectedResult, result)
				}
			},
		)
	}
}

func TestGetValue(t *testing.T) {
	c := cmdEnvFull
	key := HtgettokenOpts
	expected := htgettokenoptsTestValue
	result := c.GetValue(key)

	if expected != result {
		t.Errorf("Got wrong value from CommandEnvironment.  Expected %s, got %s", expected, result)
	}
}
