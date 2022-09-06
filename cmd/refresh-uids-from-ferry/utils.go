package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	"github.com/lestrrat-go/jwx/jwt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/vaultToken"
	"github.com/shreyb/managed-tokens/internal/worker"
)

// getAllAccountsFromConfig reads the configuration file and gets a slice of accounts
func getAllAccountsFromConfig() []string {
	s := make([]string, 0)

	for experiment := range viper.GetStringMap("experiments") {
		roleConfigPath := "experiments." + experiment + ".roles"
		for role := range viper.GetStringMap(roleConfigPath) {
			accountConfigPath := roleConfigPath + "." + role + ".account"
			account := viper.GetString(accountConfigPath)
			log.WithField("account", account).Debug("Found account")
			s = append(s, account)
		}
	}
	return s
}

// withTLSAuth uses the passed in certificate and key paths (hostCert, hostKey), and
// path to a directory of CA certificates (caPath), to return a func that initializes
// a TLS-secured *http.Client, send an HTTP request to a url, and returns the *http.Response object
func withTLSAuth() func(context.Context, string, string) (*http.Response, error) {
	return func(ctx context.Context, url, verb string) (*http.Response, error) {
		caCertSlice := make([]string, 0)
		caCertPool := x509.NewCertPool()

		// Adapted from  https://gist.github.com/michaljemala/d6f4e01c4834bf47a9c4
		// Load host cert
		cert, err := tls.LoadX509KeyPair(
			viper.GetString("ferry.hostCert"),
			viper.GetString("ferry.hostKey"),
		)
		if err != nil {
			log.Error(err)
			return &http.Response{}, err
		}

		// Load CA certs
		caFiles, err := os.ReadDir(viper.GetString("ferry.caPath"))
		if err != nil {
			log.WithField("caPath", viper.GetString("ferry.caPath")).Error(err)
			return &http.Response{}, err
		}
		for _, f := range caFiles {
			if filepath.Ext(f.Name()) == ".pem" {
				filenameToAdd := path.Join(viper.GetString("ferry.caPath"), f.Name())
				caCertSlice = append(caCertSlice, filenameToAdd)
			}
		}
		for _, f := range caCertSlice {
			caCert, err := os.ReadFile(f)
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

// withKerberosJWTAuth uses a configured service.Config to return a func that gets a bearer token,
// and uses it to send an HTTP request to the passed in url
func withKerberosJWTAuth(serviceConfig *service.Config) func() func(context.Context, string, string) (*http.Response, error) {
	// This returns a func that returns a func. This was done to have withKerberosJWTAuth(serviceConfig) have the same
	// return type as withTLSAuth.
	return func() func(context.Context, string, string) (*http.Response, error) {
		return func(ctx context.Context, url, verb string) (*http.Response, error) {
			// Get our bearer token and locate it
			if err := vaultToken.GetToken(
				ctx,
				serviceConfig.Service.Name(),
				serviceConfig.UserPrincipal,
				viper.GetString("ferry.vaultServer"),
				serviceConfig.CommandEnvironment,
			); err != nil {
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

			bearerBytes, err := os.ReadFile(bearerTokenDefaultLocation)
			if err != nil {
				log.Errorf("Could not open bearer token file for reading, %s", err)
				return &http.Response{}, err
			}

			// Validate token
			if _, err := jwt.Parse(bearerBytes); err != nil {
				log.Errorf("Token validation failed: not a valid bearer (JWT) token, %s", err)
				return &http.Response{}, err
			}

			tokenStringRaw := string(bearerBytes[:])
			tokenString := strings.TrimSuffix(tokenStringRaw, "\n")

			bearerHeader := "Bearer " + tokenString

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Errorf("Could not initialize HTTP request, %s", err)
				return &http.Response{}, err
			}
			req.Header.Add("Authorization", bearerHeader)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Errorf("Could not send request, %s", err)
				return &http.Response{}, err
			}

			return resp, nil
		}
	}
}

// Functional options

// setkrb5ccname sets the KRB5CCNAME directory environment variable in the service.Config's
// environment
func setkrb5ccname(krb5ccname string) func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
		return nil
	}
}

// setKeytabPath sets the location of a service.Config's kerberos keytab
func setKeytabPath() func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.KeytabPath = viper.GetString("ferry.serviceKeytabPath")
		return nil
	}
}

// setUserPrincipalAndHtgettokenopts sets a service.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable
func setUserPrincipalAndHtgettokenopts() func(sc *service.Config) error {
	return func(sc *service.Config) error {
		sc.UserPrincipal = viper.GetString("ferry.serviceKerberosPrincipal")

		credKey := strings.ReplaceAll(sc.UserPrincipal, "@FNAL.GOV", "")
		// TODO Make htgettokenopts configurable
		htgettokenOptsRaw := []string{
			"--credkey=" + credKey,
		}
		sc.CommandEnvironment.HtgettokenOpts = "HTGETTOKENOPTS=\"" + strings.Join(htgettokenOptsRaw, " ") + "\""
		return nil
	}
}

// Other utils

// getBearerTokenDefaultLocation returns the default location of the bearer token
// by looking first at the environment variable BEARER_TOKEN_FILE, and then
// using the current user's UID to find the default location for the bearer token
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

// newFERRYServiceConfigWithKerberosAuth uses the configuration file to return a *service.Config
// with kerberos credentials initialized
func newFERRYServiceConfigWithKerberosAuth(ctx context.Context) (*service.Config, error) {
	var serviceName string

	if viper.GetString("ferry.serviceRole") != "" {
		serviceName = viper.GetString("ferry.serviceExperiment") + "_" + viper.GetString("ferry.serviceRole")
	} else {
		serviceName = viper.GetString("ferry.serviceExperiment")
	}
	s := service.NewService(serviceName)

	// Get krb5ccname directory
	krb5ccname, err := os.MkdirTemp("", "managed-tokens/internal")
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
	if err := kerberos.SwitchCache(ctx, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
		log.Warn("No kerberos ticket in cache.  Attempting to get a new one")
		if err := kerberos.GetTicket(ctx, serviceConfig.KeytabPath, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
			log.Error("Could not get kerberos ticket to generate JWT")
			return &service.Config{}, err
		}
		if err := kerberos.CheckPrincipal(ctx, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
			log.Error("Verification of kerberos ticket failed")
			return &service.Config{}, err
		}
	}
	return serviceConfig, nil
}

// checkFerryDataInDB compares two slices of db.FERRYUIDDatum, to ensure that the dbData
// slice contains all of the data in the ferryData slice
func checkFerryDataInDB(ferryData, dbData []db.FerryUIDDatum) bool {
	type datum struct {
		username string
		uid      int
	}

	ferrySlice := make([]datum, 0, len(ferryData))
	for _, d := range ferryData {
		ferrySlice = append(
			ferrySlice,
			datum{
				username: d.Username(),
				uid:      d.Uid(),
			},
		)
	}
	dbSlice := make([]datum, 0, len(dbData))
	for _, d := range dbData {
		dbSlice = append(
			dbSlice,
			datum{
				username: d.Username(),
				uid:      d.Uid(),
			},
		)
	}

	if err := utils.IsSliceSubSlice(ferrySlice, dbSlice); err != nil {
		log.Errorf("Verification of INSERT failed: %s", err)
		return false
	}
	return true
}

// getAndAggregateFERRYData takes a username and a function that sets up authentication,
// authFunc.  It spins up a worker to get data from FERRY, and then puts that data into
// a channel for aggregation.
func getAndAggregateFERRYData(ctx context.Context, username string, authFunc func() func(context.Context, string, string) (*http.Response, error),
	ferryDataChan chan<- db.FerryUIDDatum) {
	var ferryRequestContext context.Context
	if timeout, ok := timeouts["ferryrequesttimeout"]; ok {
		ferryRequestContext = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		ferryRequestContext = ctx
	}
	entry, err := worker.GetFERRYUIDData(
		ferryRequestContext,
		username,
		viper.GetString("ferry.host"),
		viper.GetInt("ferry.port"),
		authFunc(),
		ferryDataChan,
	)
	if err != nil {
		msg := "Could not get FERRY UID data"
		log.WithField("username", username).Error(msg)
		notificationsChan <- notifications.NewSetupError(msg+" for user "+username, currentExecutable)
	} else {
		ferryDataChan <- entry
	}
}
