// Package environment contains types and functions to assist in passing around environments to be used in commands and wrapping commands in
// those environments
package environment

import (
	"strings"
)

// TODO:  Maybe clean up CommandEnvironment so that it has a friendlier interface.  We can just
// have the actual env var name be internal to the type, rather than the user needing to
// know that.  I.e. You would say c := CommandEnvironment{Krb5ccname: "blahblah"}, and not need
// to specify Krb5ccname: "KRB5CCNAME=blahblah"

// TODO:  Rather than have CommandEnvironment be a static struct, maybe have it take an enum for environment variables so it's more easily expandable in the future

// CommandEnvironment is an environment for the various token-related commands to use to obtain vault and bearer tokens
// The values of the fields are meant to be the full environment variable assignment statement, e.g.
// c := CommandEnvironment{
// Krb5ccname: "KRB5CCNAME=/tmp/mykrb5ccdir",
// }
type CommandEnvironment struct {
	// Krb5ccname is the environment variable assignment for the cache directory for kerberos credentials
	// Values should be of the form "KRB5CCNAME=DIR:/my/kerberos/cache/dir"
	Krb5ccname string
	// CondorCreddHost is the hostname that is running the HTCondor credd.  Values should be of the form
	// "_condor_CREDD_HOST=hostname.example.com"
	CondorCreddHost string
	// CondorCollectorHost is the hostname that is running the HTCondor collector.  Values should be of the form
	// "_condor_COLLECTOR_HOST=hostname.example.com"
	CondorCollectorHost string
	// HtgettokenOpts is the set of options that need to be passed to condor_vault_storer, and underneath it,
	// htgettoken.  Values should be of the form "HTGETTOKENOPTS=\"--opt1=val1 --opt2\" (note the escaped quotes)"
	HtgettokenOpts string
}

// ToMap translates the CommandEnvironment struct to a map[string]string with the keys named for the fields
func (c *CommandEnvironment) ToMap() map[string]string {
	return map[string]string{
		"Krb5ccname":          c.Krb5ccname,
		"CondorCreddHost":     c.CondorCreddHost,
		"CondorCollectorHost": c.CondorCollectorHost,
		"HtgettokenOpts":      c.HtgettokenOpts,
	}
}

// ToEnvs gives the map of the environment variable key for each field in the CommandEnvironment
func (c *CommandEnvironment) toEnvs() map[string]string {
	return map[string]string{
		"Krb5ccname":          "KRB5CCNAME",
		"CondorCreddHost":     "_condor_CREDD_HOST",
		"CondorCollectorHost": "_condor_COLLECTOR_HOST",
		"HtgettokenOpts":      "HTGETTOKENOPTS",
	}
}

// ToValues gives a map of the fields of the CommandEnvironment to just the value of the environment
// variable setting.  For example, if we have:
//
// c = CommandEnvironment{Krb5ccname: "KRB5CCNAME=/path/to/krb5cache"}
//
// then:
//
//	c.ToValues = map[string]string{
//	 "Krb5ccname": "/path/to/krb5cache",
//	 "CondorCreddHost": "",
//	 "CondorCollectorHost": "",
//	 "HtgettokenOpts": "",
//	}
func (c *CommandEnvironment) ToValues() map[string]string {
	mapC := c.ToMap()
	m := make(map[string]string, len(mapC))
	for key, envSetting := range mapC {
		value := strings.TrimPrefix(envSetting, c.toEnvs()[key]+"=")
		m[key] = value
	}
	return m
}

func (c *CommandEnvironment) String() string {
	mapC := c.ToMap()
	envSlice := make([]string, len(mapC))
	for _, val := range mapC {
		envSlice = append(envSlice, val)
	}
	return strings.Join(envSlice, " ")
}

// TODO Deprecate this.  It's too complicated
// EnvironmentMapper is an interface which can be used to get environment variable information for a command
type EnvironmentMapper interface {
	ToMap() map[string]string
	ToValues() map[string]string
	toEnvs() map[string]string
}
