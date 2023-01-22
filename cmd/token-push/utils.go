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
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/worker"
)

// startServiceConfigWorkerForProcessing starts up a worker using the provided workerFunc, gives it a set of channels to receive *worker.Configs
// and send notification.Notifications on, and sends *worker.Configs to the worker
func startServiceConfigWorkerForProcessing(ctx context.Context, workerFunc func(context.Context, worker.ChannelsForWorkers),
	serviceConfigs map[string]*worker.Config, timeoutCheckKey string) worker.ChannelsForWorkers {
	// Channels, context, and worker for getting kerberos tickets
	var useCtx context.Context
	channels := worker.NewChannelsForWorkers(len(serviceConfigs))
	startListenerOnWorkerNotificationChans(ctx, channels.GetNotificationsChan())
	if timeout, ok := timeouts[timeoutCheckKey]; ok {
		useCtx = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		useCtx = ctx
	}
	go workerFunc(useCtx, channels)
	if len(serviceConfigs) > 0 { // We add this check because if there are no serviceConfigs, don't load them into any channel
		serviceConfigSlice := make([]*worker.Config, 0, len(serviceConfigs))
		for _, sc := range serviceConfigs {
			serviceConfigSlice = append(serviceConfigSlice, sc)
		}
		loadServiceConfigsIntoChannel(channels.GetServiceConfigChan(), serviceConfigSlice)
	}
	return channels
}

// loadServiceConfigsIntoChannel loads *worker.Config objects into a channel, usually for use by a worker, and then closes the channel
func loadServiceConfigsIntoChannel(chanToLoad chan<- *worker.Config, serviceConfigSlice []*worker.Config) {
	defer close(chanToLoad)
	for _, sc := range serviceConfigs {
		chanToLoad <- sc
	}
}

// removeFailedServiceConfigs reads the worker.SuccessReporter chan from the passed in worker.ChannelsForWorkers object, and
// removes any *worker.Config objects from the passed in serviceConfigs map.  It returns a slice of the *worker.Configs that
// were removed
func removeFailedServiceConfigs(chans worker.ChannelsForWorkers, serviceConfigs map[string]*worker.Config) []*worker.Config {
	failedConfigs := make([]*worker.Config, 0, len(serviceConfigs))
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
func setUserPrincipal(serviceConfigPath, experiment string) func(sc *worker.Config) error {
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

		if viper.IsSet("htgettokenopts") {
			htgettokenOptsRaw = viper.GetString("htgettokenopts")
		} else {
			htgettokenOptsRaw = "--credkey=" + credKey
		}
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + htgettokenOptsRaw + "\""
		return nil
	}
}

// setKeytabOverride checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab for a worker.Config
func setKeytabOverride(serviceConfigPath string) func(sc *worker.Config) error {
	return func(sc *worker.Config) error {
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
