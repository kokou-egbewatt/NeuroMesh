package certgen

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIssuedLeafVerifiesAgainstCA(t *testing.T) {
	ca, err := NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.Issue("runtime", []string{"runtime", "localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM)
	if err != nil {
		t.Fatalf("leaf does not load as a key pair: %v", err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, host := range []string{"runtime", "localhost", "127.0.0.1"} {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Errorf("verify for %s: %v", host, err)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("leaf is not usable as a client certificate: %v", err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "gateway"}); err == nil {
		t.Error("leaf verified for a host it does not name")
	}
}

func TestExpiredLeafFailsVerification(t *testing.T) {
	ca, err := NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	leaf, err := ca.IssueBetween("old", []string{"localhost"}, past, past.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(pair.Certificate[0])
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); err == nil {
		t.Fatal("expired leaf verified")
	}
}

func TestIssueRequiresCommonName(t *testing.T) {
	ca, err := NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Issue("", nil, time.Hour); err == nil {
		t.Fatal("expected an error for an empty common name")
	}
}

func TestWritePairModes(t *testing.T) {
	dir := t.TempDir()
	if err := WritePair(dir, "svc", []byte("cert"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"svc.crt", "svc.key"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	if err := WritePair(dir, "ca", []byte("cert"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ca.key")); !os.IsNotExist(err) {
		t.Error("a nil key must not produce a key file")
	}
}

func TestWriteSecretLayoutMatchesCertManager(t *testing.T) {
	ca, err := NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.Issue("gateway", []string{"localhost"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "gateway")
	if err := WriteSecretLayout(dir, leaf, ca.CertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")); err != nil {
		t.Fatalf("tls.crt and tls.key do not form a pair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err != nil {
		t.Fatal(err)
	}
}
