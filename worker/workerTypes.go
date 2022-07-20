package worker

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

type CommandEnvironment struct {
	Krb5ccname          string
	CondorCreddHost     string
	CondorCollectorHost string
	HtgettokenOpts      string
}

// ServiceConfig is a mega struct containing all the information the Worker needs to have or pass onto lower level funcs.

type ServiceConfig struct {
	Name              string
	UserPrincipal     string
	Nodes             []string
	Account           string
	Role              string
	KeytabPath        string
	DesiredUID        uint32
	ServiceConfigPath string
	CommandEnvironment
}

// CreateServiceConfig takes the config information from the global file and creates an exptConfig object
// To create functional options, simply define functions that operate on an *ExptConfig.  E.g.
// func foo(e *ExptConfig) { e.Name = "bar" }.  You can then pass in foo to CreateExptConfig (e.g.
// CreateExptConfig("my_expt", foo), to set the ExptConfig.Name to "bar".
//
// To pass in something that's dynamic, define a function that returns a func(*ExptConfig).   e.g.:
// func foo(bar int, e *ExptConfig) func(*ExptConfig) {
//     baz = bar + 3
//     return func(*ExptConfig) {
//          e.spam = baz
//        }
// If you then pass in foo(3), like CreateExptConfig("my_expt", foo(3)), then ExptConfig.spam will be set to 6
// Borrowed heavily from https://cdcvs.fnal.gov/redmine/projects/discompsupp/repository/ken_proxy_push/revisions/master/entry/utils/experimentConfig.go
func NewServiceConfig(expt, role string, options ...func(*ServiceConfig) error) (*ServiceConfig, error) {
	c := ServiceConfig{
		Name: expt,
		Role: role,
	}
	for _, option := range options {
		err := option(&c)
		if err != nil {
			log.WithField("function", option).Error(err)
			return &c, err
		}
	}
	log.WithFields(log.Fields{
		"experiment": c.Name,
		"role":       c.Role,
	}).Debug("Set up service config")
	return &c, nil
}

func kerberosEnvironmentWrappedCommand(cmd *exec.Cmd, environ *CommandEnvironment) *exec.Cmd {
	// TODO Make this func so that we can pass in context and args, and it'll return the command with wrapped environ.  So basically the same API as exec.Command plus the CommandEnvironment

	cmd.Env = append(
		os.Environ(),
		environ.Krb5ccname,
	)
	return cmd
}
