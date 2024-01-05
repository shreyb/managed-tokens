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

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
)

var kerberosExecutables = map[string]string{
	"kinit": "",
	"klist": "",
}

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	if err := utils.CheckForExecutables(kerberosExecutables); err != nil {
		log.Fatal("Could not find kerberos executables")
	}
}

// GetTicket uses the keytabPath and userPrincipal to obtain a kerberos ticket
func GetTicket(ctx context.Context, keytabPath, userPrincipal string, environ environment.CommandEnvironment) error {
	funcLogger := log.WithFields(log.Fields{
		"keytabPath":    keytabPath,
		"userPrincipal": userPrincipal,
	})

	// Parse and execute kinit template
	args, err := parseAndExecuteKinitTemplate(keytabPath, userPrincipal)
	if err != nil {
		funcLogger.Error("Could not parse and execute kinit template")
		return err
	}

	// Run kinit to get kerberos ticket from keytab
	createKerberosTicket := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, kerberosExecutables["kinit"], args...)
	funcLogger.WithField("command", createKerberosTicket.String()).Debug("Now creating new kerberos ticket with keytab")
	if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		funcLogger.Error("Error running kinit to create new kerberos ticket")
		funcLogger.Errorf("%s", stdoutstdErr)
		return err
	}
	return nil
}

// CheckPrincipal verifies that the kerberos ticket principal matches checkPrincipal
func CheckPrincipal(ctx context.Context, checkPrincipal string, environ environment.CommandEnvironment) error {
	funcLogger := log.WithField("caller", "CheckKerberosPrincipal")

	checkForKerberosTicket := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, kerberosExecutables["klist"])
	funcLogger.Debug("Checking user principal against configured principal")
	stdoutStderr, err := checkForKerberosTicket.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		funcLogger.Errorf("Error running klist:\n %s", stdoutStderr)
		return err
	}
	funcLogger.Debugf("%s", stdoutStderr)

	// Check output of klist to get principal
	principal, err := getKerberosPrincipalFromKerbListOutput(stdoutStderr)
	if err != nil {
		funcLogger.Error("Could not get kerberos principal")
		return err
	}

	if principal != checkPrincipal {
		err := fmt.Errorf("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", checkPrincipal, principal)
		funcLogger.Error(err)
		return err
	}
	return nil
}

func parseAndExecuteKinitTemplate(keytabPath, userPrincipal string) ([]string, error) {
	kinitTemplate, err := template.New("kinit").Parse("-k -t {{.KeytabPath}} {{.UserPrincipal}}")
	if err != nil {
		log.Error("could not parse kinit template")
		return nil, err
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
		log.Error(retErr.Error())
		return nil, retErr
	}
	return args, nil
}

func getKerberosPrincipalFromKerbListOutput(output []byte) (string, error) {
	principalCheckRegexp := regexp.MustCompile("Default principal: (.+)")
	matches := principalCheckRegexp.FindSubmatch(output)
	if len(matches) != 2 {
		err := "could not find principal in klist output"
		log.Error(err)
		return "", errors.New(err)
	}
	principal := string(matches[1])
	log.Debugf("Found principal: %s", principal)
	return principal, nil
}
