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

// Package kerberos contains utilities to obtain kerberos tickets, query the kerberos cache, and switch caches in the case of multiple
// kerberos caches
package kerberos

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"text/template"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
)

var (
	kerberosExecutables = map[string]string{
		"kinit": "",
		"klist": "",
	}
	tracer = otel.Tracer("kerberos")
)

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	if err := utils.CheckForExecutables(kerberosExecutables); err != nil {
		panic(fmt.Errorf("could not find kerberos executables: %w", err))
	}
}

// GetTicket uses the keytabPath and userPrincipal to obtain a kerberos ticket
func GetTicket(ctx context.Context, keytabPath, userPrincipal string, environ environment.CommandEnvironment) error {
	ctx, span := tracer.Start(ctx, "GetTicket")
	span.SetAttributes(
		attribute.String("keytabPath", keytabPath),
		attribute.String("userPrincipal", userPrincipal),
	)
	defer span.End()

	// Parse and execute kinit template
	args, err := parseAndExecuteKinitTemplate(keytabPath, userPrincipal)
	if err != nil {
		err = fmt.Errorf("could not get kerberos ticket: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	// Run kinit to get kerberos ticket from keytab
	createKerberosTicket := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, kerberosExecutables["kinit"], args...)
	span.AddEvent(
		"creating new kerberos ticket with keytabs",
		trace.WithAttributes(attribute.String("command", createKerberosTicket.String())),
	)
	if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
		err = fmt.Errorf("error creating kerberos ticket: %w\n: %s", err, stdoutstdErr)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	tracing.LogSuccessWithTrace(span, "Successfully created new kerberos ticket")
	return nil
}

// CheckPrincipal verifies that the kerberos ticket principal matches checkPrincipal
func CheckPrincipal(ctx context.Context, checkPrincipal string, environ environment.CommandEnvironment) error {
	ctx, span := tracer.Start(ctx, "CheckPrincipal")
	span.SetAttributes(attribute.String("checkPrincipal", checkPrincipal))
	defer span.End()

	checkForKerberosTicket := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, kerberosExecutables["klist"])
	span.AddEvent("finding kerberos ticket with klist")
	stdoutStderr, err := checkForKerberosTicket.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("could not check kerberos principal: %w\n: %s", err, stdoutStderr)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	// Check output of klist to get principal
	span.AddEvent("checking klist output for principal")
	principal, err := getKerberosPrincipalFromKerbListOutput(stdoutStderr)
	if err != nil {
		err = fmt.Errorf("could not get kerberos principal during check: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	span.AddEvent("found principal", trace.WithAttributes(attribute.String("principal", principal)))

	if principal != checkPrincipal {
		err := fmt.Errorf("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", checkPrincipal, principal)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	tracing.LogSuccessWithTrace(span, "Kerberos principal matches configured principal")
	return nil
}

func parseAndExecuteKinitTemplate(keytabPath, userPrincipal string) ([]string, error) {
	kinitTemplate, err := template.New("kinit").Parse("-k -t {{.KeytabPath}} {{.UserPrincipal}}")
	if err != nil {
		return nil, fmt.Errorf("could not parse kinit template: %w", err)
	}

	cArgs := struct{ KeytabPath, UserPrincipal string }{
		KeytabPath:    keytabPath,
		UserPrincipal: userPrincipal,
	}
	args, err := utils.TemplateToCommand(kinitTemplate, cArgs)
	if err != nil {
		var t1 *utils.TemplateExecuteError
		var t2 *utils.TemplateArgsError
		var retErr error
		if errors.As(err, &t1) {
			retErr = fmt.Errorf("could not execute kinit template: %w", err)
		}
		if errors.As(err, &t2) {
			retErr = fmt.Errorf("could not get kinit command arguments from template: %w", err)
		}
		return nil, retErr
	}
	return args, nil
}

func getKerberosPrincipalFromKerbListOutput(output []byte) (string, error) {
	principalCheckRegexp := regexp.MustCompile("Default principal: (.+)")
	matches := principalCheckRegexp.FindSubmatch(output)
	if len(matches) != 2 {
		err := "could not find principal in klist output"
		return "", errors.New(err)
	}
	principal := string(matches[1])
	return principal, nil
}
