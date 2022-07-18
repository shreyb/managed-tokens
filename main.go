package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"

	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
	// "github.com/rifflock/lfshook"
	// "github.com/spf13/pflag"
	// "github.com/spf13/viper"
	// scitokens "github.com/scitokens/scitokens-go"
	//"github.com/shreyb/managed-tokens/utils"
)

func main() {

	experiment := "dune"
	role := "production"
	account := "dunepro"
	desiredUID := 50762 // TODO Get from FERRY
	//destination := "fermicloud525.fnal.gov"
	destination := "fermicloud525"
	keytabPath := "/home/sbhat/dunepro.keytab.REXBATCH"
	userPrincipal := "dunepro/managedtokens/fifeutilgpvm01.fnal.gov@FNAL.GOV"
	credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")

	condorCreddHost := "dunegpschedd02.fnal.gov"
	condorCollectorHost := "dunegpcoll02.fnal.gov"

	// Check for condor_store_cred executable
	if _, err := exec.LookPath("condor_store_cred"); err != nil {
		log.Warn("Could not find condor_store_cred.  Adding /usr/sbin to $PATH")
		os.Setenv("PATH", "/usr/sbin:$PATH")
	}

	// Get kerberos keytab and verify
	var kerberosExecutables = map[string]string{
		"kinit":    "",
		"klist":    "",
		"kdestroy": "",
	}

	for kExe := range kerberosExecutables {
		kPath, err := exec.LookPath(kExe)
		if err != nil {
			log.Errorf("Could not find executable %s.  Please ensure it exists on your system", kExe)
			os.Exit(1)
		}
		kerberosExecutables[kExe] = kPath
		log.Infof("Using %s executable: %s", kExe, kPath)

	}

	log.Info("Now checking for kerberos ticket and removing old one")
	checkForKerberosTicket := exec.Command(kerberosExecutables["klist"])
	if stdoutStderr, err := checkForKerberosTicket.CombinedOutput(); err != nil {
		log.Warnf("%s", stdoutStderr)
		checkBytes := []byte("klist: No credentials cache found")
		if !bytes.Contains(stdoutStderr, checkBytes) {
			log.Fatalf("%s", stdoutStderr)
		}
	} else {
		destroyKerberosTicket := exec.Command(kerberosExecutables["kdestroy"])
		if err := destroyKerberosTicket.Run(); err != nil {
			log.Fatal(err)
		}
	}

	// Get kerberos ticket, check principal
	principalCheckRegexp := regexp.MustCompile("Default principal: (.+)")
	createKerberosTicket := exec.Command(kerberosExecutables["kinit"], "-k", "-t", keytabPath, userPrincipal)
	log.Info("Now creating new kerberos ticket with keytab")
	if stdoutstdErr, err := createKerberosTicket.CombinedOutput(); err != nil {
		log.Fatalf("%s", stdoutstdErr)
	}
	log.Info("Checking user principal against configured principal")
	checkForKerberosTicket = exec.Command(kerberosExecutables["klist"])
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
		if principal != userPrincipal {
			log.Fatal("klist yielded a principal that did not match the configured user prinicpal.  Expected %s, got %s", userPrincipal, principal)
		}

	}

	// Store token in vault and get new vault token
	htgettokenOpts := []string{
		fmt.Sprintf("--credkey=%s", credKey),
	}

	os.Setenv("HTGETTOKENOPTS", strings.Join(htgettokenOpts, " "))
	os.Setenv("_condor_CREDD_HOST", condorCreddHost)
	os.Setenv("_condor_COLLECTOR_HOST", condorCollectorHost)

	condorVaultStorerExe, err := exec.LookPath("condor_vault_storer")
	if err != nil {
		log.Fatal("Could not find path to condor_vault_storer executable")
	}

	service := fmt.Sprintf("%s_%s", experiment, role)
	getTokensAndStoreInVaultCmd := exec.Command(condorVaultStorerExe, service)
	// if err := getTokensAndStoreInVaultCmd.Run(); err != nil {
	if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
		log.Fatalf("%s", stdoutStderr)
	} else {
		log.Infof("%s", stdoutStderr)
	}

	// TODO Verify token scopes with scitokens lib

	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	currentUID := currentUser.Uid

	sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, service)
	destinationFilenames := []string{
		fmt.Sprintf("/tmp/vt_u%d", desiredUID),
		fmt.Sprintf("/tmp/vt_u%d-%s", desiredUID, service),
	}

	// Send to nodes
	// Import rsync.go (maybe a utils package?)
	for _, destinationFilename := range destinationFilenames {
		rsyncConfig := utils.NewRsyncSetup(
			account,
			destination,
			destinationFilename,
			"",
		)

		if err := rsyncConfig.CopyToDestination(sourceFilename); err != nil {
			log.Errorf("Could not copy file %s to destination %s", sourceFilename, destinationFilename)
			log.Fatal(err)
		}
		log.Infof("Successfully copied file %s to destination %s", sourceFilename, destinationFilename)
	}

}
