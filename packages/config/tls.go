package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"slices"
)

// TLS is the transport section every NeuroMesh listener and client shares.
//
// Insecure is the only way to run without certificates, and every service logs
// a warning at startup when it is set. There is no silent plaintext fallback:
// a missing certificate file is a startup error that names the file.
type TLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	CAFile   string `yaml:"ca_file"`
	Insecure bool   `yaml:"insecure"`
	// AllowedClientNames, when set on a server that requires client
	// certificates, restricts which certificate common names may connect.
	// Chain verification alone says the caller holds a certificate from the
	// platform CA; this says which service it is.
	AllowedClientNames []string `yaml:"allowed_client_names"`
}

// ServerConfig builds the listener side. With requireClientCert, the peer must
// present a certificate chaining to CAFile, and its common name must be in
// AllowedClientNames when that list is non-empty.
func (t TLS) ServerConfig(requireClientCert bool) (*tls.Config, error) {
	if t.Insecure {
		return nil, errors.New("config: ServerConfig called with tls.insecure set")
	}
	cert, err := t.keyPair()
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}
	if !requireClientCert {
		return cfg, nil
	}
	pool, err := t.caPool()
	if err != nil {
		return nil, err
	}
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	cfg.ClientCAs = pool
	if len(t.AllowedClientNames) > 0 {
		allowed := slices.Clone(t.AllowedClientNames)
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("tls: no client certificate")
			}
			cn := cs.PeerCertificates[0].Subject.CommonName
			if !slices.Contains(allowed, cn) {
				return fmt.Errorf("tls: client %q is not an allowed caller", cn)
			}
			return nil
		}
	}
	return cfg, nil
}

// ClientConfig builds the dialing side: the server must chain to CAFile and
// match serverName; CertFile and KeyFile, when set, are presented as the client
// certificate for mutual TLS.
func (t TLS) ClientConfig(serverName string) (*tls.Config, error) {
	if t.Insecure {
		return nil, errors.New("config: ClientConfig called with tls.insecure set")
	}
	pool, err := t.caPool()
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
		ServerName: serverName,
	}
	if t.CertFile != "" || t.KeyFile != "" {
		cert, err := t.keyPair()
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func (t TLS) keyPair() (tls.Certificate, error) {
	if t.CertFile == "" || t.KeyFile == "" {
		return tls.Certificate{}, errors.New("tls: cert_file and key_file are required (run `task certs:dev` for local certificates)")
	}
	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tls: load %s and %s: %w (run `task certs:dev` for local certificates)", t.CertFile, t.KeyFile, err)
	}
	return cert, nil
}

func (t TLS) caPool() (*x509.CertPool, error) {
	if t.CAFile == "" {
		return nil, errors.New("tls: ca_file is required")
	}
	pem, err := os.ReadFile(t.CAFile)
	if err != nil {
		return nil, fmt.Errorf("tls: read ca_file %s: %w (run `task certs:dev` for local certificates)", t.CAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("tls: ca_file %s holds no PEM certificate", t.CAFile)
	}
	return pool, nil
}
