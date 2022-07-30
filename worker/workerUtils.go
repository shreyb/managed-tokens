package worker

import (
	"os"
	"os/exec"
	"strings"
)

type SuccessReporter interface {
	GetServiceName() string
	GetSuccess() bool
}

type EnvironmentMapper interface {
	toMap() map[string]string
	toEnvs() map[string]string
}

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
