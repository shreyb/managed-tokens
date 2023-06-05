// Package worker provides worker functions and types that allow callers to abstract away the lower-level details of the various operations needed
// for the Managed Tokens utilities.  Ideally, most callers should just need to set up worker.Config objects using worker.NewConfig, obtain the worker
// channels using worker.NewChannelsForWorkers, and call the applicable worker with the above ChannelsForWorkers object.  All that remains then for the
// caller is to pass the worker.Config objects into the ChannelsForWorkers.GetServiceConfigChan(), and listen on the ChannelsForWorkers.GetSuccessChan()
// and ChannelsForWorkers.GetNotificationsChan()
package worker

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/service"
)

// unPingableNodes holds the set of nodes that do not respond to a ping request
type unPingableNodes struct {
	sync.Map
}

// TODO DOcument this
type SupportedExtrasKey int

const (
	DefaultRoleFileTemplate SupportedExtrasKey = iota
)

func (s SupportedExtrasKey) String() string {
	switch s {
	case DefaultRoleFileTemplate:
		return "DefaultRoleFileTemplate"
	default:
		return "unsupported extras key"
	}
}

// Config is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
type Config struct {
	service.Service
	UserPrincipal string
	Nodes         []string
	Account       string
	KeytabPath    string
	DesiredUID    uint32
	Schedds       []string
	// TODO UPdate this!
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
	Extras map[SupportedExtrasKey]any
	environment.CommandEnvironment
	*unPingableNodes // Pointer to an unPingableNodes object that indicates which configured nodes in Nodes do not respond to a ping request
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
	c.Extras = make(map[SupportedExtrasKey]any)

	for _, option := range options {
		err := option(&c)
		if err != nil {
			log.Error(err)
			return &c, err
		}
	}

	// Initialize our unPingableNodes field so we don't run into a nil pointer dereference panic later on
	c.unPingableNodes = &unPingableNodes{sync.Map{}}

	log.WithFields(log.Fields{
		"experiment": c.Service.Experiment(),
		"role":       c.Service.Role(),
	}).Debug("Set up service worker config")
	return &c, nil
}

// ServiceNameFromExperimentAndRole returns a reconstructed service name by concatenating the underlying Service.Experiment() value, "_", and
// the underlying Service.Role() value.  This is useful in case the caller has overridden the experiment or role name in the case of duplicate
// services that have different configurations (e.g. the same vault token needs to be pushed to two sets of credds in two different pools)
// In general, this should be used in lieu of Config.Service.Name() for notifications passing
func (c *Config) ServiceNameFromExperimentAndRole() string {
	return c.Service.Experiment() + "_" + c.Service.Role()
}

// RegisterUnpingableNode registers a node in the Config's unPingableNodes field
func (c *Config) RegisterUnpingableNode(node string) {
	c.unPingableNodes.Store(node, struct{}{})
}

// IsNodeUnpingable checks the Config's unPingableNodes field to see if a node is registered there
func (c *Config) IsNodeUnpingable(node string) bool {
	_, ok := c.unPingableNodes.Load(node)
	return ok
}
