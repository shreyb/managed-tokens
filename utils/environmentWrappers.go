package utils

import (
	"context"
	"os"
	"os/exec"
)

func kerberosEnvironmentWrappedCommand(ctx context.Context, environ EnvironmentMapper, name string, arg ...string) *exec.Cmd {
	envMapping := environ.ToEnvs()
	os.Unsetenv(envMapping["Krb5ccname"])

	cmd := exec.CommandContext(ctx, name, arg...)

	cmd.Env = append(
		os.Environ(),
		environ.ToMap()["Krb5ccname"],
	)

	return cmd
}

func environmentWrappedCommand(ctx context.Context, environ EnvironmentMapper, name string, arg ...string) *exec.Cmd {
	for _, val := range environ.ToEnvs() {
		os.Unsetenv(val)
	}

	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = os.Environ()

	for _, val := range environ.ToMap() {
		cmd.Env = append(cmd.Env, val)
	}
	return cmd
}
