package worker

import "github.com/shreyb/managed-tokens/internal/environment"

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
func SetCommandEnvironment(cmdEnvFuncs ...func(e *environment.CommandEnvironment)) func(*Config) {
	return func(c *Config) {
		for _, f := range cmdEnvFuncs {
			f(&c.CommandEnvironment)
		}
	}
}

func SetUserPrincipal(value string) func(*Config) {
	return func(c *Config) { c.UserPrincipal = value }
}

func SetNodes(value []string) func(*Config) {
	return func(c *Config) { c.Nodes = value }
}

func SetAccount(value string) func(*Config) {
	return func(c *Config) { c.Account = value }
}

func SetKeytabPath(value string) func(*Config) {
	return func(c *Config) { c.KeytabPath = value }
}

func SetDesiredUID(value int) func(*Config) {
	return func(c *Config) { c.DesiredUID = uint32(value) }
}

func SetDesiredSchedds(value []string) func(*Config) {
	return func(c *Config) { c.Schedds = value }
}
