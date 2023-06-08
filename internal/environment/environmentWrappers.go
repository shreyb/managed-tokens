package environment

import (
	"context"
	"os"
	"os/exec"
)

// The _WrappedCommand funcs have a very similar API to the exec.CommandContext func, except that they also accept a
// *CommandEnvironment, and use it to set the environment of the returned *exec.Cmd

// KerberosEnvironmentWrappedCommand takes an EnvironmentMapper, extracts the kerberos-related environment variables, and
// returns an *exec.Cmd that has those variables in its environment
func KerberosEnvironmentWrappedCommand(ctx context.Context, environ *CommandEnvironment, name string, arg ...string) *exec.Cmd {
	os.Unsetenv(Krb5ccname.envVarKey())

	cmd := exec.CommandContext(ctx, name, arg...)

	cmd.Env = append(
		os.Environ(),
		environ.GetSetting(Krb5ccname),
	)

	return cmd
}

// EnvironmentWrappedCommand takes an EnvironmentMapper, extracts the environment variables, and returns an *exec.Cmd that has those
// variables in its environment
func EnvironmentWrappedCommand(ctx context.Context, environ *CommandEnvironment, name string, arg ...string) *exec.Cmd {
	// If any of the supported CommandEnvironment keys are set, unset them now
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		os.Unsetenv(field.envVarKey())
	}

	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = os.Environ()

	// Now set the supported CommandEnvironment keys in the cmd's environment to the values in the given CommandEnvironment
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		cmd.Env = append(cmd.Env, environ.GetSetting(field))
	}

	return cmd
}
