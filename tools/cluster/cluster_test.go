package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	raw, err := os.ReadFile("cluster.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCommittedConfigParses(t *testing.T) {
	c := testConfig(t)
	if c.Distro != "Ubuntu" || c.Node != "gpu-lab" || c.GPU.TimeSlicing != 1 || !c.GPU.Expected {
		t.Fatalf("unexpected config: %+v", c)
	}
	if c.KeepaliveTask() != "gpu-lab keepalive" {
		t.Fatalf("task name %q", c.KeepaliveTask())
	}
}

func TestConfigRejects(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field": "distro: Ubuntu\nnode: a\nk3s: v1.36.5+k3s1\nnodes: 2\n",
		"no distro":     "node: a\nk3s: v1.36.5+k3s1\n",
		"bad node":      "distro: Ubuntu\nnode: GPU_Lab\nk3s: v1.36.5+k3s1\n",
		"bad version":   "distro: Ubuntu\nnode: a\nk3s: latest\n",
		"bad slicing":   "distro: Ubuntu\nnode: a\nk3s: v1.36.5+k3s1\ngpu: {timeSlicing: -1}\n",
	} {
		if _, err := parseConfig([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestK3sConfig(t *testing.T) {
	got := k3sConfig(testConfig(t))
	for _, want := range []string{"node-name: gpu-lab\n", "write-kubeconfig-mode: \"0644\"\n", "  - traefik\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("k3s config lacks %q:\n%s", want, got)
		}
	}
}

func TestDevicePluginConfig(t *testing.T) {
	c := testConfig(t)
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal([]byte(devicePluginConfig(c)), &cm); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cm.Data["config.yaml"], "sharing") {
		t.Errorf("one replica must not time-slice:\n%s", cm.Data["config.yaml"])
	}
	c.GPU.TimeSlicing = 2
	if err := yaml.Unmarshal([]byte(devicePluginConfig(c)), &cm); err != nil {
		t.Fatal(err)
	}
	var inner struct {
		Sharing struct {
			TimeSlicing struct {
				Resources []struct {
					Name     string `yaml:"name"`
					Replicas int    `yaml:"replicas"`
				} `yaml:"resources"`
			} `yaml:"timeSlicing"`
		} `yaml:"sharing"`
	}
	if err := yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &inner); err != nil {
		t.Fatal(err)
	}
	r := inner.Sharing.TimeSlicing.Resources
	if len(r) != 1 || r[0].Name != "nvidia.com/gpu" || r[0].Replicas != 2 {
		t.Errorf("time-slicing config %+v", r)
	}
}

func TestStartFilesOrderAndPaths(t *testing.T) {
	files := startFiles(testConfig(t), map[string]string{"b.yaml": "b", "a.yaml": "a"})
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	want := []string{k3sConfigPath, manifestsDir + "/a.yaml", manifestsDir + "/b.yaml", manifestsDir + "/" + pluginConfig}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Fatalf("paths %v, want %v", paths, want)
	}
}

// Every rendered script must at least parse as bash.
func TestScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	c := testConfig(t)
	scripts := map[string]string{
		"start":  startScript(c, startFiles(c, map[string]string{"x.yaml": "it's 'quoted'\n"}), 300),
		"resume": resumeScript(c, 300),
		"pause":  pauseScript(),
		"delete": deleteScript(),
		"status": statusScript(c),
	}
	for name, s := range scripts {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(s)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s does not parse: %v\n%s\n---\n%s", name, err, out, s)
		}
	}
}

func TestStartScriptContent(t *testing.T) {
	c := testConfig(t)
	s := startScript(c, startFiles(c, nil), 120)
	for _, want := range []string{
		"INSTALL_K3S_VERSION='v1.36.5+k3s1'",
		"command -v nvidia-container-runtime",
		"config_changed=1",
		"wait_ready 120",
		"nvidia\\.com/gpu",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("start script lacks %q", want)
		}
	}
	c.GPU.Expected = false
	if s := startScript(c, startFiles(c, nil), 120); strings.Contains(s, "nvidia-container-runtime") || strings.Contains(s, "allocatable=") {
		t.Error("a cluster without a GPU must not check for the toolkit or wait for nvidia.com/gpu")
	}
}

func TestRenameKubeconfig(t *testing.T) {
	k3s := `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://127.0.0.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
users:
- name: default
  user:
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
`
	out, err := renameKubeconfig([]byte(k3s), "gpu-lab")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"server: https://localhost:6443", "current-context: gpu-lab", "cluster: gpu-lab", "user: gpu-lab", "client-key-data: S0VZ"} {
		if !strings.Contains(s, want) {
			t.Errorf("renamed kubeconfig lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "default") {
		t.Errorf("a default name survived:\n%s", s)
	}
	if _, err := renameKubeconfig([]byte("clusters: []\n"), "x"); err == nil {
		t.Error("accepted a kubeconfig with no clusters")
	}
}

func TestParseDistroState(t *testing.T) {
	list := "  NAME              STATE           VERSION\n* Ubuntu            Running         2\n  docker-desktop    Stopped         2\n"
	for distro, want := range map[string]string{"Ubuntu": "Running", "docker-desktop": "Stopped", "Debian": ""} {
		if got := parseDistroState(list, distro); got != want {
			t.Errorf("%s: %q, want %q", distro, got, want)
		}
	}
}

func TestDecodeWSL(t *testing.T) {
	utf16 := []byte{0xff, 0xfe, 'O', 0, 'K', 0, '\r', 0, '\n', 0}
	if got := decodeWSL(utf16); got != "OK\n" {
		t.Errorf("utf-16: %q", got)
	}
	if got := decodeWSL([]byte("plain\n")); got != "plain\n" {
		t.Errorf("utf-8: %q", got)
	}
}

func TestKeepaliveLoops(t *testing.T) {
	exe, args := keepaliveCommand("Ubuntu")
	if !strings.HasSuffix(exe, `\conhost.exe`) || !strings.HasPrefix(args, "--headless ") {
		t.Errorf("not hidden: %s %s", exe, args)
	}
	// wsl.exe exits 0 when the distro is terminated, so restart-on-failure never fires: the loop
	// is what brings the cluster back.
	if !strings.Contains(args, "while ($true)") || !strings.Contains(args, "wsl.exe -d Ubuntu --exec /bin/sleep infinity") {
		t.Errorf("keepalive does not loop: %s", args)
	}
	if q := psQuote("it's"); q != "'it''s'" {
		t.Errorf("psQuote: %s", q)
	}
}

func healthyKV() map[string]string {
	return parseKV(`systemd=systemd
toolkit=1.20.1-1
dxg=yes
k3s=v1.36.5+k3s1
k3s_enabled=enabled
k3s_active=active
containerd_nvidia=yes
node_created=2026-10-29T21:21:29Z
node_ready=True
allocatable=1
runtimeclass=nvidia
policy=gpu-request-and-runtimeclass-together
plugin=1/1
restarts=10
gpu_pods=
`)
}

func TestEvaluateHealthy(t *testing.T) {
	c := testConfig(t)
	in := statusInput{
		Driver: "NVIDIA GeForce RTX 5060, 621.01", Distro: "Running", Keepalive: "Running",
		Context: true, KV: healthyKV(), Now: time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC),
	}
	cs := evaluate(c, in)
	if !healthy(cs) {
		t.Fatalf("healthy cluster reported unhealthy:\n%s", formatChecks(cs))
	}
	if !strings.Contains(formatChecks(cs), "created 38m31s ago") {
		t.Errorf("node age missing:\n%s", formatChecks(cs))
	}
}

func TestEvaluateFailures(t *testing.T) {
	c := testConfig(t)
	base := func() statusInput {
		return statusInput{Driver: "x", Distro: "Running", Keepalive: "Running", Context: true, KV: healthyKV()}
	}
	cases := map[string]func(*statusInput){
		"no driver":         func(in *statusInput) { in.Driver = "" },
		"no toolkit":        func(in *statusInput) { in.KV["toolkit"] = "none" },
		"plugin no gpu":     func(in *statusInput) { in.KV["allocatable"] = "" },
		"no policy":         func(in *statusInput) { in.KV["policy"] = "" },
		"no runtimeclass":   func(in *statusInput) { in.KV["runtimeclass"] = "" },
		"keepalive missing": func(in *statusInput) { in.Keepalive = "missing" },
		"not ready":         func(in *statusInput) { in.KV["node_ready"] = "False" },
	}
	for name, mutate := range cases {
		in := base()
		mutate(&in)
		if healthy(evaluate(c, in)) {
			t.Errorf("%s: reported healthy", name)
		}
	}
	// paused and stopped are warnings, not failures
	for name, mutate := range map[string]func(*statusInput){
		"paused":  func(in *statusInput) { in.KV["k3s_active"] = "inactive"; in.KV["k3s_enabled"] = "disabled" },
		"stopped": func(in *statusInput) { in.Distro = "Stopped"; in.Keepalive = "Disabled" },
	} {
		in := base()
		mutate(&in)
		if !healthy(evaluate(c, in)) {
			t.Errorf("%s: reported as a failure:\n%s", name, formatChecks(evaluate(c, in)))
		}
	}
}

func TestProbePodShapes(t *testing.T) {
	full := probePod("gpu", true, true)
	if !strings.Contains(full, "runtimeClassName: nvidia") || !strings.Contains(full, "nvidia.com/gpu: 1") {
		t.Errorf("full probe:\n%s", full)
	}
	if strings.Contains(probePod("x", true, false), "nvidia.com/gpu") || strings.Contains(probePod("x", false, true), "runtimeClassName") {
		t.Error("half-shaped probes are not half-shaped")
	}
	var pod map[string]any
	if err := yaml.Unmarshal([]byte(full), &pod); err != nil {
		t.Errorf("probe pod is not YAML: %v", err)
	}
}

func TestEncodePS(t *testing.T) {
	// "ok" in UTF-16LE is 6f 00 6b 00
	if got := encodePS("ok"); got != "bwBrAA==" {
		t.Errorf("encodePS: %s", got)
	}
}
