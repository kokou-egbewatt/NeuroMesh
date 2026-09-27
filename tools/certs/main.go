// Command certs mints the throwaway CA and per-service certificates for
// running NeuroMesh on a laptop outside a cluster (`task certs:dev`).
//
// On a cluster cert-manager issues these instead. The output uses the same
// layout a cert-manager Secret mounts, one directory per service holding
// tls.crt, tls.key and ca.crt, so service configs do not change between the two.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/utils/certgen"
)

// services and the names each certificate must be valid for: the Kubernetes
// Service name, the container name `task images:smoke` uses, and loopback.
var services = map[string][]string{
	"runtime": {"runtime", "nm-runtime", "localhost", "127.0.0.1", "::1"},
	"gateway": {"gateway", "nm-gateway", "localhost", "127.0.0.1", "::1"},
}

func main() {
	out := flag.String("out", "certs/dev", "output directory")
	force := flag.Bool("force", false, "overwrite existing certificates")
	validFor := flag.Duration("valid-for", 90*24*time.Hour, "validity of the CA and leaves")
	keyMode := flag.String("key-mode", "0600", "file mode for private keys (octal)")
	flag.Parse()

	if err := run(*out, *force, *validFor, *keyMode); err != nil {
		fmt.Fprintln(os.Stderr, "certs:", err)
		os.Exit(1)
	}
}

func run(out string, force bool, validFor time.Duration, keyModeStr string) error {
	mode, err := strconv.ParseUint(keyModeStr, 8, 32)
	if err != nil {
		return fmt.Errorf("bad -key-mode %q: %w", keyModeStr, err)
	}
	caPath := filepath.Join(out, "ca.crt")
	if _, err := os.Stat(caPath); err == nil && !force {
		fmt.Printf("certificates already exist in %s (use -force to replace)\n", out)
		return nil
	}
	ca, err := certgen.NewCA("NeuroMesh dev CA", validFor)
	if err != nil {
		return err
	}
	if err := certgen.WritePair(out, "ca", ca.CertPEM, nil, 0o600); err != nil {
		return err
	}
	for name, hosts := range services {
		leaf, err := ca.Issue(name, hosts, validFor)
		if err != nil {
			return err
		}
		if err := certgen.WriteSecretLayout(filepath.Join(out, name), leaf, ca.CertPEM, os.FileMode(mode)); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %s/ca.crt and %s/{runtime,gateway}/{tls.crt,tls.key,ca.crt}, valid until %s\n",
		out, out, time.Now().Add(validFor).Format(time.DateOnly))
	fmt.Println("the CA private key is not written: re-run with -force to rotate everything")
	return nil
}
