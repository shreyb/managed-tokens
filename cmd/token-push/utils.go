package main

import (
	"context"
	"fmt"
	"html/template"
	"path"
	"strings"

	"github.com/shreyb/managed-tokens/db"
	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func startServiceConfigWorkerForProcessing(ctx context.Context, workerFunc func(context.Context, worker.ChannelsForWorkers),
	serviceConfigs map[string]*service.Config, timeoutCheckKey string) worker.ChannelsForWorkers {
	// Channels, context, and worker for getting kerberos tickets
	var useCtx context.Context
	channels := worker.NewChannelsForWorkers(len(serviceConfigs))
	registerWorkerNotificationChans(channels.GetNotificationsChan())
	if timeout, ok := timeouts[timeoutCheckKey]; ok {
		useCtx = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		useCtx = ctx
	}
	go workerFunc(useCtx, channels)
	if len(serviceConfigs) > 0 { // We add this check because if there are no serviceConfigs, don't load them into any channel
		loadServiceConfigsIntoChannel(channels.GetServiceConfigChan(), serviceConfigs)
	}
	return channels
}

func loadServiceConfigsIntoChannel(chanToLoad chan<- *service.Config, serviceConfigs map[string]*service.Config) {
	defer close(chanToLoad)
	for _, sc := range serviceConfigs {
		chanToLoad <- sc
	}
}

// Functional options for service.Config initialization
func setCondorCreddHost(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
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

func setCondorCollectorHost(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
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

// setUserPrincipal blah blah.  It also adds the proper --credkey flag to the *service.Config's CommandEnvironment.HtgettokenOpts string
func setUserPrincipal(serviceConfigPath, experiment string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
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

func setKeytabOverride(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
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

func setDesiredUIByOverrideOrLookup(ctx context.Context, serviceConfigPath string) func(*service.Config) error {
	return func(sc *service.Config) error {
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

				ferryUidDb, err := db.OpenOrCreateDatabase(dbLocation)
				if err != nil {
					log.WithField("executable", currentExecutable).Fatal("Could not open or create FERRYUIDDatabase")
				}
				defer ferryUidDb.Close()

				uid, err = ferryUidDb.GetUIDByUsername(ctx, username)
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

func serviceConfigViperPath(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.ConfigPath = serviceConfigPath
		return nil
	}
}

func setkrb5ccname(krb5ccname string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}

func destinationNodes(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.Nodes = viper.GetStringSlice(serviceConfigPath + ".destinationNodes")
		return nil
	}
}

func account(serviceConfigPath string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.Account = viper.GetString(serviceConfigPath + ".account")
		return nil
	}
}

func removeFailedServiceConfigs(chans worker.ChannelsForWorkers, serviceConfigs map[string]*service.Config) []*service.Config {
	failedConfigs := make([]*service.Config, 0, len(serviceConfigs))
	for workerSuccess := range chans.GetSuccessChan() {
		if !workerSuccess.GetSuccess() {
			log.WithField(
				"service", workerSuccess.GetServiceName(),
			).Debug("Removing serviceConfig from list of configs to use")
			failedConfigs = append(failedConfigs, serviceConfigs[workerSuccess.GetServiceName()])
			delete(serviceConfigs, workerSuccess.GetServiceName())
		}
	}
	return failedConfigs
}
