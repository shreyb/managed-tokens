package main

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/lestrrat-go/jwx/jwt"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
)

func getAllAccountsFromConfig() []string {
	s := make([]string, 0)

	for experiment := range viper.GetStringMap("experiments") {
		roleConfigPath := "experiments." + experiment + ".roles"
		for role := range viper.GetStringMap(roleConfigPath) {
			accountConfigPath := roleConfigPath + "." + role + ".account"
			account := viper.GetString(accountConfigPath)
			log.WithField("account", account).Info("Found account")
			s = append(s, account)
		}
	}
	return s
}

// withTLSAuth uses the passed in certificate ane key paths (hostCert, hostKey), and
// path to a directory of CA certificates (caPath), to return a func that initializes
// a TLS-secured *http.Client, send an HTTP request to a url, and returns the *http.Response object
func withTLSAuth(hostCert, hostKey, caPath string) func(string, string) (*http.Response, error) {
	return func(url, verb string) (*http.Response, error) {
		caCertSlice := make([]string, 0)
		caCertPool := x509.NewCertPool()

		// Adapted from  https://gist.github.com/michaljemala/d6f4e01c4834bf47a9c4
		// Load host cert
		// TODO review if these errors should be Fatal or just Error
		cert, err := tls.LoadX509KeyPair(hostCert, hostKey)
		if err != nil {
			log.Fatal(err)
		}

		// Load CA certs
		caFiles, err := ioutil.ReadDir(caPath)
		if err != nil {
			log.WithField("caPath", caPath).Fatal(err)

		}
		for _, f := range caFiles {
			if filepath.Ext(f.Name()) == ".pem" {
				filenameToAdd := path.Join(caPath, f.Name())
				caCertSlice = append(caCertSlice, filenameToAdd)
			}
		}
		for _, f := range caCertSlice {
			caCert, err := ioutil.ReadFile(f)
			if err != nil {
				log.WithField("filename", f).Warn(err)
			}
			caCertPool.AppendCertsFromPEM(caCert)
		}

		// Setup HTTPS client
		tlsConfig := &tls.Config{
			Certificates:  []tls.Certificate{cert},
			RootCAs:       caCertPool,
			Renegotiation: tls.RenegotiateFreelyAsClient,
		}
		transport := &http.Transport{TLSClientConfig: tlsConfig}
		client := &http.Client{Transport: transport}

		// Now send the request
		if verb == "" {
			// Default value for HTTP verb
			verb = "GET"
		}
		req, err := http.NewRequest(strings.ToUpper(verb), url, nil)
		if err != nil {
			log.WithField("account", url).Error("Could not initialize HTTP request")
		}
		resp, err := client.Do(req)
		if err != nil {
			log.WithFields(log.Fields{
				"url":        url,
				"verb":       "GET",
				"authMethod": "cert",
			}).Error("Error executing HTTP request")
			log.WithField("url", url).Error(err)
		}
		return resp, err
	}
}

// TODO Similarly, WithTokenAuth should get a bearer token, initialize the client, prepare and send the request using the bearer token in the
// header, and return the response text as is.
// We then pass this into GetFERRYUIDData so the function call looks like GetFERRYUIDData(WithTokenAuth, username)
// Each of these funcs may need to be wrapped so we take the necessary parameters and return a func() *http.Response
//
// TODO
// Figure out what info we need here, like maybe htgettoken executable path,
// kerberos info to run kinit on, maybe viper config if this is moved into executable package
// main

func withKerberosJWTAuth() func(string, string) (*http.Response, error) {
	return func(url, verb string) (*http.Response, error) {
		// TODO go through this func and figure out if errors should be fatals or errors
		var serviceName string

		if viper.GetString("viper.serviceRole") != "" {
			serviceName = viper.GetString("ferry.serviceExperiment") + "_" + viper.GetString("ferry.serviceRole")
		} else {
			serviceName = viper.GetString("ferry.serviceExperiment")
		}
		s, err := service.NewService(serviceName)
		if err != nil {
			log.WithField("service", serviceName).Fatal("Could not initialize service object")
		}

		// Get krb5ccname directory
		krb5ccname, err := ioutil.TempDir("", "managed-tokens")
		if err != nil {
			log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
		}
		defer func() {
			os.RemoveAll(krb5ccname)
			log.Info("Cleared kerberos cache")
		}()

		// Tempfile for bearer token
		bearerTokenFile, err := ioutil.TempFile("", "managed-tokens")
		if err != nil {
			log.Error("Could not create tempfile to store bearer token")
			return &http.Response{}, err
		}
		defer func() {
			os.Remove(bearerTokenFile.Name())
			log.Info("Removed bearer token")
		}()

		serviceConfig, err := service.NewConfig(
			s,
			setkrb5ccname(krb5ccname),
			setKeytabPath(),
			setUserPrincipalAndHtgettokenopts(bearerTokenFile.Name()),
		)
		if err != nil {
			log.Error("Could not create new service configuration")
			return &http.Response{}, err
		}

		// Get kerberos ticket and check it
		if err := utils.GetKerberosTicket(serviceConfig); err != nil {
			log.Error("Could not get kerberos ticket to generate JWT")
			return &http.Response{}, err
		}
		if err := utils.CheckKerberosPrincipal(serviceConfig); err != nil {
			log.Error("Verification of kerberos ticket failed")
			return &http.Response{}, err
		}

		// Get our token
		if err := utils.GetToken(serviceConfig); err != nil {
			log.Error("Could not get token to authenticate to FERRY")
			return &http.Response{}, err
		}

		// Get bearer token file.  Maybe vaildate with jwt library? TODO
		bearerBytes, err := ioutil.ReadFile(bearerTokenFile.Name())
		if err != nil {
			log.Error("Could not open bearer token file for reading")
			log.Error(err)
			return &http.Response{}, err
		}

		// Validate token
		if _, err := jwt.Parse(bearerBytes); err != nil {
			log.Error("Token validation failed: not a valid bearer (JWT) token")
			log.Error(err)
			return &http.Response{}, err
		}

		bearerHeader := "Bearer " + string(bearerBytes[:])

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Error("Could not initialize HTTP request")
			return &http.Response{}, err
		}
		req.Header.Add("Authorization", bearerHeader)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Error("Could not send request")
			log.Error(err)
			return &http.Response{}, err
		}

		// kinit
		// set command environment with credkey, kerb cache, htgettokenopts w/ outfile
		// htgettoken -a fermicloud543.fnal.gov -i dune_production
		// Read bearer token into header
		// Initialize request like this here, and attach the bearer token header
		return resp, nil
	}
}

// Functional options
func setkrb5ccname(krb5ccname string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}

func setKeytabPath() func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.KeytabPath = viper.GetString("serviceKeytabPath")
		return nil
	}
}

func setUserPrincipalAndHtgettokenopts(bearerTokenFile string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.UserPrincipal = viper.GetString("ferry.serviceKerberosPrincipal")

		credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")
		// TODO Make htgettokenopts configurable
		htgettokenOptsRaw := []string{
			"--credkey=" + credKey,
			"--outfile=" + bearerTokenFile,
			"--vaultserver=" + viper.GetString("ferry.vaultServer"),
		}
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""
		return nil
	}
}
