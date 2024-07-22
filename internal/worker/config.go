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

// Package worker provides worker functions and types that allow callers to abstract away the lower-level details of the various operations needed
// for the Managed Tokens utilities.  Ideally, most callers should just need to set up worker.Config objects using worker.NewConfig, obtain the worker
// channels using worker.NewChannelsForWorkers, and call the applicable worker with the above ChannelsForWorkers object.  All that remains then for the
// caller is to pass the worker.Config objects into the ChannelsForWorkers.GetServiceConfigChan(), and listen on the ChannelsForWorkers.GetSuccessChan()
// and ChannelsForWorkers.GetNotificationsChan()
package worker

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/service"
)

const (
	retryDefault = 0
)

// unPingableNodes holds the set of nodes that do not respond to a ping request
type unPingableNodes struct {
	sync.Map
}

// Config is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
type Config struct {
	service.Service
	UserPrincipal string   // The principal of the kerberos credential needed by the various workers
	Nodes         []string // The destination nodes for PushTokensWorker to push copies of the vault tokens to
	Account       string   // The user account the PushTokensWorker should use to connect to a destination node
	KeytabPath    string   // The path on disk where the kerberos keytab is stored
	// The directory on disk where the vault tokens per credd are stored.  If this path is given, and either does not
	// exist or does not have the relevant token, the utility will assume that the file/directory does not exist and
	// create it
	ServiceCreddVaultTokenPathRoot string
	DesiredUID                     uint32   // The UID associated with the Account. This determines the destination filename
	Schedds                        []string // The list of schedds/credds where a StoreAndGetTokenWorker should store vault tokens
	VaultServer                    string   // The vault server hosting the Hashicorp Vault that the refresh token should be saved to
	// Extras is a map where any value can be stored that may not fit into the above categories.
	// To allow an external package to set an Extras value, define an exported func that sets
	// the value directly.  For example:
	//  func SetSupportedExtrasKeyValue(c *Config, key supportedExtrasKey, value any) { c.Extras[key] = value }
	//
	// The functional option version of this would look like:
	//  func SetSupportedExtrasKeyValue(key supportedExtrasKey, value any) func (*Config) { return func(c *Config) {c.Extras[key] = value } }
	//
	// It's also a good idea to create a getter value for each supportedExtrasKey, so that type checks can be done.  For example, for a key whose
	// value's underlying type we expect to be a string, define a func like this:
	//  func GetMySupportedExtrasKey(c *Config) (string, bool) {
	// 		if val, ok := c.Extras[MySupportedKey].(string); ok {
	//  			return val, ok
	//  	}
	//  }
	// Then the caller should check ok to make sure it's true before using the value
	Extras map[supportedExtrasKey]any
	// workerSpecificConfig is a map of values that are specific to a worker type.  This is useful for setting values that are specific to a worker
	workerSpecificConfig map[WorkerType]any
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
func NewConfig(service service.Service, options ...ConfigOption) (*Config, error) {
	c := &Config{Service: service}
	c.Extras = make(map[supportedExtrasKey]any)
	c.workerSpecificConfig = initializeWorkerSpecificConfigDefaults()

	for _, option := range options {
		cBackup := backupConfig(c)
		err := option(c)
		if err != nil {
			log.Error(err)
			c = cBackup
			return c, err
		}
	}

	// Initialize our unPingableNodes field so we don't run into a nil pointer dereference panic later on
	c.unPingableNodes = &unPingableNodes{sync.Map{}}

	log.WithFields(log.Fields{
		"experiment": c.Service.Experiment(),
		"role":       c.Service.Role(),
	}).Debug("Set up service worker config")
	return c, nil
}

func backupConfig(c1 *Config) *Config {
	c2 := &Config{
		Service:                        c1.Service,
		UserPrincipal:                  c1.UserPrincipal,
		Nodes:                          c1.Nodes,
		Account:                        c1.Account,
		KeytabPath:                     c1.KeytabPath,
		ServiceCreddVaultTokenPathRoot: c1.ServiceCreddVaultTokenPathRoot,
		DesiredUID:                     c1.DesiredUID,
		Schedds:                        c1.Schedds,
		VaultServer:                    c1.VaultServer,
		Extras:                         c1.Extras,
		CommandEnvironment:             c1.CommandEnvironment,
		unPingableNodes:                c1.unPingableNodes,
	}
	return c2
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

// initializeWorkerSpecificConfigDefaults initializes and returns a map of default configuration values for each worker type.
func initializeWorkerSpecificConfigDefaults() map[WorkerType]any {
	m := make(map[WorkerType]any, 0)
	m[GetKerberosTicketsWorkerType] = retryDefault
	m[StoreAndGetTokenWorkerType] = retryDefault
	m[PingAggregatorWorkerType] = retryDefault
	m[PushTokensWorkerType] = retryDefault
	return m
}
