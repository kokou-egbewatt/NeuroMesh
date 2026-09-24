package config

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/utils/certgen"
)

type pki struct {
	dir string
	ca  *certgen.CA
}

func newPKI(t *testing.T) pki {
	t.Helper()
	ca, err := certgen.NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := certgen.WritePair(dir, "ca", ca.CertPEM, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return pki{dir: dir, ca: ca}
}

func (p pki) leaf(t *testing.T, name string) TLS {
	t.Helper()
	l, err := p.ca.Issue(name, []string{"localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := certgen.WritePair(p.dir, name, l.CertPEM, l.KeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return TLS{
		CertFile: filepath.Join(p.dir, name+".crt"),
		KeyFile:  filepath.Join(p.dir, name+".key"),
		CAFile:   filepath.Join(p.dir, "ca.crt"),
	}
}

// handshake runs one TLS handshake over loopback TCP and returns the
// client-side and server-side errors. A kernel socket rather than net.Pipe: an
// unbuffered pipe deadlocks when the server rejects the client mid-write.
func handshake(t *testing.T, server, client *tls.Config) (clientErr, serverErr error) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lis.Close() }()
	done := make(chan error, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			done <- err
			return
		}
		srv := tls.Server(conn, server)
		_ = srv.SetDeadline(time.Now().Add(5 * time.Second))
		err = srv.Handshake()
		if err == nil {
			buf := make([]byte, 1)
			_, _ = srv.Read(buf)
			_, _ = srv.Write(buf)
		}
		_ = srv.Close()
		done <- err
	}()
	conn, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cl := tls.Client(conn, client)
	_ = cl.SetDeadline(time.Now().Add(5 * time.Second))
	clientErr = cl.Handshake()
	if clientErr == nil {
		// TLS 1.3 reports a rejected client certificate after the handshake,
		// on the first read.
		_, _ = cl.Write([]byte("x"))
		buf := make([]byte, 1)
		if _, err := cl.Read(buf); err != nil && !errors.Is(err, io.EOF) {
			clientErr = err
		}
	}
	_ = cl.Close()
	serverErr = <-done
	return clientErr, serverErr
}

func TestMutualTLSAcceptsAllowedClient(t *testing.T) {
	p := newPKI(t)
	srvTLS := p.leaf(t, "runtime")
	srvTLS.AllowedClientNames = []string{"gateway"}
	server, err := srvTLS.ServerConfig(true)
	if err != nil {
		t.Fatal(err)
	}
	client, err := p.leaf(t, "gateway").ClientConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	if _, serr := handshake(t, server, client); serr != nil {
		t.Fatalf("server rejected an allowed client: %v", serr)
	}
}

func TestMutualTLSRejectsDisallowedCommonName(t *testing.T) {
	p := newPKI(t)
	srvTLS := p.leaf(t, "runtime")
	srvTLS.AllowedClientNames = []string{"gateway"}
	server, _ := srvTLS.ServerConfig(true)
	client, _ := p.leaf(t, "intruder").ClientConfig("localhost")
	_, serr := handshake(t, server, client)
	if serr == nil || !strings.Contains(serr.Error(), "not an allowed caller") {
		t.Fatalf("server error = %v, want the allow-list rejection", serr)
	}
}

func TestMutualTLSRejectsClientWithoutCertificate(t *testing.T) {
	p := newPKI(t)
	server, _ := p.leaf(t, "runtime").ServerConfig(true)
	client, _ := TLS{CAFile: filepath.Join(p.dir, "ca.crt")}.ClientConfig("localhost")
	if _, serr := handshake(t, server, client); serr == nil {
		t.Fatal("server accepted a client with no certificate")
	}
}

func TestMutualTLSRejectsForeignCA(t *testing.T) {
	p := newPKI(t)
	other := newPKI(t)
	server, _ := p.leaf(t, "runtime").ServerConfig(true)
	client, _ := other.leaf(t, "gateway").ClientConfig("localhost")
	cerr, serr := handshake(t, server, client)
	if cerr == nil && serr == nil {
		t.Fatal("a certificate from a different CA completed the handshake")
	}
}

func TestClientRejectsServerNameMismatch(t *testing.T) {
	p := newPKI(t)
	server, _ := p.leaf(t, "runtime").ServerConfig(false)
	client, _ := TLS{CAFile: filepath.Join(p.dir, "ca.crt")}.ClientConfig("not-in-the-sans")
	if cerr, _ := handshake(t, server, client); cerr == nil {
		t.Fatal("client accepted a server certificate for the wrong name")
	}
}

func TestMissingFilesNameTheFix(t *testing.T) {
	_, err := TLS{CertFile: "nope.crt", KeyFile: "nope.key"}.ServerConfig(false)
	if err == nil || !strings.Contains(err.Error(), "task certs:dev") {
		t.Fatalf("err = %v, want it to point at task certs:dev", err)
	}
	if _, err := (TLS{}).ClientConfig("x"); err == nil {
		t.Fatal("a client without ca_file must fail")
	}
	if _, err := (TLS{Insecure: true}).ServerConfig(false); err == nil {
		t.Fatal("ServerConfig must refuse insecure")
	}
}

func TestResolveRelative(t *testing.T) {
	base := filepath.Join("etc", "svc")
	abs, _ := filepath.Abs("x.crt")
	c := TLS{CertFile: "../certs/tls.crt", KeyFile: abs, CAFile: ""}
	c.ResolveRelative(base)
	if c.CertFile != filepath.Join("etc", "certs", "tls.crt") || c.KeyFile != abs || c.CAFile != "" {
		t.Fatalf("unexpected: %+v", c)
	}
}
