package main

import (
	"context"
	"fmt"
	"path"
	"strings"
	"text/template"

	condor "github.com/retzkek/htcondor-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/worker"
)

// Functional options for worker.Config initialization

// setCondorCredHost sets the _condor_CREDD_HOST environment variable in the worker.Config's environment
func setCondorCreddHost(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		addString := "_condor_CREDD_HOST="
		overrideVar := serviceConfigPath + ".condorCreddHostOverride"
		if viper.IsSet(overrideVar) {
			addString = addString + viper.GetString(overrideVar)
			sc.CommandEnvironment.CondorCreddHost = addString
		}
		return nil
	}
}

// setCondorCollectorHost sets the _condor_COLLECTOR_HOST environment variable in the worker.Config's environment
func setCondorCollectorHost(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		addString := "_condor_COLLECTOR_HOST="
		overrideVar := serviceConfigPath + ".condorCollectorHostOverride"
		if viper.IsSet(overrideVar) {
			addString = addString + viper.GetString(overrideVar)
		} else {
			addString = addString + viper.GetString("condorCollectorHost")
		}
		sc.CommandEnvironment.CondorCollectorHost = addString
		return nil
	}
}

// setUserPrincipalAndHtgettokenopts sets a worker.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable
func setUserPrincipalAndHtgettokenopts(serviceConfigPath, experiment string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		var htgettokenOptsRaw string
		userPrincipalOverrideConfigPath := serviceConfigPath + ".userPrincipalOverride"
		if viper.IsSet(userPrincipalOverrideConfigPath) {
			sc.UserPrincipal = viper.GetString(userPrincipalOverrideConfigPath)
		} else {
			userPrincipalTemplate := template.Must(template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern")))
			var b strings.Builder
			templateArgs := struct{ Account string }{Account: viper.GetString(serviceConfigPath + ".account")}
			if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
				log.WithField("experiment", experiment).Error("Could not execute kerberos prinicpal template")
				return err
			}
			sc.UserPrincipal = b.String()
		}

		credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")

		// Look for HTGETTOKKENOPTS in environment.  If it's given here, take as is, but add credkey if it's absent
		if viper.IsSet("ORIG_HTGETTOKENOPTS") {
			log.Debugf("Prior to running, HTGETTOKENOPTS was set to %s", viper.GetString("ORIG_HTGETTOKENOPTS"))
			// If we have the right credkey in the HTGETTOKENOPTS, leave it be
			if strings.Contains(viper.GetString("ORIG_HTGETTOKENOPTS"), credKey) {
				htgettokenOptsRaw = viper.GetString("ORIG_HTGETTOKENOPTS")
			} else {
				once.Do(
					func() {
						log.Warn("HTGETTOKENOPTS was provided in the environment and does not have the proper --credkey specified.  Will add it to the existing HTGETTOKENOPTS")
					},
				)
				htgettokenOptsRaw = viper.GetString("ORIG_HTGETTOKENOPTS") + " --credkey=" + credKey
			}
		} else {
			// Calculate minimum vault token lifetime from config
			var lifetimeString string
			defaultLifetimeString := "3d"
			if viper.IsSet("minTokenLifetime") {
				lifetimeString = viper.GetString("minTokenLifetime")
			} else {
				lifetimeString = defaultLifetimeString
			}

			htgettokenOptsRaw = "--vaulttokenminttl=" + lifetimeString + " --credkey=" + credKey
		}
		log.Debugf("Final HTGETTOKENOPTS: %s", htgettokenOptsRaw)
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=" + htgettokenOptsRaw
		return nil
	}
}

// setKeytabOverride checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab for a worker.Config
func setKeytabOverride(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		keytabConfigPath := serviceConfigPath + ".keytabPathOverride"
		if viper.IsSet(keytabConfigPath) {
			sc.KeytabPath = viper.GetString(keytabConfigPath)
		} else {
			// Default keytab location
			keytabDir := viper.GetString("keytabPath")
			sc.KeytabPath = path.Join(
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

// setDesiredUIByOverrideOrLookup sets the worker.Config's DesiredUID field by checking the configuration for the "account"
// field.  It then checks the configuration to see if there is a configured override for the UID.  If it not overridden,
// the default behavior is to query the managed tokens database that should be populated by the refresh-uids-from-ferry executable.
//
// If the default behavior is not possible, the configuration should have a desiredUIDOverride field to allow token-push to run properly
func setDesiredUIByOverrideOrLookup(ctx context.Context, serviceConfigPath string) func(*worker.Config) error {
	return func(sc *worker.Config) error {
		if viper.IsSet(serviceConfigPath + ".desiredUIDOverride") {
			sc.DesiredUID = viper.GetUint32(serviceConfigPath + ".desiredUIDOverride")
		} else {
			// Get UID from SQLite DB that should be kept up to date by refresh-uids-from-ferry
			func() error {
				var dbLocation string
				var uid int

				username := viper.GetString(serviceConfigPath + ".account")

				if viper.IsSet("dbLocation") {
					dbLocation = viper.GetString("dbLocation")
				} else {
					dbLocation = "/var/lib/managed-tokens/uid.db"

				}

				ferryUidDb, err := db.OpenOrCreateDatabase(dbLocation)
				if err != nil {
					log.WithField("executable", currentExecutable).Error("Could not open or create FERRYUIDDatabase")
					return err
				}
				defer ferryUidDb.Close()

				uid, err = ferryUidDb.GetUIDByUsername(ctx, username)
				if err != nil {
					log.Error("Could not get UID by username")
					return err
				}
				log.WithFields(log.Fields{
					"username": username,
					"uid":      uid,
				}).Debug("Got UID")
				sc.DesiredUID = uint32(uid)
				return nil
			}()
		}
		return nil
	}
}

// setkrb5ccname sets the KRB5CCNAME directory environment variable in the worker.Config's environment
func setkrb5ccname(krb5ccname string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}

// destinationNodes sets the Nodes field in the worker.Config object from the configuration's serviceConfigPath.destinationNodes field
func destinationNodes(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		sc.Nodes = viper.GetStringSlice(serviceConfigPath + ".destinationNodes")
		return nil
	}
}

// account sets the Account field in the worker.Config object from the configuration's serviceConfigPath.account field
func account(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		sc.Account = viper.GetString(serviceConfigPath + ".account")
		return nil
	}
}

// setSchedds sets the Schedds field in the passed-in worker.Config object by querying the condor collector.  It can be overridden
// by setting the serviceConfigPath's condorCreddHostOverride field, in which case that value will be set as the schedd
func setSchedds(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		sc.Schedds = make([]string, 0)

		// If condorCreddHostOverride is set, set the schedd slice to that
		addString := "_condor_CREDD_HOST="
		creddOverrideVar := serviceConfigPath + ".condorCreddHostOverride"
		if viper.IsSet(creddOverrideVar) {
			addString = addString + viper.GetString(creddOverrideVar)
			sc.CommandEnvironment.CondorCreddHost = addString
			sc.Schedds = append(sc.Schedds, viper.GetString(creddOverrideVar))
			return nil
		}

		// Run condor_status to get schedds.
		var collectorHost, constraint string
		if c := viper.GetString(serviceConfigPath + ".condorCollectorHostOverride"); c != "" {
			collectorHost = c
		} else if c := viper.GetString("condorCollectorHost"); c != "" {
			collectorHost = c
		}
		if c := viper.GetString(serviceConfigPath + ".condorScheddConstraintOverride"); c != "" {
			constraint = c
		} else if c := viper.GetString("condorScheddConstraint"); c != "" {
			constraint = c
		}

		statusCmd := condor.NewCommand("condor_status").WithPool(collectorHost).WithConstraint(constraint).WithArg("-schedd")
		classads, err := statusCmd.Run()
		if err != nil {
			log.WithField("command", statusCmd.Cmd().String()).Error("Could not run condor_status to get cluster schedds")

		}

		for _, classad := range classads {
			name := classad["Name"].String()
			sc.Schedds = append(sc.Schedds, name)
		}

		log.WithField("schedds", sc.Schedds).Debug("Set schedds successfully")
		return nil

	}
}

// setDefaultRoleFileDestinationTemplate sets the DefaultRoleFileDestinationTemplate field in the passed in worker.Config object
// to the template that the pushTokenWorker should use when deriving the default role file path on the destination node.
func setDefaultRoleFileDestinationTemplate(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
		defaultRoleFileDestOverridePath := serviceConfigPath + ".defaultRoleFileDestinationTemplateOverride"
		if viper.IsSet(defaultRoleFileDestOverridePath) {
			worker.SetDefaultRoleFileTemplateValueInExtras(sc, viper.GetString(defaultRoleFileDestOverridePath))
		} else {
			notSetBackup := "/tmp/default_role_{{.Experiment}}_{{.DesiredUID}}" // Default role file destination template
			if defaultRoleFileDestinationTplString := viper.GetString("defaultRoleFileDestinationTemplate"); defaultRoleFileDestinationTplString != "" {
				worker.SetDefaultRoleFileTemplateValueInExtras(sc, defaultRoleFileDestinationTplString)
			} else {
				worker.SetDefaultRoleFileTemplateValueInExtras(sc, notSetBackup)
			}
		}
		return nil
	}
}
