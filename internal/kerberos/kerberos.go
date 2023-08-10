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

var kerberosTemplates = map[string]*template.Template{
	"kinit": template.Must(template.New("kinit").Parse("-k -t {{.KeytabPath}} {{.UserPrincipal}}")),
}

var principalCheckRegexp = regexp.MustCompile("Default principal: (.+)")

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	if err := utils.CheckForExecutables(kerberosExecutables); err != nil {
		log.Fatal("Could not find kerberos executables")
	}
}

// GetTicket uses the keytabPath and userPrincipal to obtain a kerberos ticket
func GetTicket(ctx context.Context, keytabPath, userPrincipal string, environ environment.CommandEnvironment) error {
	cArgs := struct{ KeytabPath, UserPrincipal string }{
		KeytabPath:    keytabPath,
		UserPrincipal: userPrincipal,
	}

	funcLogger := log.WithFields(log.Fields{
		"keytabPath":    keytabPath,
		"userPrincipal": userPrincipal,
	})

	args, err := utils.TemplateToCommand(kerberosTemplates["kinit"], cArgs)
	var t1 *utils.TemplateExecuteError
	if errors.As(err, &t1) {
		retErr := fmt.Errorf("could not execute kinit template: %w", err)
		funcLogger.Error(retErr.Error())
		return retErr
	}
	var t2 *utils.TemplateArgsError
	if errors.As(err, &t2) {
		retErr := fmt.Errorf("could not get kinit command arguments from template: %w", err)
		funcLogger.Error(retErr.Error())
		return retErr
	}

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
	checkForKerberosTicket := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, kerberosExecutables["klist"])
	funcLogger := log.WithField("caller", "CheckKerberosPrincipal")

	funcLogger.Debug("Checking user principal against configured principal")
	if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		funcLogger.Errorf("Error running klist:\n %s", stdoutStderr)
		return err

	} else {
		funcLogger.Debugf("%s", stdoutStderr)

		matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
		if len(matches) != 2 {
			err := "could not find principal in kinit output"
			funcLogger.Error(err)
			return errors.New(err)
		}
		principal := string(matches[1])
		funcLogger.Debugf("Found principal: %s", principal)

		if principal != checkPrincipal {
			err := fmt.Sprintf("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", checkPrincipal, principal)
			funcLogger.Error(err)
			return errors.New(err)
		}
	}
	return nil
}
