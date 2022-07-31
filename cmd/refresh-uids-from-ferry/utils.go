package main

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
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
func WithJWTAuth(url string) func() (*http.Response, error) {
	return func() (*http.Response, error) {
		// Initialize request like this here, and attach the bearer token header
		// req, err := http.NewRequest("GET", b.String(), nil)
		// if err != nil {
		// 	log.WithField("account", username).Error("Could not initialize HTTP request")
		// }
		// // resp, err := httpsClient.Get(b.String())
		// resp, err := httpsClient.Do(req)
		// if err != nil {
		// 	log.WithField("account", username).Error("Attempt to get UID from FERRY failed")
		// 	log.WithField("account", username).Error(err)
		// 	return
		// }
		return &http.Response{}, nil
	}
}
