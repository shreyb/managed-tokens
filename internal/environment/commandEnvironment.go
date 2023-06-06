// Package environment contains types and functions to assist in passing around environments to be used in commands and wrapping commands in
// those environments
package environment

import (
	"strings"
)

// supportedCommandEnvironmentField is a mapping to the environment variable keys that are supported by this library
type supportedCommandEnvironmentField int

const (
	Krb5ccname supportedCommandEnvironmentField = iota
	CondorCreddHost
	CondorCollectorHost
	HtgettokenOpts
)

// getAllSupportedCommandEnvironmentFields returns an array of the possible valid supportedCommandEnvironmentField values
func getAllSupportedCommandEnvironmentFields() [4]supportedCommandEnvironmentField {
	return [4]supportedCommandEnvironmentField{
		Krb5ccname,
		CondorCreddHost,
		CondorCollectorHost,
		HtgettokenOpts,
	}
}

func (s supportedCommandEnvironmentField) envVarKey() string {
	switch s {
	case Krb5ccname:
		return "KRB5CCNAME"
	case CondorCreddHost:
		return "_condor_CREDD_HOST"
	case CondorCollectorHost:
		return "_condor_COLLECTOR_HOST"
	case HtgettokenOpts:
		return "HTGETTOKENOPTS"
	default:
		return "unsupported environment prefix"
	}
}

// kerberos CCache types.  See https://web.mit.edu/kerberos/krb5-1.12/doc/basic/ccache_def.html
type kerberosCCacheType int

const (
	api     kerberosCCacheType = iota // Unsupported on Linux - only on Windows
	DIR                               // Indicates the location of a collection of caches
	FILE                              // Indicates the location of a single cache
	keyring                           // Unsupported by this library
)

func (k kerberosCCacheType) String() string {
	switch k {
	case DIR:
		return "DIR:"
	case FILE:
		return "FILE:"
	default:
		return "unsupported kerberos cache type"
	}
}

type environmentVariableSetting string

// CommandEnvironment is an environment for the various token-related commands to use to obtain vault and bearer tokens
// The values of the fields are meant to be the full environment variable assignment statement, e.g.
//
//	c := CommandEnvironment{
//	Krb5ccname: "KRB5CCNAME=/tmp/mykrb5ccdir",
//	}
//
// It is recommended to set the fields of the CommandEnvironment using the exported methods SetKrb5ccname, SetCondorCreddHost, etc.
type CommandEnvironment struct {
	// Krb5ccname is the environment variable assignment for the cache directory for kerberos credentials
	// Values should be of the form "KRB5CCNAME=DIR:/my/kerberos/cache/dir"
	Krb5ccname environmentVariableSetting
	// CondorCreddHost is the hostname that is running the HTCondor credd.  Values should be of the form
	// "_condor_CREDD_HOST=hostname.example.com"
	CondorCreddHost environmentVariableSetting
	// CondorCollectorHost is the hostname that is running the HTCondor collector.  Values should be of the form
	// "_condor_COLLECTOR_HOST=hostname.example.com"
	CondorCollectorHost environmentVariableSetting
	// HtgettokenOpts is the set of options that need to be passed to condor_vault_storer, and underneath it,
	// htgettoken.  Values should be of the form "HTGETTOKENOPTS=\"--opt1=val1 --opt2\" (note the escaped quotes)"
	HtgettokenOpts environmentVariableSetting
}

// SetKrb5CCName sets Krb5ccname field in the CommandEnvironment
func (c *CommandEnvironment) SetKrb5ccname(value string, t kerberosCCacheType) {
	c.Krb5ccname = environmentVariableSetting(Krb5ccname.envVarKey() + "=" + t.String() + value)
}

// SetCondorCreddHost sets CondorCreddHost field in the CommandEnvironment
func (c *CommandEnvironment) SetCondorCreddHost(value string) {
	c.CondorCreddHost = environmentVariableSetting(CondorCreddHost.envVarKey() + "=" + value)
}

// SetCondorCollectorHost sets CondorCollectorHost field in the CommandEnvironment
func (c *CommandEnvironment) SetCondorCollectorHost(value string) {
	c.CondorCollectorHost = environmentVariableSetting(CondorCollectorHost.envVarKey() + "=" + value)
}

// SetHtgettokenOpts sets the HtgettokenOpts field in the Command Environment
func (c *CommandEnvironment) SetHtgettokenOpts(value string) {
	c.HtgettokenOpts = environmentVariableSetting(HtgettokenOpts.envVarKey() + "=" + value)
}

// GetSetting retrieves the full key=value setting from a supportedCommandEnvironmentField in the CommandEnvironment.
// For example, for the snippet:
//
//	c := CommandEnvironment{ Krb5ccname: "KRB5CCNAME=DIR:krb5ccname_setting "}
//	setting := c.GetSetting(Krb5ccname)
//
// setting will be "KRB5CCNAME=DIR:krb5ccname_setting"
func (c *CommandEnvironment) GetSetting(s supportedCommandEnvironmentField) string {
	return string(c.mapSupportedFieldsToStructFields(s))
}

// GetValue retrieves the full key=value setting from a supportedCommandEnvironmentField in the CommandEnvironment, trims the "key=" portion,
// and returns just the value
// For example, for the snippet:
//
//	c := CommandEnvironment{ Krb5ccname: "KRB5CCNAME=DIR:krb5ccname_setting "}
//	value:= c.GetValue(Krb5ccname)
//
// value will be "DIR:krb5ccname_setting"
func (c *CommandEnvironment) GetValue(s supportedCommandEnvironmentField) string {
	fullString := c.GetSetting(s)
	prefix := s.envVarKey() + "="
	return strings.TrimPrefix(fullString, prefix)
}

// Copy returns a new *CommandEnvironment with the fields set to the same values as the original
func (c *CommandEnvironment) Copy() *CommandEnvironment {
	newEnv := CommandEnvironment{
		Krb5ccname:          c.Krb5ccname,
		CondorCreddHost:     c.CondorCreddHost,
		CondorCollectorHost: c.CondorCollectorHost,
		HtgettokenOpts:      c.HtgettokenOpts,
	}
	return &newEnv
}

func (c *CommandEnvironment) String() string {
	envSlice := make([]string, 0)
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		envSlice = append(envSlice, c.GetSetting(field))
	}
	return strings.Join(envSlice, " ")
}

func (c *CommandEnvironment) mapSupportedFieldsToStructFields(s supportedCommandEnvironmentField) environmentVariableSetting {
	switch s {
	case Krb5ccname:
		return c.Krb5ccname
	case CondorCreddHost:
		return c.CondorCreddHost
	case CondorCollectorHost:
		return c.CondorCollectorHost
	case HtgettokenOpts:
		return c.HtgettokenOpts
	default:
		return "unsupported CommandEnvironment field"
	}
}
