package cmdUtils

import (
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/service"
)

// ExperimentOverriddenService is a service where the experiment is overridden.  We want to monitor/act on the config key, but use
// the service name that might duplicate another service.
type ExperimentOverriddenService struct {
	// Service should contain the actual experiment name (the overridden experiment name), not the configuration key
	service.Service
	// ConfigExperiment is the configuration key under the experiments section where this
	// experiment can be found
	ConfigExperiment string
	// ConfigService is the service obtained by using the configExperiment concatenated with an underscore, and Service.Role()
	ConfigService string
}

// NewExperimentOverriddenService returns a new *ExperimentOverriddenService by using the service name and configuration key
func NewExperimentOverriddenService(serviceName, configKey string) *ExperimentOverriddenService {
	s := service.NewService(serviceName)
	return &ExperimentOverriddenService{
		Service:          s,
		ConfigExperiment: configKey,
		ConfigService:    configKey + "_" + s.Role(),
	}
}

func (e *ExperimentOverriddenService) Experiment() string { return e.ConfigExperiment }
func (e *ExperimentOverriddenService) Role() string       { return e.Service.Role() }

// Name returns the ExperimentOverriddenService's Service.Name field
func (e *ExperimentOverriddenService) Name() string { return e.Service.Name() }

// ConfigName returns the value stored in the ConfigService key, meant to be a concatenation
// of the return value of the Experiment() method, "_", and the return value of the Role() method
// The reason for having this separate method is to avoid duplicated service names for
// multiple experiment configurations that have the same overridden experiment values and roles
// but are meant to be handled independently, for example, for different condor pools
func (e *ExperimentOverriddenService) ConfigName() string { return e.ConfigService }

// GetServiceName type checks the service.Service passed in, and returns the appropriate service name for registration
// and logging purposes.
func GetServiceName(s service.Service) string {
	if serv, ok := s.(*ExperimentOverriddenService); ok {
		return serv.ConfigName()
	}
	return s.Name()
}

// CheckExperimentOverride checks the configuration for a given experiment to see if it has an "experimentOverride" key defined.
// If it does, it will return that override value.  Else, it will return the passed in experiment string
func CheckExperimentOverride(experiment string) string {
	if override := viper.GetString("experiments." + experiment + ".experimentOverride"); override != "" {
		return override
	}
	return experiment
}
