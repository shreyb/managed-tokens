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

// Package service provides the types and related methods to declare, manage, and configure OAuth services (as defined by the HTCondor project).
// A "service" is used by grid token-retrieving tools to ascertain the correct token issuer, scope, and group memberships that a SciToken should
// contain.
package service

import (
	"regexp"

	log "github.com/sirupsen/logrus"
)

const DefaultRole string = "Analysis"

var serviceWithRolePattern = regexp.MustCompile(`(.+)_([[:alnum:]]+)`)

// Service is implemented by any value that has an experiment name and role, and defines methods for retrieving those from the underlying
type Service interface {
	Experiment() string
	Role() string
	Name() string
}

// NewService takes a serviceName string, parses it into the experiment and role components, and returns an initialized Service object
func NewService(serviceName string) Service {
	s := &service{name: serviceName}
	s.experiment, s.role = ExtractExperimentAndRoleFromServiceName(serviceName)
	log.WithFields(log.Fields{
		"service":    s.name,
		"experiment": s.experiment,
		"role":       s.role,
	}).Debug("Parsed experiment and role from service")
	return s
}

// ExtractExperimentAndRoleFromServiceName parses a service name and returns the experiment and role, assuming the separating
// character between those in the service name is "_"
func ExtractExperimentAndRoleFromServiceName(serviceName string) (string, string) {
	matches := serviceWithRolePattern.FindStringSubmatch(serviceName)
	if len(matches) == 3 {
		return matches[1], matches[2]
	} else {
		log.WithField("service", serviceName).Infof("Service does not include role.  Setting role to defaultRole %s", DefaultRole)
		return serviceName, DefaultRole
	}
}

// service is an unexported type that implements Service
type service struct {
	name       string
	experiment string
	role       string
}

func (s *service) Experiment() string {
	return s.experiment
}

func (s *service) Role() string {
	return s.role
}

func (s *service) Name() string {
	return s.name
}
