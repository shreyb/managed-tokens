package worker

import (
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

// TODO clean up this whole command environment business.  Maybe call it WorkerEnvironment?
type CommandEnvironment struct {
	Krb5ccname          string
	CondorCreddHost     string
	CondorCollectorHost string
	HtgettokenOpts      string
}

func (c *CommandEnvironment) toMap() map[string]string {
	return map[string]string{
		"Krb5ccname":          c.Krb5ccname,
		"CondorCreddHost":     c.CondorCreddHost,
		"CondorCollectorHost": c.CondorCollectorHost,
		"HtgettokenOpts":      c.HtgettokenOpts,
	}
}

func (c *CommandEnvironment) toEnvs() map[string]string {
	return map[string]string{
		"Krb5ccname":          "KRB5CCNAME",
		"CondorCreddHost":     "_condor_CREDD_HOST",
		"CondorCollectorHost": "_condor_COLLECTOR_HOST",
		"HtgettokenOpts":      "HTGETTOKENOPTS",
	}
}

type EnvironmentMapper interface {
	toMap() map[string]string
	toEnvs() map[string]string
}

// ServiceConfig is a mega struct containing all the information the workers need to have or pass onto lower level funcs.
//TODO Add Service as a field that is <experiment>_<role>, use it everywhere applicable
type ServiceConfig struct {
	Experiment        string
	Role              string
	UserPrincipal     string
	Nodes             []string
	Account           string
	KeytabPath        string
	DesiredUID        uint32
	ServiceConfigPath string
	Success           bool
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
		Experiment: expt,
		Role:       role,
	}
	for _, option := range options {
		err := option(&c)
		if err != nil {
			log.WithField("function", option).Error(err)
			return &c, err
		}
	}
	log.WithFields(log.Fields{
		"experiment": c.Experiment,
		"role":       c.Role,
	}).Debug("Set up service config")
	return &c, nil
}

func kerberosEnvironmentWrappedCommand(cmd *exec.Cmd, environ EnvironmentMapper) *exec.Cmd {
	// TODO Make this func so that we can pass in context and args, and it'll return the command with wrapped environ.  So basically the same API as exec.Command plus the CommandEnvironment
	envMapping := environ.toEnvs()
	os.Unsetenv(envMapping["Krb5ccname"])

	cmd.Env = append(
		os.Environ(),
		environ.toMap()["Krb5ccname"],
	)
	return cmd
}

func environmentWrappedCommand(cmd *exec.Cmd, environ EnvironmentMapper) *exec.Cmd {
	// TODO Make this func so that we can pass in context and args, and it'll return the command with wrapped environ.  So basically the same API as exec.Command plus the CommandEnvironment
	for _, val := range environ.toEnvs() {
		os.Unsetenv(val)
	}

	cmd.Env = os.Environ()

	for _, val := range environ.toMap() {
		cmd.Env = append(cmd.Env, val)
	}
	return cmd
}

// Borrowed from hashicorp's vault API, since we ONLY need this func
// Source: https://github.com/hashicorp/vault/blob/main/vault/version_store.go
// and https://github.com/hashicorp/vault/blob/main/sdk/helper/consts/token_consts.go

const (
	ServiceTokenPrefix       = "hvs."
	LegacyServiceTokenPrefix = "s."
)

func IsServiceToken(token string) bool {
	return strings.HasPrefix(token, ServiceTokenPrefix) ||
		strings.HasPrefix(token, LegacyServiceTokenPrefix)
}
