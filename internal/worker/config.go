// Package worker provides worker functions and types that allow callers to abstract away the lower-level details of the various operations needed
// for the Managed Tokens utilities.  Ideally, most callers should just need to set up worker.Config objects using worker.NewConfig, obtain the worker
// channels using worker.NewChannelsForWorkers, and call the applicable worker with the above ChannelsForWorkers object.  All that remains then for the
// caller is to pass the worker.Config objects into the ChannelsForWorkers.GetServiceConfigChan(), and listen on the ChannelsForWorkers.GetSuccessChan()
// and ChannelsForWorkers.GetNotificationsChan()
package worker

import (
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/service"
)

// Config is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
type Config struct {
	service.Service
	UserPrincipal string
	Nodes         []string
	Account       string
	KeytabPath    string
	DesiredUID    uint32
	Schedds       []string
	// Extras is a map where any value can be stored that may not fit into the above categories.
	// However, to avoid runtime errors/bad data, it is strongly suggested to create setter/getter
	// funcs that set these values, and run type checks.  For example, if we wanted to set
	// a string property with key "foo" here, we should have two funcs:
	//	func SetFooInExtras(c *Config, fooValue string) { c.Extras["foo"] = fooValue }
	// and
	//	func GetFooFromExtras(c *Config) (string, bool) {
	//		val, ok := c.Extras["foo"].(string)
	//		return val, ok
	//	}
	// The code that tries to retrieve Extras["foo"] can then just call GetFooFromExtras and check
	// the bool value to make sure it's true
	Extras map[string]any
	environment.CommandEnvironment
}

// NewConfig takes the config information from the global file and creates an *Config object
// To create functional options, simply define functions that operate on an *Config.  E.g.
// func foo(e *Config) { e.Name = "bar" }.  You can then pass in foo to CreateConfig (e.g.
// NewConfig("my_expt", foo), to set the Config.Name to "bar".
//
// To pass in something that's dynamic, define a function that returns a func(*Config).   e.g.:
//
//	func foo(bar int, e *Config) func(*Config) {
//		baz = bar + 3
//		return func(*Config) {
//			e.spam = baz
//		}
//
// If you then pass in foo(3), like NewConfig("my_expt", foo(3)), then Config.spam will be set to 6
// Borrowed heavily from https://cdcvs.fnal.gov/redmine/projects/discompsupp/repository/ken_proxy_push/revisions/master/entry/utils/experimentConfig.go
func NewConfig(service service.Service, options ...func(*Config) error) (*Config, error) {
	c := Config{Service: service}
	c.Extras = make(map[string]any)

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
	}).Debug("Set up service worker config")
	return &c, nil
}
