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

// ConfigOption is a functional option that should be used as an argument to NewConfig to set various fields
// of the Config
// For example:
//
//	 f := func(c *Config) error {
//		  c.Account = "myaccount"
//	   return nil
//	 }
//	 g := func(c *Config) error {
//		  c.DesiredUID = 42
//	   return nil
//	 }
//	 config := NewConfig("myservice", f, g)
type ConfigOption func(*Config) error

// SetCommandEnvironment is a helper func that takes a variadic of func(*environment.CommandEnvironment) and returns a func(*Config) that sets
// the Config's embedded CommandEnvironment by running the funcs passed in the variadic.  It is meant to be used to as a functional
// opt in the call to NewConfig to create a new Config object.   For example:
//
//	c := NewConfig(
//	  SetCommandEnvironment(
//	    func(e *environment.CommandEnvironment) { e.SetCondorCreddHost("my_credd_host") }
//	  )
//	)
func SetCommandEnvironment(cmdEnvFuncs ...func(e *environment.CommandEnvironment)) ConfigOption {
	return ConfigOption(func(c *Config) error {
		for _, f := range cmdEnvFuncs {
			f(&c.CommandEnvironment)
		}
		return nil
	})
}

func SetUserPrincipal(value string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.UserPrincipal = value
		return nil
	})
}

func SetNodes(value []string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.Nodes = value
		return nil
	})
}

func SetAccount(value string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.Account = value
		return nil
	})
}

func SetKeytabPath(value string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.KeytabPath = value
		return nil
	})
}

func SetDesiredUID(value uint32) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.DesiredUID = uint32(value)
		return nil
	})
}

func SetSchedds(value []string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.Schedds = value
		return nil
	})
}

func SetVaultServer(value string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.VaultServer = value
		return nil
	})
}

func SetServiceCreddVaultTokenPathRoot(value string) ConfigOption {
	return ConfigOption(func(c *Config) error {
		c.ServiceCreddVaultTokenPathRoot = value
		return nil
	})
}
