package main

import (
	"strings"

	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/worker"
)

// Functional options for use in setting up kerberos/JWT Auth

// setkrb5ccname sets the KRB5CCNAME directory environment variable in the worker.Config's
// environment
func setkrb5ccname(krb5ccname string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		c.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}

// setKeytabPath sets the location of a worker.Config's kerberos keytab
func setKeytabPath() func(c *worker.Config) error {
	return func(c *worker.Config) error {
		c.KeytabPath = viper.GetString("ferry.serviceKeytabPath")
		return nil
	}
}

// setUserPrincipalAndHtgettokenopts sets a worker.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable
func setUserPrincipalAndHtgettokenopts() func(c *worker.Config) error {
	return func(c *worker.Config) error {
		var htgettokenOptsRaw string
		c.UserPrincipal = viper.GetString("ferry.serviceKerberosPrincipal")
		credKey := strings.ReplaceAll(c.UserPrincipal, "@FNAL.GOV", "")

		if viper.IsSet("htgettokenopts") {
			htgettokenOptsRaw = viper.GetString("htgettokenopts")
		} else {
			htgettokenOptsRaw = "--credkey=" + credKey
		}
		c.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + htgettokenOptsRaw + "\""
		return nil
	}
}
