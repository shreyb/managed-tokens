package service

import (
	"regexp"

	log "github.com/sirupsen/logrus"
)

const DefaultRole string = "Analysis"

var serviceWithRolePattern = regexp.MustCompile(`([[:alnum:]]+)_([[:alnum:]]+)`)

type Service interface {
	Experiment() string
	Role() string
	Name() string
}

func NewService(serviceName string) (Service, error) {
	s := &service{}

	s.name = serviceName
	matches := serviceWithRolePattern.FindStringSubmatch(serviceName)
	if len(matches) == 3 {
		s.experiment = matches[1]
		s.role = matches[2]
	} else {
		log.WithField("service", serviceName).Infof("Service does not include role.  Setting role to defaultRole %s", DefaultRole)
		s.experiment = serviceName
		s.role = DefaultRole
	}

	log.WithFields(log.Fields{
		"service":    s.name,
		"experiment": s.experiment,
		"role":       s.role,
	}).Debug("Parsed experiment and role from service")

	return s, nil
}

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
