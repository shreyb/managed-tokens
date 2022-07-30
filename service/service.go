package service

import (
	"errors"
	"regexp"

	log "github.com/sirupsen/logrus"
)

var servicePattern = regexp.MustCompile(`([[:alnum:]]+)_([[:alnum:]]+)`)

type Service interface {
	Experiment() string
	Role() string
	Name() string
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

func NewService(serviceName string) (Service, error) {
	s := &service{}
	matches := servicePattern.FindStringSubmatch(serviceName)
	if len(matches) < 3 {
		msg := "could not parse experiment and role from service"
		log.WithField("service", serviceName).Error(msg)
		return s, errors.New(msg)
	}

	log.WithFields(log.Fields{
		"service":    serviceName,
		"experiment": matches[1],
		"role":       matches[2],
	}).Debug("Parsed experiment and role from service")
	s.name = serviceName
	s.experiment = matches[1]
	s.role = matches[2]

	return s, nil
}
