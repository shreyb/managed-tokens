package service

import (
	"github.com/shreyb/managed-tokens/environment"
	log "github.com/sirupsen/logrus"
)

// Config is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
type Config struct {
	Service
	UserPrincipal string
	Nodes         []string
	Account       string
	KeytabPath    string
	DesiredUID    uint32
	ConfigPath    string
	environment.CommandEnvironment
}

// NewConfig takes the config information from the global file and creates an exptConfig object
// To create functional options, simply define functions that operate on an *Config.  E.g.
// func foo(e *Config) { e.Name = "bar" }.  You can then pass in foo to CreateConfig (e.g.
// NewConfig("my_expt", foo), to set the Config.Name to "bar".
//
// To pass in something that's dynamic, define a function that returns a func(*Config).   e.g.:
// func foo(bar int, e *Config) func(*Config) {
//     baz = bar + 3
//     return func(*Config) {
//          e.spam = baz
//        }
// If you then pass in foo(3), like NewConfig("my_expt", foo(3)), then Config.spam will be set to 6
// Borrowed heavily from https://cdcvs.fnal.gov/redmine/projects/discompsupp/repository/ken_proxy_push/revisions/master/entry/utils/experimentConfig.go
func NewConfig(service Service, options ...func(*Config) error) (*Config, error) {
	c := Config{Service: service}

	for _, option := range options {
		err := option(&c)
		if err != nil {
			log.Error(err)
			return &c, err
		}
	}
	log.WithFields(log.Fields{
		"experiment": c.Service.Experiment(),
		"role":       c.Service.Role(),
	}).Debug("Set up service config")
	return &c, nil
}
