package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config is tools/cluster/cluster.yaml.
type Config struct {
	Distro  string   `yaml:"distro"`
	Node    string   `yaml:"node"`
	K3s     string   `yaml:"k3s"`
	Disable []string `yaml:"disable"`
	GPU     struct {
		Expected    bool `yaml:"expected"`
		TimeSlicing int  `yaml:"timeSlicing"`
	} `yaml:"gpu"`
}

var (
	k3sVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+\+k3s\d+$`)
	dnsLabel   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
)

// KeepaliveTask is the Windows Scheduled Task that holds a session on the distro.
func (c *Config) KeepaliveTask() string { return c.Node + " keepalive" }

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseConfig(raw)
}

func parseConfig(raw []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("cluster.yaml: %w", err)
	}
	if c.GPU.TimeSlicing == 0 {
		c.GPU.TimeSlicing = 1
	}
	var errs []error
	if c.Distro == "" {
		errs = append(errs, errors.New("distro is required"))
	}
	if !dnsLabel.MatchString(c.Node) {
		errs = append(errs, fmt.Errorf("node %q is not a DNS label", c.Node))
	}
	if !k3sVersion.MatchString(c.K3s) {
		errs = append(errs, fmt.Errorf("k3s %q is not a release like v1.36.5+k3s1", c.K3s))
	}
	if c.GPU.TimeSlicing < 1 {
		errs = append(errs, fmt.Errorf("gpu.timeSlicing %d must be at least 1", c.GPU.TimeSlicing))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("cluster.yaml: %w", errors.Join(errs...))
	}
	return &c, nil
}
