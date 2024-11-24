// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/spf13/viper"

	"github.com/fermitools/managed-tokens/internal/service"
)

// experimentOverriddenService is a service where the experiment is overridden.  We want to monitor/act on the config key, but use
// the service name that might duplicate another service.
type experimentOverriddenService struct {
	// Service should contain the actual experiment name (the overridden experiment name), not the configuration key
	service.Service
	// configExperiment is the configuration key under the experiments section where this
	// experiment can be found
	configExperiment string
	// configService is the service obtained by using the configExperiment concatenated with an underscore, and Service.Role()
	configService string
}

// NewExperimentOverriddenService returns a new *experimentOverriddenService by using the service name and configuration key that corresponds
// to the name of the experiment in the configuration.  For example, if there is a configuration:
// experiments:
//
//	experiment1:
//		roles:
//			role1:
//				foo: bar
//	experiment2:
//		experimentOverride: experiment1
//		roles:
//			role1:
//				foo: baz
//
// Then NewExperimentOverriddenService("experiment1_role1", "experiment2") would return an experimentOverriddenService
// whose Service field would have "experiment1" and "role1" as the experiment and role, respectively; whose configExperiment field would be "experiment2",
// and whose configService field would be "experiment2_role1".
//
// Further, the returned experimentOverriddenService's Experiment() method would return "experiment2" rather than "experiment1",
// the Role() method would return "role1", and the Name() method would return "experiment1_role1"
func newExperimentOverriddenService(serviceName, configKey string) *experimentOverriddenService {
	s := service.NewService(serviceName)
	return &experimentOverriddenService{
		Service:          s,
		configExperiment: configKey,
		configService:    configKey + "_" + s.Role(),
	}
}

// Experiment returns the experimentOverriddenService's name that is guaranteed to be unique across all services
func (e *experimentOverriddenService) Experiment() string { return e.configExperiment }
func (e *experimentOverriddenService) Role() string       { return e.Service.Role() }

// Name returns the experimentOverriddenService's Service.Name field.  If there is another service with the same experiment name in the
// configuration file, this may not be a unique value across all services.
func (e *experimentOverriddenService) Name() string { return e.Service.Name() }

// configName returns the value stored in the configService key, meant to be a concatenation
// of the return value of the Experiment() method, "_", and the return value of the Role() method
// The reason for having this separate method is to avoid duplicated service names for
// multiple experiment configurations that have the same overridden experiment values and roles
// but are meant to be handled independently, for example, for different condor pools
func (e *experimentOverriddenService) configName() string { return e.configService }

// GetServiceName type checks the service.Service passed in, and returns the appropriate service name for registration
// and logging purposes.
func GetServiceName(s service.Service) string {
	if serv, ok := s.(*experimentOverriddenService); ok {
		return serv.configName()
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
