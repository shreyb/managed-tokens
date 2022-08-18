package utils

import (
	"os"
	"os/exec"
)

func kerberosEnvironmentWrappedCommand(cmd *exec.Cmd, environ EnvironmentMapper) *exec.Cmd {
	// TODO Make this func so that we can pass in context and args, and it'll return the command with wrapped environ.  So basically the same API as exec.Command plus the CommandEnvironment
	envMapping := environ.ToEnvs()
	os.Unsetenv(envMapping["Krb5ccname"])

	cmd.Env = append(
		os.Environ(),
		environ.ToMap()["Krb5ccname"],
	)
	return cmd
}

func environmentWrappedCommand(cmd *exec.Cmd, environ EnvironmentMapper) *exec.Cmd {
	// TODO Make this func so that we can pass in context and args, and it'll return the command with wrapped environ.  So basically the same API as exec.Command plus the CommandEnvironment
	for _, val := range environ.ToEnvs() {
		os.Unsetenv(val)
	}

	cmd.Env = os.Environ()

	for _, val := range environ.ToMap() {
		cmd.Env = append(cmd.Env, val)
	}
	return cmd
}
