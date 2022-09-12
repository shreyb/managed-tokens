package main

import (
	"fmt"
	"html/template"
	"os"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/worker"
)

// Custom usage function for positional argument.
func onboardingUsage() {
	fmt.Printf("Usage: %s [OPTIONS] service...\n", os.Args[0])
	fmt.Printf("service must be of the form 'experiment_role', e.g. 'dune_production'\n")
	pflag.PrintDefaults()
}

// Functional options for initialization of serviceConfigs

// setCondorCredHost sets the _condor_CREDD_HOST environment variable in the worker.Config's environment
func setCondorCreddHost(serviceConfigPath string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		addString := "_condor_CREDD_HOST="
		overrideVar := serviceConfigPath + ".condorCreddHostOverride"
		if viper.IsSet(overrideVar) {
			addString = addString + viper.GetString(overrideVar)
		} else {
			addString = addString + viper.GetString("condorCreddHost")
		}
		c.CommandEnvironment.CondorCreddHost = addString
		return nil
	}
}

// setCondorCollectorHost sets the _condor_COLLECTOR_HOST environment variable in the worker.Config's environment
func setCondorCollectorHost(serviceConfigPath string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		addString := "_condor_COLLECTOR_HOST="
		overrideVar := serviceConfigPath + ".condorCollectorHostOverride"
		if viper.IsSet(overrideVar) {
			addString = addString + viper.GetString(overrideVar)
		} else {
			addString = addString + viper.GetString("condorCollectorHost")
		}
		c.CommandEnvironment.CondorCollectorHost = addString
		return nil
	}
}

// setUserPrincipalAndHtgettokenopts sets a worker.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable
func setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, experiment string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		var htgettokenOptsRaw string
		userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern"))
		if err != nil {
			log.Errorf("Error parsing Kerberos Principal Template, %s", err)
			return err
		}
		userPrincipalOverrideConfigPath := serviceConfigPath + ".userPrincipalOverride"
		if viper.IsSet(userPrincipalOverrideConfigPath) {
			c.UserPrincipal = viper.GetString(userPrincipalOverrideConfigPath)
		} else {
			var b strings.Builder
			templateArgs := struct{ Account string }{Account: viper.GetString(serviceConfigPath + ".account")}
			if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
				log.WithField("experiment", experiment).Error("Could not execute kerberos prinicpal template")
				return err
			}
			c.UserPrincipal = b.String()
		}

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

// setKeytabOverride checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab for a worker.Config
func setKeytabOverride(serviceConfigPath string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		keytabConfigPath := serviceConfigPath + ".keytabPath"
		if viper.IsSet(keytabConfigPath) {
			c.KeytabPath = viper.GetString(keytabConfigPath)
		} else {
			// Default keytab location
			keytabDir := viper.GetString("keytabPath")
			c.KeytabPath = path.Join(
				keytabDir,
				fmt.Sprintf(
					"%s.keytab",
					viper.GetString(serviceConfigPath+".account"),
				),
			)
		}
		return nil
	}
}

// account sets the account field in the worker.Config object
func account(serviceConfigPath string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		c.Account = viper.GetString(serviceConfigPath + ".account")
		return nil
	}
}

// setkrb5ccname sets the KRB5CCNAME directory environment variable in the worker.Config's
// environment
func setkrb5ccname(krb5ccname string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		c.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}
