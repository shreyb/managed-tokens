package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"path"
	"strings"

	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func LoadServiceConfigsIntoChannel(chanToLoad chan<- *worker.ServiceConfig, serviceConfigs map[string]*worker.ServiceConfig) {
	defer close(chanToLoad)
	for _, sc := range serviceConfigs {
		chanToLoad <- sc
	}
}

func setCondorCreddHost(serviceConfigPath string) func(sc *worker.ServiceConfig) error {
	return func(sc *worker.ServiceConfig) error {
		addString := "_condor_CREDD_HOST="
		overrideVar := serviceConfigPath + ".condorCreddHostOverride"
		if viper.IsSet(overrideVar) {
			addString = addString + viper.GetString(overrideVar)
		} else {
			addString = addString + viper.GetString("condorCreddHost")
		}
		sc.CommandEnvironment.CondorCreddHost = addString
		return nil
	}
}

func setCondorCollectorHost(serviceConfigPath string) func(sc *worker.ServiceConfig) error {
	return func(sc *worker.ServiceConfig) error {
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

func setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, experiment string) func(sc *worker.ServiceConfig) error {
	return func(sc *worker.ServiceConfig) error {
		userPrincipalTemplate, err := template.New("userPrincipal").Parse(viper.GetString("kerberosPrincipalPattern")) // TODO Maybe move this out so it's not evaluated every experiment
		if err != nil {
			log.Error("Error parsing Kerberos Principal Template")
			log.Error(err)
			return err
		}
		userPrincipalOverrideConfigPath := serviceConfigPath + ".userPrincipalOverride"
		if viper.IsSet(userPrincipalOverrideConfigPath) {
			sc.UserPrincipal = viper.GetString(userPrincipalOverrideConfigPath)
		} else {
			var b strings.Builder
			templateArgs := struct{ Account string }{Account: viper.GetString(serviceConfigPath + ".account")}
			if err := userPrincipalTemplate.Execute(&b, templateArgs); err != nil {
				log.WithField("experiment", experiment).Error("Could not execute kerberos prinicpal template")
				return err
			}
			sc.UserPrincipal = b.String()
		}

		credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")
		// TODO Make htgettokenopts configurable
		htgettokenOptsRaw := []string{
			fmt.Sprintf("--credkey=%s", credKey),
		}
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""
		return nil
	}
}

func setKeytabOverride(serviceConfigPath string) func(sc *worker.ServiceConfig) error {
	return func(sc *worker.ServiceConfig) error {
		keytabConfigPath := serviceConfigPath + ".keytabPath"
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

func setDesiredUIByOverrideOrLookup(serviceConfigPath string) func(*worker.ServiceConfig) error {
	return func(sc *worker.ServiceConfig) error {
		if viper.IsSet(serviceConfigPath + ".desiredUIDOverride") {
			sc.DesiredUID = viper.GetUint32(serviceConfigPath + ".desiredUIDOverride")
		} else {
			// Get UID from SQLite DB that should be kept up to date by refresh-uids-from-ferry
			func() {
				var dbLocation string
				var uid int

				username := viper.GetString(serviceConfigPath + ".account")

				if viper.IsSet("dbLocation") {
					dbLocation = viper.GetString("dbLocation")
				} else {
					dbLocation = "/var/lib/managed-tokens/uid.db"

				}
				db, err := sql.Open("sqlite3", dbLocation)
				if err != nil {
					log.Error("Could not open the UID database file")
					log.Error(err)
					return
				}
				defer db.Close()

				uid, err = utils.GetUIDByUsername(db, username)
				if err != nil {
					log.Error("Could not get UID by username")
					return
				}
				log.WithFields(log.Fields{
					"username": username,
					"uid":      uid,
				}).Debug("Got UID")
				sc.DesiredUID = uint32(uid)
			}()
		}
		return nil
	}
}