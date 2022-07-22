package utils

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"path"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

type UIDEntryFromFerry struct {
	Username string
	Uid      int
}

// InitializeHTTPSClientForFerry sets up the HTTPS client to query the FERRY service
func InitializeHTTPSClient(hostCert, hostKey, caPath string) *http.Client {
	log.Debug("Initializing client to query FERRY")
	caCertSlice := make([]string, 0)
	caCertPool := x509.NewCertPool()

	// Adapted from  https://gist.github.com/michaljemala/d6f4e01c4834bf47a9c4
	// Load host cert
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

	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport}
}
