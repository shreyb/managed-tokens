package worker

import (
	"github.com/shreyb/managed-tokens/internal/environment"
)

// To call, for example, do something like this in package main
//
//	f := func(e *environment.CommandEnvironment) {
//	 e.SetCondorCreddHost("my_credd_host")
//	}
//	g := func(e *environment.CommandEnvironment) {
//	 e.SetCondorCollectorHost("my_collector_host")
//	}
//
//	c := worker.NewConfig(
//	 service,
//	 worker.SetCommandEnvironment(f, g)
//	)
//
// TODO Document these funcs, including the example above
func SetCommandEnvironment(cmdEnvFuncs ...func(e *environment.CommandEnvironment)) func(*Config) error {
	return func(c *Config) error {
		for _, f := range cmdEnvFuncs {
			f(&c.CommandEnvironment)
		}
		return nil
	}
}

func SetUserPrincipal(value string) func(*Config) error {
	return func(c *Config) error {
		c.UserPrincipal = value
		return nil
	}
}

func SetNodes(value []string) func(*Config) error {
	return func(c *Config) error {
		c.Nodes = value
		return nil
	}
}

func SetAccount(value string) func(*Config) error {
	return func(c *Config) error {
		c.Account = value
		return nil
	}
}

func SetKeytabPath(value string) func(*Config) error {
	return func(c *Config) error {
		c.KeytabPath = value
		return nil
	}
}

func SetDesiredUID(value uint32) func(*Config) error {
	return func(c *Config) error {
		c.DesiredUID = uint32(value)
		return nil
	}
}

func SetSchedds(value []string) func(*Config) error {
	return func(c *Config) error {
		c.Schedds = value
		return nil
	}
}

func SetSupportedExtrasKeyValue(key SupportedExtrasKey, value any) func(*Config) error {
	return func(c *Config) error {
		c.Extras[key] = value
		return nil
	}
}
