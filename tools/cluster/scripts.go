package main

// Every script that runs inside the distro is rendered here, by pure functions, so the logic is
// testable on any machine. They are sent to `wsl.exe ... bash -s` on stdin: passing them as
// arguments would let Git Bash rewrite paths and expand $variables before Linux sees them.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"
)

const (
	k3sConfigPath = "/etc/rancher/k3s/config.yaml"
	manifestsDir  = "/var/lib/rancher/k3s/server/manifests"
	pluginConfig  = "nvidia-device-plugin-config.yaml"
)

// file is one file the start script places in the distro.
type file struct {
	Path    string
	Content string
}

func (f file) b64() string { return base64.StdEncoding.EncodeToString([]byte(f.Content)) }

func (f file) sha() string {
	sum := sha256.Sum256([]byte(f.Content))
	return hex.EncodeToString(sum[:])
}

// k3sConfig is /etc/rancher/k3s/config.yaml. write-kubeconfig-mode keeps the kubeconfig readable
// after every restart; without it k3s rewrites it root-only.
func k3sConfig(c *Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Written by tools/cluster from cluster.yaml; edits are overwritten.\n")
	fmt.Fprintf(&b, "node-name: %s\n", c.Node)
	fmt.Fprintf(&b, "write-kubeconfig-mode: \"0644\"\n")
	if len(c.Disable) > 0 {
		b.WriteString("disable:\n")
		for _, d := range c.Disable {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
	}
	return b.String()
}

// devicePluginConfig is the ConfigMap the device plugin DaemonSet mounts. More than one replica
// turns on time-slicing: each replica is a schedulable nvidia.com/gpu sharing the whole card.
func devicePluginConfig(c *Config) string {
	cfg := "    version: v1\n"
	if c.GPU.TimeSlicing > 1 {
		cfg += fmt.Sprintf("    sharing:\n      timeSlicing:\n        resources:\n"+
			"          - name: nvidia.com/gpu\n            replicas: %d\n", c.GPU.TimeSlicing)
	}
	return "# Written by tools/cluster from cluster.yaml gpu.timeSlicing.\n" +
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: nvidia-device-plugin-config\n" +
		"  namespace: kube-system\ndata:\n  config.yaml: |\n" + cfg
}

// startFiles is everything start keeps in place: k3s's config, and the GPU manifests in the
// auto-deploy folder k3s reapplies on every start. repoManifests maps file name to content.
func startFiles(c *Config, repoManifests map[string]string) []file {
	files := []file{{Path: k3sConfigPath, Content: k3sConfig(c)}}
	names := make([]string, 0, len(repoManifests))
	for n := range repoManifests {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files = append(files, file{Path: path.Join(manifestsDir, n), Content: repoManifests[n]})
	}
	if c.GPU.Expected {
		files = append(files, file{Path: path.Join(manifestsDir, pluginConfig), Content: devicePluginConfig(c)})
	}
	return files
}

var funcs = template.FuncMap{"dir": path.Dir}

// waitFn defines wait_ready <timeout-s>: the node Ready, then, when a GPU is expected, the device
// plugin ready and nvidia.com/gpu allocatable. Prints ready= and allocatable= lines.
//
// Right after a restart the API still serves the node status from before it, Ready and with the
// GPU allocatable, for a few seconds. So Ready only counts once its heartbeat is newer than this
// k3s start, and allocatable only once the plugin DaemonSet reports every pod ready.
const waitFn = `
wait_ready() {
  local end=$((SECONDS + $1)) a="" r="" since
  since="$(date -d "$(systemctl show -p ActiveEnterTimestamp --value k3s)" +%s)"
  until r="$(k3s kubectl get node {{.C.Node}} -o jsonpath='{.status.conditions[?(@.type=="Ready")].status} {.status.conditions[?(@.type=="Ready")].lastHeartbeatTime}' 2>/dev/null)" \
    && [ "${r%% *}" = True ] && [ "$(date -d "${r#* }" +%s 2>/dev/null || echo 0)" -ge "$since" ]; do
    if [ "$SECONDS" -ge "$end" ]; then echo "ready=timeout"; return 1; fi
    sleep 1
  done
  echo "ready=True"
{{- if .C.GPU.Expected}}
  until r="$(k3s kubectl -n kube-system get ds nvidia-device-plugin-daemonset -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}' 2>/dev/null)" \
    && [ "${r%/*}" = "${r#*/}" ] && [ "${r%/*}" != 0 ] \
    && a="$(k3s kubectl get node {{.C.Node}} -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' 2>/dev/null)" && [ -n "$a" ] && [ "$a" != 0 ]; do
    if [ "$SECONDS" -ge "$end" ]; then echo "allocatable=none"; return 1; fi
    sleep 1
  done
  echo "allocatable=$a"
{{- end}}
}
`

const preamble = `set -euo pipefail
if [ "$(ps -p 1 -o comm=)" != systemd ]; then
  echo "error=systemd is not running in this distro: set [boot] systemd=true in /etc/wsl.conf, then wsl --terminate it"
  exit 1
fi
`

var startTmpl = template.Must(template.New("start").Funcs(funcs).Parse(preamble + `
{{- if .C.GPU.Expected}}
if ! command -v nvidia-container-runtime >/dev/null; then
  echo "error=nvidia-container-toolkit is not installed in {{.C.Distro}}; k3s needs it to create the nvidia runtime and RuntimeClass"
  exit 1
fi
{{- end}}
config_changed=0
{{- range .Files}}
want={{.Sha}}
have="$( [ -f '{{.Path}}' ] && sha256sum '{{.Path}}' | cut -d' ' -f1 || true)"
if [ "$want" != "$have" ]; then
  mkdir -p '{{dir .Path}}'
  printf %s '{{.B64}}' | base64 -d > '{{.Path}}'
  echo "changed {{.Path}}"
  {{- if eq .Path "` + k3sConfigPath + `"}}
  config_changed=1
  {{- end}}
else
  echo "ok {{.Path}}"
fi
{{- end}}
if [ ! -x /usr/local/bin/k3s ]; then
  if ! curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='{{.C.K3s}}' sh - > /tmp/k3s-install.log 2>&1; then
    tail -20 /tmp/k3s-install.log
    echo "error=k3s install failed"
    exit 1
  fi
  echo "changed k3s {{.C.K3s}} installed"
else
  have="$(k3s --version | head -1 | awk '{print $3}')"
  if [ "$have" = '{{.C.K3s}}' ]; then
    echo "ok k3s $have"
  else
    echo "warn k3s $have is installed and cluster.yaml pins {{.C.K3s}}; not upgraded"
  fi
  if ! systemctl is-enabled --quiet k3s; then
    systemctl enable k3s > /dev/null 2>&1
    echo "changed k3s enabled at boot"
  fi
  if [ "$config_changed" = 1 ] && systemctl is-active --quiet k3s; then
    systemctl restart k3s
    echo "changed k3s restarted for {{"` + k3sConfigPath + `"}}"
  fi
  if ! systemctl is-active --quiet k3s; then
    systemctl start k3s
    echo "changed k3s started"
  fi
fi
` + waitFn + `
wait_ready {{.Timeout}}
`))

// startScript provisions or reconciles the cluster and waits for it. timeout is in seconds.
func startScript(c *Config, files []file, timeout int) string {
	type tf struct{ Path, Sha, B64 string }
	fs := make([]tf, len(files))
	for i, f := range files {
		fs[i] = tf{f.Path, f.sha(), f.b64()}
	}
	return render(startTmpl, map[string]any{"C": c, "Files": fs, "Timeout": timeout})
}

var resumeTmpl = template.Must(template.New("resume").Parse(preamble + `
if [ ! -x /usr/local/bin/k3s ]; then echo "error=k3s is not installed: run start"; exit 1; fi
if systemctl is-active --quiet k3s; then
  echo "ok k3s already running"
else
  systemctl enable --now k3s > /dev/null 2>&1
  echo "changed k3s started"
fi
` + waitFn + `
wait_ready {{.Timeout}}
`))

// resumeScript starts k3s again after pause or stop.
func resumeScript(c *Config, timeout int) string {
	return render(resumeTmpl, map[string]any{"C": c, "Timeout": timeout})
}

// pauseScript stops k3s and every pod, and keeps it from starting with the distro. State on disk
// (node, PVCs, images) is untouched. k3s-killall.sh also removes the shims, mounts and CNI
// interfaces a stopped service leaves behind, which is what frees the GPU.
func pauseScript() string {
	return preamble + `
if [ ! -x /usr/local/bin/k3s ]; then echo "error=k3s is not installed"; exit 1; fi
systemctl disable --now k3s > /dev/null 2>&1 || true
/usr/local/bin/k3s-killall.sh > /dev/null 2>&1 || true
if pgrep -x k3s-server > /dev/null || pgrep -f containerd-shim > /dev/null; then
  echo "error=k3s processes are still running after k3s-killall.sh"
  exit 1
fi
echo "changed k3s stopped and disabled; node, volumes and images kept"
`
}

// deleteScript removes k3s and everything its uninstaller leaves, including the node password
// file a reinstall would otherwise inherit.
func deleteScript() string {
	return preamble + `
if [ -x /usr/local/bin/k3s-uninstall.sh ]; then
  /usr/local/bin/k3s-uninstall.sh > /dev/null 2>&1
  echo "changed k3s uninstalled"
else
  echo "ok k3s not installed"
fi
rm -rf /etc/rancher /var/lib/rancher
echo "changed removed /etc/rancher and /var/lib/rancher"
`
}

var statusTmpl = template.Must(template.New("status").Parse(`set -uo pipefail
K="k3s kubectl"
echo "systemd=$(ps -p 1 -o comm=)"
echo "toolkit=$(dpkg-query -W -f '${Version}' nvidia-container-toolkit 2>/dev/null || echo none)"
echo "dxg=$( [ -e /dev/dxg ] && echo yes || echo no)"
if [ ! -x /usr/local/bin/k3s ]; then echo "k3s=none"; exit 0; fi
echo "k3s=$(k3s --version | head -1 | awk '{print $3}')"
echo "k3s_enabled=$(systemctl is-enabled k3s 2>/dev/null)"
echo "k3s_active=$(systemctl is-active k3s 2>/dev/null)"
echo "containerd_nvidia=$(grep -qs 'BinaryName = "/usr/bin/nvidia-container-runtime"' /var/lib/rancher/k3s/agent/etc/containerd/config.toml* && echo yes || echo no)"
systemctl is-active --quiet k3s || exit 0
echo "node_created=$($K get node {{.Node}} -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)"
echo "node_ready=$($K get node {{.Node}} -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)"
echo "allocatable=$($K get node {{.Node}} -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' 2>/dev/null)"
echo "runtimeclass=$($K get runtimeclass nvidia -o jsonpath='{.handler}' 2>/dev/null)"
echo "policy=$($K get validatingadmissionpolicybinding gpu-request-and-runtimeclass-together -o jsonpath='{.spec.policyName}' 2>/dev/null)"
echo "plugin=$($K -n kube-system get ds nvidia-device-plugin-daemonset -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}' 2>/dev/null)"
echo "restarts=$($K get pods -A --no-headers 2>/dev/null | awk '{s+=$5} END{print s+0}')"
echo "gpu_pods=$($K get pods -A --field-selector=status.phase=Running -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}={.spec.containers[*].resources.limits.nvidia\.com/gpu}{"\n"}{end}' 2>/dev/null | awk -F= '$2!=""' | paste -sd, -)"
`))

// statusScript prints key=value lines describing every layer inside the distro.
func statusScript(c *Config) string { return render(statusTmpl, c) }

func render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("template %s: %v", t.Name(), err)) // templates are fixed; a failure is a bug
	}
	return b.String()
}
