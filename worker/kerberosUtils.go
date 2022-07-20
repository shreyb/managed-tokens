package worker

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"text/template"

	"github.com/shreyb/managed-tokens/utils"
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

func getKerberosTicket(sc *ServiceConfig) error {
	// Kinit
	cArgs := struct{ KeytabPath, UserPrincipal string }{
		KeytabPath:    sc.KeytabPath,
		UserPrincipal: sc.UserPrincipal,
	}

	var b strings.Builder
	if err := kerberosTemplates["kinit"].Execute(&b, cArgs); err != nil {
		err := fmt.Sprintf("Could not execute kinit template: %s", err.Error())
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error(err)
		return errors.New(err)
	}

	args, err := utils.GetArgsFromTemplate(b.String())
	if err != nil {
		err := fmt.Sprintf("Could not get kinit command arguments from template: %s", err.Error())
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error(err)
		return errors.New(err)
	}

	createKerberosTicket := exec.Command(kerberosExecutables["kinit"], args...)
	createKerberosTicket = kerberosEnvironmentWrappedCommand(createKerberosTicket, &sc.CommandEnvironment)
	log.Info("Now creating new kerberos ticket with keytab")
	if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error("Error running kinit to create new keytab")
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Errorf("%s", stdoutstdErr)
		return err
	}
	return nil
}

func checkKerberosPrincipal(sc *ServiceConfig) error {
	// Verify principal matches config principal
	checkForKerberosTicket := exec.Command(kerberosExecutables["klist"])
	checkForKerberosTicket = kerberosEnvironmentWrappedCommand(checkForKerberosTicket, &sc.CommandEnvironment)

	log.Info("Checking user principal against configured principal")
	if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error("Error running klist")
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Errorf("%s", stdoutStderr)
		return err

	} else {
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Infof("%s", stdoutStderr)

		matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
		if len(matches) != 2 {
			err := "could not find principal in kinit output"
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error(err)
			return errors.New(err)
		}
		principal := string(matches[1])
		// TODO Make this a debug
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Infof("Found principal: %s", principal)

		if principal != sc.UserPrincipal {
			err := fmt.Sprintf("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", sc.UserPrincipal, principal)
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error(err)
			return errors.New(err)
		}
	}
	return nil
}

func switchKerberosCache(sc *ServiceConfig) error {
	// kswitch
	cArgs := struct{ UserPrincipal string }{
		UserPrincipal: sc.UserPrincipal,
	}

	var b strings.Builder
	if err := kerberosTemplates["kswitch"].Execute(&b, cArgs); err != nil {
		err := fmt.Sprintf("Could not execute kswitch template: %s", err.Error())
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error(err)
		return errors.New(err)
	}

	args, err := utils.GetArgsFromTemplate(b.String())
	if err != nil {
		err := fmt.Sprintf("Could not get kswitch command arguments from template: %s", err.Error())
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error(err)
		return errors.New(err)
	}

	switchkCache := exec.Command(kerberosExecutables["kswitch"], args...)
	switchkCache = kerberosEnvironmentWrappedCommand(switchkCache, &sc.CommandEnvironment)
	log.Info("Now creating new kerberos ticket with keytab")
	if stdoutstdErr, err := switchkCache.CombinedOutput(); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error("Error running kswitch to load proper principal")
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Errorf("%s", stdoutstdErr)
		return err
	}
	return nil
}
