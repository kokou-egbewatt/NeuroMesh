package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// statusInput is everything status reads, collected before evaluation so evaluation is pure.
type statusInput struct {
	Driver     string            // nvidia-smi.exe name and driver, "" when it failed
	Distro     string            // Running, Stopped, or "" when missing
	Keepalive  string            // Scheduled Task state, or "missing"
	Context    bool              // the node's context is in the Windows kubeconfig
	KV         map[string]string // statusScript output; nil when the distro was not running
	Now        time.Time
	ConfigFile string
}

type level int

const (
	pass level = iota
	warning
	failure
)

type check struct {
	Level  level
	Layer  string
	Detail string
}

// parseKV reads key=value lines; anything else is ignored.
func parseKV(out string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k != "" && !strings.ContainsAny(k, " \t") {
			kv[k] = v
		}
	}
	return kv
}

// evaluate walks the layers from the Windows driver up to the pods, the order gpu-debug.md
// diagnoses them in: the first failure is the layer to fix.
func evaluate(c *Config, in statusInput) []check {
	var cs []check
	add := func(l level, layer, format string, a ...any) {
		cs = append(cs, check{l, layer, fmt.Sprintf(format, a...)})
	}

	if in.Driver == "" {
		add(failure, "Windows driver", "nvidia-smi.exe did not answer")
	} else {
		add(pass, "Windows driver", "%s", in.Driver)
	}

	switch in.Keepalive {
	case "Running":
		add(pass, "keepalive", "task %q running", c.KeepaliveTask())
	case "missing":
		add(failure, "keepalive", "task %q not registered: run start", c.KeepaliveTask())
	default:
		add(warning, "keepalive", "task %q is %s: the distro stops when no terminal is open (stopped with `stop`?)", c.KeepaliveTask(), in.Keepalive)
	}

	switch in.Distro {
	case "Running":
		add(pass, "WSL distro", "%s running", c.Distro)
	case "":
		add(failure, "WSL distro", "%s does not exist", c.Distro)
		return cs
	default:
		add(warning, "WSL distro", "%s is %s: run resume", c.Distro, in.Distro)
		return cs
	}

	kv := in.KV
	if kv["systemd"] != "systemd" {
		add(failure, "systemd", "PID 1 is %q: set [boot] systemd=true in /etc/wsl.conf", kv["systemd"])
	}
	if c.GPU.Expected {
		if kv["dxg"] == "yes" {
			add(pass, "WSL passthrough", "/dev/dxg present")
		} else {
			add(failure, "WSL passthrough", "/dev/dxg missing: the Windows driver is not reaching WSL")
		}
		if v := kv["toolkit"]; v == "" || v == "none" {
			add(failure, "container toolkit", "nvidia-container-toolkit not installed in %s", c.Distro)
		} else {
			add(pass, "container toolkit", "nvidia-container-toolkit %s", v)
		}
	}

	switch v := kv["k3s"]; {
	case v == "" || v == "none":
		add(failure, "k3s", "not installed: run start")
		return cs
	case v != c.K3s:
		add(warning, "k3s", "%s installed, %s pins %s", v, in.ConfigFile, c.K3s)
	default:
		add(pass, "k3s", "%s", v)
	}
	if kv["k3s_active"] != "active" {
		state := "stopped"
		if kv["k3s_enabled"] != "enabled" {
			state = "paused (disabled)"
		}
		add(warning, "k3s service", "%s: run resume", state)
		return cs
	}
	add(pass, "k3s service", "active, %s at boot", kv["k3s_enabled"])

	if c.GPU.Expected {
		if kv["containerd_nvidia"] == "yes" {
			add(pass, "containerd runtime", "nvidia runtime configured")
		} else {
			add(failure, "containerd runtime", "no nvidia runtime in k3s's containerd: install the toolkit, then restart k3s")
		}
	}

	if kv["node_ready"] == "True" {
		age := ""
		if t, err := time.Parse(time.RFC3339, kv["node_created"]); err == nil {
			age = ", created " + in.Now.Sub(t).Round(time.Second).String() + " ago"
		}
		add(pass, "node", "%s Ready%s", c.Node, age)
	} else {
		add(failure, "node", "%s not Ready (%q)", c.Node, kv["node_ready"])
	}

	if c.GPU.Expected {
		if kv["runtimeclass"] == "nvidia" {
			add(pass, "RuntimeClass", "nvidia -> nvidia")
		} else {
			add(failure, "RuntimeClass", "nvidia missing: k3s creates it when it finds the toolkit at start")
		}
		ready, desired, _ := strings.Cut(kv["plugin"], "/")
		pluginReady := ready != "" && ready == desired && ready != "0"
		if pluginReady {
			add(pass, "device plugin", "%s pod ready", ready)
		} else {
			add(failure, "device plugin", "%q ready: right after a start or restart it takes about 20 s; "+
				"otherwise `kubectl -n kube-system logs ds/nvidia-device-plugin-daemonset`", kv["plugin"])
		}
		n, _ := strconv.Atoi(kv["allocatable"])
		switch {
		case n == 0 && !pluginReady:
			add(failure, "nvidia.com/gpu", "not allocatable until the plugin is ready")
		case n == 0:
			add(failure, "nvidia.com/gpu", "not allocatable with the plugin ready: if its log says `Incompatible strategy`, it lacks runtimeClassName")
		case n != c.GPU.TimeSlicing:
			add(warning, "nvidia.com/gpu", "%d allocatable, cluster.yaml expects %d (time-slicing)", n, c.GPU.TimeSlicing)
		default:
			add(pass, "nvidia.com/gpu", "%d allocatable", n)
		}
		if kv["policy"] != "" {
			add(pass, "admission policy", "GPU request and RuntimeClass must come together")
		} else {
			add(failure, "admission policy", "not bound: pods can reach the GPU without the scheduler knowing")
		}
		if pods := kv["gpu_pods"]; pods != "" {
			add(pass, "GPU in use", "%s", pods)
		} else {
			add(pass, "GPU in use", "no running pod holds nvidia.com/gpu")
		}
	}

	if in.Context {
		add(pass, "kubeconfig", "context %s in the Windows kubeconfig", c.Node)
	} else {
		add(warning, "kubeconfig", "no %s context for Windows kubectl: run start", c.Node)
	}
	return cs
}

func healthy(cs []check) bool {
	for _, c := range cs {
		if c.Level == failure {
			return false
		}
	}
	return true
}

func formatChecks(cs []check) string {
	var b strings.Builder
	mark := map[level]string{pass: "✅", warning: "⚠️ ", failure: "❌"}
	for _, c := range cs {
		fmt.Fprintf(&b, "  %s %-19s %s\n", mark[c.Level], c.Layer, c.Detail)
	}
	return b.String()
}
