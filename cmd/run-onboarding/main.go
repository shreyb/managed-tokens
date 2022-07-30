package main

import (

	// "os/user"

	"io/ioutil"
	"os"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	"github.com/shreyb/managed-tokens/worker"

	"github.com/rifflock/lfshook"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func init() {
	const configFile string = "managedTokens"

	if err := utils.CheckRunningUserNotRoot(); err != nil {
		log.Fatal("Current user is root.  Please run this executable as a non-root user")
	}

	// Defaults
	viper.SetDefault("notifications.admin_email", "fife-group@fnal.gov")

	// Parse our command-line arguments
	pflag.Usage = onboardingUsage
	pflag.StringP("configfile", "c", "", "Specify alternate config file")
	pflag.Bool("version", false, "Version of Managed Tokens library")
	pflag.String("admin", "", "Override the config file admin email")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)
	if pflag.NArg() != 0 {
		viper.Set("service", pflag.Arg(0))
	}

	// If no experiment is set, exit, since we only want to onboard a single experiment at a time
	if viper.GetString("service") == "" {
		log.Error("A service must be given on the command line for run-onboarding")
		onboardingUsage()
		os.Exit(1)

	}

	// Get config file
	// Check for override
	if viper.GetString("configfile") != "" {
		viper.SetConfigFile(viper.GetString("configfile"))
	} else {
		viper.SetConfigName(configFile)
	}

	viper.AddConfigPath("/etc/managed-tokens/")
	viper.AddConfigPath("$HOME/.managed-tokens/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Panicf("Fatal error reading in config file: %w", err)
	}

	// Set up logs
	log.SetLevel(log.DebugLevel)
	debugLogConfigLookup := "logs.run-onboarding.debugfile"
	logConfigLookup := "logs.run-onboarding.logfile"
	// Debug log
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.DebugLevel: viper.GetString(debugLogConfigLookup),
		log.InfoLevel:  viper.GetString(debugLogConfigLookup),
		log.WarnLevel:  viper.GetString(debugLogConfigLookup),
		log.ErrorLevel: viper.GetString(debugLogConfigLookup),
		log.FatalLevel: viper.GetString(debugLogConfigLookup),
		log.PanicLevel: viper.GetString(debugLogConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	// Info log file
	log.AddHook(lfshook.NewHook(lfshook.PathMap{
		log.InfoLevel:  viper.GetString(logConfigLookup),
		log.WarnLevel:  viper.GetString(logConfigLookup),
		log.ErrorLevel: viper.GetString(logConfigLookup),
		log.FatalLevel: viper.GetString(logConfigLookup),
		log.PanicLevel: viper.GetString(logConfigLookup),
	}, &log.TextFormatter{FullTimestamp: true}))

	// log.Debugf("Using config file %s", viper.ConfigFileUsed())
	log.Infof("Using config file %s", viper.ConfigFileUsed())

	// Test flag sets which notifications section from config we want to use.
	if viper.GetBool("test") {
		log.Info("Running in test mode")
	}
}

func main() {
	var serviceConfig *worker.ServiceConfig
	// TODO delete any generated vault token

	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}
	defer func() {
		os.RemoveAll(krb5ccname)
		log.Info("Cleared kerberos cache")
	}()

	// All my channels
	serviceConfigsForKinit := make(chan *worker.ServiceConfig, 1)
	kerberosTicketsDone := make(chan worker.SuccessReporter)

	// Get Kerberos tickets
	go worker.GetKerberosTicketsWorker(serviceConfigsForKinit, kerberosTicketsDone)

	func() {
		defer close(serviceConfigsForKinit)
		s, err := service.NewService(viper.GetString("service"))
		if err != nil {
			log.WithField(
				"service",
				viper.GetString("service"),
			).Fatal("Could not parse service properly.  Please ensure that the service follows the format laid out in the help text.")
		}

		serviceConfigPath := "experiments." + s.Experiment() + ".roles." + s.Role()
		serviceConfig, err = worker.NewServiceConfig(
			s,
			serviceConfigViperPath(serviceConfigPath),
			setkrb5ccname(krb5ccname),
			setCondorCreddHost(serviceConfigPath),
			setCondorCollectorHost(serviceConfigPath),
			setUserPrincipalAndHtgettokenoptsOverride(serviceConfigPath, s.Experiment()),
			setKeytabOverride(serviceConfigPath),
			account(serviceConfigPath),
		)
		if err != nil {
			// Something more descriptive
			log.WithFields(log.Fields{
				"experiment": s.Experiment(),
				"role":       s.Role(),
			}).Fatal("Could not create config for service")
		}

		serviceConfigsForKinit <- serviceConfig
	}()

	// If we couldn't get a kerberos ticket for a service, we don't want to try to get vault
	// tokens for that service
	for kerberosTicketSuccess := range kerberosTicketsDone {
		if !kerberosTicketSuccess.GetSuccess() {
			log.WithField(
				"service", kerberosTicketSuccess.GetServiceName(),
			).Error("Failed to obtain kerberos ticket. Stopping onboarding")
			return
		}
	}

	if err := worker.StoreAndGetRefreshAndVaultTokens(serviceConfig); err != nil {
		log.WithFields(log.Fields{
			"experiment": serviceConfig.Service.Experiment(),
			"role":       serviceConfig.Service.Role(),
		}).Error("Could not generate refresh tokens and store vault token for service")
		return
	}

	log.WithField("service", serviceConfig.Service.Name()).Info("Successfully generated refresh and vault tokens")
}
