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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	jwtLib "github.com/lestrrat-go/jwx/jwt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/vaultToken"
	"github.com/shreyb/managed-tokens/internal/worker"
)

// Supported FERRY Authentication methods
type supportedFERRYAuthMethod string

const (
	tlsAuth supportedFERRYAuthMethod = "tls"
	jwtAuth supportedFERRYAuthMethod = "jwt"
)

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

// withKerberosJWTAuth uses a configured worker.Config to return a func that gets a bearer token,
// and uses it to send an HTTP request to the passed in url
func withKerberosJWTAuth(serviceConfig *worker.Config) func() func(context.Context, string, string) (*http.Response, error) {
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
			if _, err := jwtLib.Parse(bearerBytes); err != nil {
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
