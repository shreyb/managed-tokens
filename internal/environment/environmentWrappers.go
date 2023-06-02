package environment

import (
	"context"
	"os"
	"os/exec"
)

// The _WrappedCommand funcs have a very similar API to the exec.CommandContext func, except that they also accept an
// EnvironmentMapper, and use it to set the environment of the returned *exec.Cmd

// KerberosEnvironmentWrappedCommand takes an EnvironmentMapper, extracts the kerberos-related environment variables, and
// returns an *exec.Cmd that has those variables in its environment
func KerberosEnvironmentWrappedCommand(ctx context.Context, environ EnvironmentMapper, name string, arg ...string) *exec.Cmd {
	envMapping := environ.toEnvs()
	os.Unsetenv(envMapping["Krb5ccname"])

	cmd := exec.CommandContext(ctx, name, arg...)

	cmd.Env = append(
		os.Environ(),
		environ.ToMap()["Krb5ccname"],
	)

	return cmd
}

// EnvironmentWrappedCommand takes an EnvironmentMapper, extracts the environment variables, and returns an *exec.Cmd that has those
// variables in its environment
func EnvironmentWrappedCommand(ctx context.Context, environ EnvironmentMapper, name string, arg ...string) *exec.Cmd {
	for _, val := range environ.toEnvs() {
		os.Unsetenv(val)
	}

	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = os.Environ()

	for _, val := range environ.ToMap() {
		cmd.Env = append(cmd.Env, val)
	}
	return cmd
}
