package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
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
func withTLSAuth() func(string, string) (*http.Response, error) {
	return func(url, verb string) (*http.Response, error) {
		caCertSlice := make([]string, 0)
		caCertPool := x509.NewCertPool()

		// Adapted from  https://gist.github.com/michaljemala/d6f4e01c4834bf47a9c4
		// Load host cert
		// TODO review if these errors should be Fatal or just Error
		cert, err := tls.LoadX509KeyPair(
			viper.GetString("ferry.hostCert"),
			viper.GetString("ferry.hostKey"),
		)
		if err != nil {
			log.Fatal(err)
		}

		// Load CA certs
		caFiles, err := ioutil.ReadDir(viper.GetString("ferry.caPath"))
		if err != nil {
			log.WithField("caPath", viper.GetString("ferry.caPath")).Fatal(err)

		}
		for _, f := range caFiles {
			if filepath.Ext(f.Name()) == ".pem" {
				filenameToAdd := path.Join(viper.GetString("ferry.caPath"), f.Name())
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

func withKerberosJWTAuth(serviceConfig *service.Config) func() func(string, string) (*http.Response, error) {
	// This returns a func that returns a func. This was done to have withKerberosJWTAuth(serviceConfig) have the same
	// return type as withTLSAuth.
	return func() func(string, string) (*http.Response, error) {
		return func(url, verb string) (*http.Response, error) {
			// TODO go through this func and figure out if errors should be fatals or errors
			// Get our bearer token and locate it
			if err := utils.GetToken(serviceConfig, viper.GetString("ferry.vaultServer")); err != nil {
				log.Error("Could not get token to authenticate to FERRY")
				return &http.Response{}, err
			}

			bearerTokenDefaultLocation, err := getBearerTokenDefaultLocation()
			if err != nil {
				log.Error("Could not get default location for bearer tokens")
				return &http.Response{}, err
			}
			defer func() {
				if err := os.Remove(bearerTokenDefaultLocation); err != nil {
					log.Error("Could not remove bearer token file")
				}
				log.Info("Removed bearer token file")
			}()

			bearerBytes, err := ioutil.ReadFile(bearerTokenDefaultLocation)
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

			tokenStringRaw := string(bearerBytes[:])
			tokenString := strings.TrimSuffix(tokenStringRaw, "\n")

			bearerHeader := "Bearer " + tokenString

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Error("Could not initialize HTTP request")
				return &http.Response{}, err
			}
			req.Header.Add("Authorization", bearerHeader)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Error("Could not send request")
				log.Error(err)
				return &http.Response{}, err
			}

			return resp, nil
		}
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
		sc.KeytabPath = viper.GetString("ferry.serviceKeytabPath")
		return nil
	}
}

func setUserPrincipalAndHtgettokenopts() func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.UserPrincipal = viper.GetString("ferry.serviceKerberosPrincipal")

		credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")
		// TODO Make htgettokenopts configurable
		htgettokenOptsRaw := []string{
			// "--vaultserver=" + viper.GetString("ferry.vaultServer"),
			// "--outfile=" + bearerTokenFile,
			"--credkey=" + credKey,
		}
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""
		return nil
	}
}

// Other utils
func getBearerTokenDefaultLocation() (string, error) {
	var location string
	if location = os.Getenv("BEARER_TOKEN_FILE"); location != "" {
		return location, nil
	}

	var tempDir string
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Could not get current user")
		return location, err
	}
	currentUID := currentUser.Uid
	filename := fmt.Sprintf("bt_u%s", currentUID)

	if tempDir = os.Getenv("XDG_RUNTIME_DIR"); tempDir == "" {
		tempDir = os.TempDir()
	}

	return path.Join(tempDir, filename), nil
}

func newFERRYServiceConfigWithKerberosAuth(ctx context.Context) (*service.Config, error) {
	var serviceName string

	if viper.GetString("ferry.serviceRole") != "" {
		serviceName = viper.GetString("ferry.serviceExperiment") + "_" + viper.GetString("ferry.serviceRole")
	} else {
		serviceName = viper.GetString("ferry.serviceExperiment")
	}
	s, err := service.NewService(serviceName)
	if err != nil {
		log.WithField("service", serviceName).Fatal("Could not initialize service object")
		return &service.Config{}, err
	}

	// Get krb5ccname directory
	krb5ccname, err := ioutil.TempDir("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}

	serviceConfig, err := service.NewConfig(
		s,
		setkrb5ccname(krb5ccname),
		setKeytabPath(),
		setUserPrincipalAndHtgettokenopts(),
	)
	if err != nil {
		log.Error("Could not create new service configuration")
		return &service.Config{}, err
	}

	// Get kerberos ticket and check it.  If we already have kerberos ticket, use it
	if err := utils.SwitchKerberosCache(ctx, serviceConfig); err != nil {
		log.Warn("No kerberos ticket in cache.  Attempting to get a new one")
		if err := utils.GetKerberosTicket(ctx, serviceConfig); err != nil {
			log.Error("Could not get kerberos ticket to generate JWT")
			return &service.Config{}, err
		}
		if err := utils.CheckKerberosPrincipal(ctx, serviceConfig); err != nil {
			log.Error("Verification of kerberos ticket failed")
			return &service.Config{}, err
		}
	}
	return serviceConfig, nil
}
