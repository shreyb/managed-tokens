package utils

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"text/template"

	log "github.com/sirupsen/logrus"
)

var kerberosExecutables = map[string]string{
	"kinit":   "",
	"klist":   "",
	"kswitch": "",
}

var kerberosTemplates = map[string]*template.Template{
	"kinit":   template.Must(template.New("kinit").Parse("-k -t {{.KeytabPath}} {{.UserPrincipal}}")),
	"kswitch": template.Must(template.New("kswitch").Parse("-p {{.UserPrincipal}}")),
}

var principalCheckRegexp = regexp.MustCompile("Default principal: (.+)")

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	if err := CheckForExecutables(kerberosExecutables); err != nil {
		log.Fatal("Could not find kerberos executables")
	}
}

func GetKerberosTicket(ctx context.Context, keytabPath, userPrincipal string, environment CommandEnvironment) error {
	// Kinit
	cArgs := struct{ KeytabPath, UserPrincipal string }{
		KeytabPath:    keytabPath,
		UserPrincipal: userPrincipal,
	}

	args, err := templateToCommand(kerberosTemplates["kinit"], cArgs)
	var t1 *templateExecuteError
	if errors.As(err, &t1) {
		retErr := fmt.Errorf("could not execute kinit template: %w", err)
		log.WithFields(log.Fields{
			"keytabPath":    keytabPath,
			"userPrincipal": userPrincipal,
		}).Error(retErr.Error())
		return retErr
	}
	var t2 *templateArgsError
	if errors.As(err, &t2) {
		retErr := fmt.Errorf("could not get kinit command arguments from template: %w", err)
		log.WithFields(log.Fields{
			"keytabPath":    keytabPath,
			"userPrincipal": userPrincipal,
		}).Error(retErr.Error())
		return retErr
	}

	createKerberosTicket := exec.CommandContext(ctx, kerberosExecutables["kinit"], args...)
	createKerberosTicket = kerberosEnvironmentWrappedCommand(createKerberosTicket, &environment)
	log.Info("Now creating new kerberos ticket with keytab")
	if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithFields(log.Fields{
				"keytabPath":    keytabPath,
				"userPrincipal": userPrincipal,
			}).Error("Context timeout")
			return ctx.Err()
		}
		log.WithFields(log.Fields{
			"keytabPath":    keytabPath,
			"userPrincipal": userPrincipal,
		}).Error("Error running kinit to create new keytab")
		log.WithFields(log.Fields{
			"keytabPath":    keytabPath,
			"userPrincipal": userPrincipal,
		}).Errorf("%s", stdoutstdErr)
		return err
	}
	return nil

}

// func CheckKerberosPrincipalForServiceConfig(ctx context.Context, sc *service.Config) error {
func CheckKerberosPrincipal(ctx context.Context, checkPrincipal string, environment CommandEnvironment) error {
	// Verify principal matches config principal
	checkForKerberosTicket := exec.CommandContext(ctx, kerberosExecutables["klist"])
	checkForKerberosTicket = kerberosEnvironmentWrappedCommand(checkForKerberosTicket, &environment)

	log.WithFields(log.Fields{}).Info("Checking user principal against configured principal")
	if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("caller", "CheckKerberosPrincipal").Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("caller", "CheckKerberosPrincipal").Errorf("Error running klist:\n %s", stdoutStderr)
		return err

	} else {
		log.WithField("caller", "CheckKerberosPrincipal").Infof("%s", stdoutStderr)

		matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
		if len(matches) != 2 {
			err := "could not find principal in kinit output"
			log.WithField("caller", "CheckKerberosPrincipal").Error(err)
			return errors.New(err)
		}
		principal := string(matches[1])
		// TODO Make this a debug
		log.WithField("caller", "CheckKerberosPrincipal").Infof("Found principal: %s", principal)

		if principal != checkPrincipal {
			err := fmt.Sprintf("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", checkPrincipal, principal)
			log.WithField("caller", "CheckKerberosPrincipal").Error(err)
			return errors.New(err)
		}
	}
	return nil
}

func SwitchKerberosCache(ctx context.Context, userPrincipal string, environment CommandEnvironment) error {
	// kswitch
	cArgs := struct{ UserPrincipal string }{
		UserPrincipal: userPrincipal,
	}

	args, err := templateToCommand(kerberosTemplates["kswitch"], cArgs)
	if err != nil {
		var t1 *templateExecuteError
		if errors.As(err, &t1) {
			retErr := fmt.Errorf("could not execute kswitch template: %w", err)
			log.WithField("userPrincipal", userPrincipal).Error(retErr.Error())
			return retErr
		}
		var t2 *templateArgsError
		if errors.As(err, &t2) {
			retErr := fmt.Errorf("could not get kswitch command arguments from template: %w", err)
			log.WithField("userPrincipal", userPrincipal).Error(retErr.Error())
			return retErr
		}
	}

	switchkCache := exec.CommandContext(ctx, kerberosExecutables["kswitch"], args...)
	switchkCache = kerberosEnvironmentWrappedCommand(switchkCache, &environment)
	if stdoutstdErr, err := switchkCache.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithFields(log.Fields{
				"caller":        "SwitchKerberosCache",
				"userPrincipal": userPrincipal,
			}).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("userPrincipal", userPrincipal).Errorf("Error running kswitch to load proper principal: %s", stdoutstdErr)
		return err
	}
	return nil
}
