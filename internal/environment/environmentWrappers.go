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

package environment

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("environment")

// The _WrappedCommand funcs have a very similar API to the exec.CommandContext func, except that they also accept a
// *CommandEnvironment, and use it to set the environment of the returned *exec.Cmd

// KerberosEnvironmentWrappedCommand takes an EnvironmentMapper, extracts the kerberos-related environment variables, and
// returns an *exec.Cmd that has those variables in its environment
func KerberosEnvironmentWrappedCommand(ctx context.Context, environ *CommandEnvironment, name string, arg ...string) *exec.Cmd {
	ctx, span := tracer.Start(ctx, "KerberosEnvironmentWrappedCommand")
	span.SetAttributes(
		attribute.String("command", name),
		attribute.StringSlice("args", arg),
	)
	defer span.End()

	os.Unsetenv(Krb5ccname.EnvVarKey())

	cmd := exec.CommandContext(ctx, name, arg...)

	cmd.Env = append(
		os.Environ(),
		environ.GetSetting(Krb5ccname),
	)

	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Prepared command in kerberos environment. Command: %s, Injected Environment: %v", cmd, environ))
	}

	return cmd
}

// EnvironmentWrappedCommand takes an EnvironmentMapper, extracts the environment variables, and returns an *exec.Cmd that has those
// variables in its environment
func EnvironmentWrappedCommand(ctx context.Context, environ *CommandEnvironment, name string, arg ...string) *exec.Cmd {
	ctx, span := tracer.Start(ctx, "EnvironmentWrappedCommand")
	span.SetAttributes(
		attribute.String("command", name),
		attribute.StringSlice("args", arg),
	)
	defer span.End()

	// If any of the supported CommandEnvironment keys are set, unset them now
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		os.Unsetenv(field.EnvVarKey())
	}

	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = os.Environ()

	// Now set the supported CommandEnvironment keys in the cmd's environment to the values in the given CommandEnvironment
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		cmd.Env = append(cmd.Env, environ.GetSetting(field))
	}

	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Prepared command with environment. Command: %s, Injected Environment: %v", cmd, environ))
	}

	return cmd
}
