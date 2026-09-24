package sdk

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestNewClientRequiresAnExplicitTransport(t *testing.T) {
	if _, err := NewClient("localhost:1"); !errors.Is(err, ErrNoTransport) {
		t.Fatalf("err = %v, want ErrNoTransport", err)
	}
	if _, err := NewClient("localhost:1", WithTLS(&tls.Config{MinVersion: tls.VersionTLS13}), WithInsecure()); err == nil {
		t.Fatal("TLS and insecure together must fail")
	}
}

func TestNewClientIsLazy(t *testing.T) {
	for name, opt := range map[string]Option{
		"tls":      WithTLS(&tls.Config{MinVersion: tls.VersionTLS13}),
		"insecure": WithInsecure(),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := NewClient("localhost:1", opt)
			if err != nil {
				t.Fatalf("construction dialed or failed: %v", err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
