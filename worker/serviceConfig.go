package worker

import (
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/service"
)

// ServiceConfig is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
//TODO Add Service as a field that is <experiment>_<role>, use it everywhere applicable
type ServiceConfig struct {
	Service           service.Service
	UserPrincipal     string
	Nodes             []string
	Account           string
	KeytabPath        string
	DesiredUID        uint32
	ServiceConfigPath string
	CommandEnvironment
}

// NewServiceConfig takes the config information from the global file and creates an exptConfig object
// To create functional options, simply define functions that operate on an *ServiceConfig.  E.g.
// func foo(e *ServiceConfig) { e.Name = "bar" }.  You can then pass in foo to CreateServiceConfig (e.g.
// NewServiceConfig("my_expt", foo), to set the ServiceConfig.Name to "bar".
//
// To pass in something that's dynamic, define a function that returns a func(*ServiceConfig).   e.g.:
// func foo(bar int, e *ServiceConfig) func(*ServiceConfig) {
//     baz = bar + 3
//     return func(*ServiceConfig) {
//          e.spam = baz
//        }
// If you then pass in foo(3), like NewServiceConfig("my_expt", foo(3)), then ServiceConfig.spam will be set to 6
// Borrowed heavily from https://cdcvs.fnal.gov/redmine/projects/discompsupp/repository/ken_proxy_push/revisions/master/entry/utils/experimentConfig.go
func NewServiceConfig(service service.Service, options ...func(*ServiceConfig) error) (*ServiceConfig, error) {
	c := ServiceConfig{Service: service}

	for _, option := range options {
		err := option(&c)
		if err != nil {
			log.WithField("function", option).Error(err)
			return &c, err
		}
	}
	log.WithFields(log.Fields{
		"experiment": c.Service.Experiment(),
		"role":       c.Service.Role(),
	}).Debug("Set up service config")
	return &c, nil
}
