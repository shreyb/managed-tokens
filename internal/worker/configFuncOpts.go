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

package worker

import (
	"github.com/fermitools/managed-tokens/internal/environment"
)

// SetCommandEnvironment is a helper func that takes a variadic of func(*environment.CommandEnvironment) and returns a func(*Config) that sets
// the Config's embedded CommandEnvironment by running the funcs passed in the variadic.  It is meant to be used to as a functional
// opt in the call to NewConfig to create a new Config object.   For example:
//
//	c := NewConfig(
//	  SetCommandEnvironment(
//	    func(e *environment.CommandEnvironment) { e.SetCondorCreddHost("my_credd_host") }
//	  )
//	)
func SetCommandEnvironment(cmdEnvFuncs ...func(e *environment.CommandEnvironment)) func(*Config) error {
	return func(c *Config) error {
		for _, f := range cmdEnvFuncs {
			f(&c.CommandEnvironment)
		}
		return nil
	}
}

// SetUserPrincipal returns a func(*Config) with the UserPrincipal field set to the passed in value
func SetUserPrincipal(value string) func(*Config) error {
	return func(c *Config) error {
		c.UserPrincipal = value
		return nil
	}
}

// SetNodes returns a func(*Config) with the Nodes field set to the passed in value
func SetNodes(value []string) func(*Config) error {
	return func(c *Config) error {
		c.Nodes = value
		return nil
	}
}

// SetAccount returns a func(*Config) with the Account field set to the passed in value
func SetAccount(value string) func(*Config) error {
	return func(c *Config) error {
		c.Account = value
		return nil
	}
}

// SetKeytabPath returns a func(*Config) with the KeytabPath field set to the passed in value
func SetKeytabPath(value string) func(*Config) error {
	return func(c *Config) error {
		c.KeytabPath = value
		return nil
	}
}

// SetDesiredUID returns a func(*Config) with the DesiredUID field set to the passed in value
func SetDesiredUID(value uint32) func(*Config) error {
	return func(c *Config) error {
		c.DesiredUID = uint32(value)
		return nil
	}
}

// SetSchedds returns a func(*Config) with the Schedds field set to the passed in value
func SetSchedds(value []string) func(*Config) error {
	return func(c *Config) error {
		c.Schedds = value
		return nil
	}
}

// SetVaultServer returns a func(*Config) that sets the Config.VaultServer field set to the passed in value
func SetVaultServer(value string) func(*Config) error {
	return func(c *Config) error {
		c.VaultServer = value
		return nil
	}
}

// SetServiceCreddVaultTokenPathRoot returns a func(*Config) that sets the Config.ServiceCreddVaultTokenPathRoot
// field to the passed in value
func SetServiceCreddVaultTokenPathRoot(value string) func(*Config) error {
	return func(c *Config) error {
		c.ServiceCreddVaultTokenPathRoot = value
		return nil
	}
}
