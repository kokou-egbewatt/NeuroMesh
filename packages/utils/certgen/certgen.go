// Package certgen mints a throwaway certificate authority and leaf
// certificates for tests and for running services directly on a laptop,
// outside any cluster.
//
// On a cluster, cert-manager mints every certificate (a Certificate and Issuer
// in each chart); nothing here runs there. The generator writes the same file
// layout a cert-manager Secret mounts, one directory per service holding
// tls.crt, tls.key and ca.crt, so a service config written for one works
// unchanged with the other.
package certgen

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CA is a self-signed certificate authority held in memory.
type CA struct {
	Cert    *x509.Certificate
	Key     crypto.Signer
	CertPEM []byte
	KeyPEM  []byte
}

// Leaf is an issued certificate and its private key, PEM encoded.
type Leaf struct {
	CertPEM []byte
	KeyPEM  []byte
}

// NewCA creates a self-signed CA valid from now for validFor.
func NewCA(commonName string, validFor time.Duration) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"NeuroMesh dev"}},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key, CertPEM: encodeCert(der), KeyPEM: keyPEM}, nil
}

// Issue signs a leaf valid from now for validFor. The leaf carries both server
// and client authentication usages, so one certificate per service covers its
// listener and its outbound mutual TLS dials. hosts become DNS or IP SANs.
func (ca *CA) Issue(commonName string, hosts []string, validFor time.Duration) (*Leaf, error) {
	now := time.Now()
	return ca.IssueBetween(commonName, hosts, now.Add(-time.Minute), now.Add(validFor))
}

// IssueBetween signs a leaf with an explicit validity window, which is how the
// tests produce expired and not-yet-valid certificates.
func (ca *CA) IssueBetween(commonName string, hosts []string, notBefore, notAfter time.Time) (*Leaf, error) {
	if commonName == "" {
		return nil, errors.New("certgen: common name is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName, Organization: []string{"NeuroMesh dev"}},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, fmt.Errorf("sign leaf %q: %w", commonName, err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, err
	}
	return &Leaf{CertPEM: encodeCert(der), KeyPEM: keyPEM}, nil
}

// WritePair writes <name>.crt and <name>.key into dir. Keys are written with
// keyMode (0o600 for a developer machine; the image smoke test passes 0o644
// because the containers run as a different uid than the file's owner).
func WritePair(dir, name string, certPEM, keyPEM []byte, keyMode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".crt"), certPEM, 0o644); err != nil {
		return err
	}
	if keyPEM == nil {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, name+".key"), keyPEM, keyMode)
}

func encodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func newSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}
	return serial, nil
}

// WriteSecretLayout writes tls.crt, tls.key and ca.crt into dir: the keys a
// cert-manager Secret carries when mounted as a volume.
func WriteSecretLayout(dir string, leaf *Leaf, caPEM []byte, keyMode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), leaf.CertPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), caPEM, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tls.key"), leaf.KeyPEM, keyMode)
}
