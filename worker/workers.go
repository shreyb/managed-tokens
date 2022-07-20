package worker

import (
	"os"
	"os/exec"
	"regexp"

	log "github.com/sirupsen/logrus"
)

// Get kerberos keytab and verify
var kerberosExecutables = map[string]string{
	"kinit":   "",
	"klist":   "",
	"kswitch": "",
}

var principalCheckRegexp = regexp.MustCompile("Default principal: (.+)")

func init() {
	for kExe := range kerberosExecutables {
		kPath, err := exec.LookPath(kExe)
		if err != nil {
			log.Errorf("Could not find executable %s.  Please ensure it exists on your system", kExe)
			os.Exit(1)
		}
		kerberosExecutables[kExe] = kPath
		log.Infof("Using %s executable: %s", kExe, kPath)
	}
}

func GetKerberosTicketsWorker(inputChan <-chan *ServiceConfig, doneChan chan<- struct{}) {
	defer close(doneChan)
	for sc := range inputChan {

		// Kinit
		createKerberosTicket := exec.Command(kerberosExecutables["kinit"], "-k", "-t", sc.KeytabPath, sc.UserPrincipal)
		createKerberosTicket = kerberosEnvironmentWrappedCommand(createKerberosTicket, &sc.CommandEnvironment)
		log.Info("Now creating new kerberos ticket with keytab")
		if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
			log.Fatalf("%s", stdoutstdErr)
		}

		// Verify principal matches config principal
		checkForKerberosTicket := exec.Command(kerberosExecutables["klist"])
		checkForKerberosTicket = kerberosEnvironmentWrappedCommand(checkForKerberosTicket, &sc.CommandEnvironment)
		log.Info("Checking user principal against configured principal")
		if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
			log.Fatal(err)
		} else {
			log.Infof("%s", stdoutStderr)
			matches := principalCheckRegexp.FindSubmatch(stdoutStderr)
			if len(matches) != 2 {
				log.Fatal("Could not find principal in kinit output")
			}
			principal := string(matches[1])
			log.Infof("Found principal: %s", principal)
			if principal != sc.UserPrincipal {
				log.Fatal("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", sc.UserPrincipal, principal)
			}
		}
	}
}
