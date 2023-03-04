package main

import (
	"fmt"
	"html/template"
	"os"
	"path"
	"strings"
	"sync"

	condor "github.com/retzkek/htcondor-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/worker"
)

var once sync.Once

// Custom usage function for positional argument.
func onboardingUsage() {
	fmt.Printf("Usage: %s [OPTIONS] service...\n", os.Args[0])
	fmt.Printf("service must be of the form 'experiment_role', e.g. 'dune_production'\n")
	pflag.PrintDefaults()
}

// checkExperimentOverride checks the configuration for a given experiment to see if it has an "experimentOverride" key defined.
// If it does, it will return that override value.  Else, it will return the passed in experiment string
func checkExperimentOverride(experiment string) string {
	if override := viper.GetString("experiments." + experiment + ".experimentOverride"); override != "" {
		return override
	}
	return experiment
}

// ExperimentOverriddenService is a service where the experiment is overridden.  We want to monitor/act on the config key, but use
// the service name that might duplicate another service.
type ExperimentOverriddenService struct {
	// Service should contain the actual experiment name (the overridden experiment name), not the configuration key
	service.Service
	// configExperiment is the configuration key under the experiments section where this
	// experiment can be found
	configExperiment string
	// configService is the service obtained by using the configExperiment concatenated with an underscore, and Service.Role()
	configService string
}

// newExperimentOverridenService returns a new *ExperimentOverridenService by using the service name and configuration key
func newExperimentOverridenService(serviceName, configKey string) *ExperimentOverriddenService {
	s := service.NewService(serviceName)
	return &ExperimentOverriddenService{
		Service:          s,
		configExperiment: configKey,
		configService:    configKey + "_" + s.Role(),
	}
}

func (e *ExperimentOverriddenService) Experiment() string { return e.configExperiment }
func (e *ExperimentOverriddenService) Role() string       { return e.Service.Role() }

// Name returns the ExperimentOverriddenService's Service.Name field
func (e *ExperimentOverriddenService) Name() string { return e.Service.Name() }

// ConfigName returns the value stored in the configService key, meant to be a concatenation
// of the return value of the Experiment() method, "_", and the return value of the Role() method
// The reason for having this separate method is to avoid duplicated service names for
// multiple experiment configurations that have the same overridden experiment values and roles
// but are meant to be handled independently, for example, for different condor pools
func (e *ExperimentOverriddenService) ConfigName() string { return e.configService }

// getServiceName type checks the service.Service passed in, and returns the appropriate service name for registration
// and logging purposes
func getServiceName(s service.Service) string {
	if serv, ok := s.(*ExperimentOverriddenService); ok {
		return serv.ConfigName()
	}
	return s.Name()
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
func setUserPrincipalAndHtgettokenopts(serviceConfigPath, experiment string) func(c *worker.Config) error {
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
			defaultLifetimeString := "10s"
			if viper.IsSet("minTokenLifetime") {
				lifetimeString = viper.GetString("minTokenLifetime")
			} else {
				lifetimeString = defaultLifetimeString
			}

			htgettokenOptsRaw = "--vaulttokenminttl=" + lifetimeString + " --credkey=" + credKey
		}

		log.Debugf("Final HTGETTOKENOPTS: %s", htgettokenOptsRaw)
		c.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=" + htgettokenOptsRaw
		return nil
	}
}

// setKeytabOverride checks the configuration at the serviceConfigPath for an override for the path to the kerberos keytab.
// If the override does not exist, it uses the configuration to calculate the default path to the keytab for a worker.Config
func setKeytabOverride(serviceConfigPath string) func(c *worker.Config) error {
	return func(c *worker.Config) error {
		keytabConfigPath := serviceConfigPath + ".keytabPathOverride"
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
