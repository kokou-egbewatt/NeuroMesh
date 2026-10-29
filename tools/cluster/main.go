// Command cluster runs the local k3s cluster in WSL2 that NeuroMesh (and infinity-bench) deploy
// to: `task cluster:start|status|pause|resume|stop|delete` (#21).
//
// It is one single-node k3s in the distro named in cluster.yaml, with the NVIDIA device plugin,
// an admission policy that keeps every GPU pod visible to the scheduler, a Windows Scheduled Task
// that keeps the distro alive, and a context for Windows kubectl. Every command is idempotent.
//
// The reasoning behind each piece is measured in infinity-bench #22 (which local Kubernetes runs
// GPU work on WSL2) and summarised in docs/onboarding/local-cluster.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const usage = `usage: cluster [-config tools/cluster/cluster.yaml] <command> [flags]

commands:
  start    provision or reconcile the cluster, then wait for it (idempotent)
  status   check every layer from the Windows driver to the pods; -probe also runs a GPU pod
  pause    stop k3s and every pod, keep the node, volumes and images; the distro stays up
  resume   start k3s again after pause or stop
  stop     pause, then let the distro go: disable the keepalive and terminate it
  delete   remove k3s and all its state; needs -yes
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cluster:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	top := flag.NewFlagSet("cluster", flag.ContinueOnError)
	cfgPath := top.String("config", filepath.Join("tools", "cluster", "cluster.yaml"), "cluster shape")
	manifests := top.String("manifests", filepath.Join("infra", "kubernetes", "gpu"), "GPU manifests to deploy")
	top.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := top.Parse(args); err != nil {
		return err
	}
	if top.NArg() == 0 {
		top.Usage()
		return errors.New("no command")
	}
	if runtime.GOOS != "windows" {
		return errors.New("runs on Windows, against a WSL2 distro")
	}
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd, rest := top.Arg(0), top.Args()[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait for Ready and the GPU")
	switch cmd {
	case "start":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return start(ctx, c, *manifests, *timeout)
	case "status":
		withProbe := fs.Bool("probe", false, "also run a GPU pod and check the admission policy rejects bad shapes")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return status(ctx, c, *cfgPath, *withProbe)
	case "pause":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return pause(ctx, c)
	case "resume":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return resume(ctx, c, *timeout)
	case "stop":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return stopCluster(ctx, c)
	case "delete":
		yes := fs.Bool("yes", false, "really delete")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return deleteCluster(ctx, c, *yes)
	default:
		top.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// show prints a script's output, indented, and fails on an error= line or a non-zero exit.
func show(out string, err error) error {
	var failed string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if msg, ok := strings.CutPrefix(line, "error="); ok {
			failed = msg
			continue
		}
		fmt.Println("  " + line)
	}
	if failed != "" {
		return errors.New(failed)
	}
	return err
}

func start(ctx context.Context, c *Config, manifestDir string, timeout time.Duration) error {
	repo := map[string]string{}
	for _, name := range []string{"nvidia-device-plugin.yaml", "gpu-admission-policy.yaml"} {
		if !c.GPU.Expected {
			break
		}
		raw, err := os.ReadFile(filepath.Join(manifestDir, name))
		if err != nil {
			return fmt.Errorf("%w (run from the repo root)", err)
		}
		repo[name] = string(raw)
	}

	fmt.Println("keepalive")
	if err := show(runPS(ctx, ensureKeepaliveScript(c.KeepaliveTask(), c.Distro))); err != nil {
		return err
	}
	fmt.Printf("k3s in %s\n", c.Distro)
	if err := show(runWSL(ctx, c.Distro, startScript(c, startFiles(c, repo), int(timeout.Seconds())))); err != nil {
		return err
	}
	fmt.Println("kubeconfig")
	return syncKubeconfig(ctx, c)
}

func syncKubeconfig(ctx context.Context, c *Config) error {
	raw, err := runWSL(ctx, c.Distro, "cat /etc/rancher/k3s/k3s.yaml\n")
	if err != nil {
		return fmt.Errorf("reading k3s.yaml: %w: %s", err, raw)
	}
	ours, err := renameKubeconfig([]byte(raw), c.Node)
	if err != nil {
		return err
	}
	target, err := kubeconfigPath()
	if err != nil {
		return err
	}
	changed, err := mergeKubeconfig(ctx, target, ours)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("  changed context %s in %s\n", c.Node, target)
	} else {
		fmt.Printf("  ok context %s in %s\n", c.Node, target)
	}
	return nil
}

func status(ctx context.Context, c *Config, cfgPath string, withProbe bool) error {
	in := statusInput{Now: time.Now(), ConfigFile: cfgPath}
	in.Driver, _ = windowsDriver(ctx)
	var err error
	if in.Keepalive, err = runPS(ctx, keepaliveStateScript(c.KeepaliveTask())); err != nil {
		in.Keepalive = "missing"
	}
	if in.Distro, err = distroState(ctx, c.Distro); err != nil {
		return err
	}
	if in.Distro == "Running" {
		out, err := runWSL(ctx, c.Distro, statusScript(c))
		if err != nil {
			return fmt.Errorf("status script: %w: %s", err, out)
		}
		in.KV = parseKV(out)
	}
	if out, err := kubectl(ctx, c.Node, "", "config", "get-contexts", "-o", "name"); err == nil {
		for _, name := range strings.Fields(out) {
			in.Context = in.Context || name == c.Node
		}
	}
	cs := evaluate(c, in)
	if withProbe && healthy(cs) && in.Context {
		cs = append(cs, probe(ctx, c.Node)...)
	}
	fmt.Print(formatChecks(cs))
	if !healthy(cs) {
		return errors.New("unhealthy: the first ❌ is the layer to fix")
	}
	return nil
}

func pause(ctx context.Context, c *Config) error {
	if st, err := distroState(ctx, c.Distro); err != nil {
		return err
	} else if st != "Running" {
		fmt.Printf("  ok %s is %s; nothing is running\n", c.Distro, st)
		return nil
	}
	return show(runWSL(ctx, c.Distro, pauseScript()))
}

func resume(ctx context.Context, c *Config, timeout time.Duration) error {
	fmt.Println("keepalive")
	if err := show(runPS(ctx, ensureKeepaliveScript(c.KeepaliveTask(), c.Distro))); err != nil {
		return err
	}
	fmt.Printf("k3s in %s\n", c.Distro)
	return show(runWSL(ctx, c.Distro, resumeScript(c, int(timeout.Seconds()))))
}

func stopCluster(ctx context.Context, c *Config) error {
	if err := pause(ctx, c); err != nil {
		return err
	}
	if err := show(runPS(ctx, disableKeepaliveScript(c.KeepaliveTask()))); err != nil {
		return err
	}
	if err := terminateDistro(ctx, c.Distro); err != nil {
		return err
	}
	fmt.Printf("  changed %s terminated; WSL returns its memory once no other distro runs\n", c.Distro)
	return nil
}

func deleteCluster(ctx context.Context, c *Config, yes bool) error {
	target, err := kubeconfigPath()
	if err != nil {
		return err
	}
	fmt.Printf("delete removes, and cannot undo:\n"+
		"  k3s in %s (k3s-uninstall.sh), with every pod, volume and image\n"+
		"  /etc/rancher and /var/lib/rancher in %s\n"+
		"  the Scheduled Task %q\n"+
		"  the %s context, cluster and user in %s\n"+
		"it keeps the distro and nvidia-container-toolkit.\n",
		c.Distro, c.Distro, c.KeepaliveTask(), c.Node, target)
	if !yes {
		return errors.New("not deleted: run again with -yes")
	}
	if err := show(runPS(ctx, removeKeepaliveScript(c.KeepaliveTask()))); err != nil {
		return err
	}
	if err := show(runWSL(ctx, c.Distro, deleteScript())); err != nil {
		return err
	}
	if err := removeKubeconfig(ctx, target, c.Node); err != nil {
		return err
	}
	fmt.Printf("  changed removed context %s from %s\n", c.Node, target)
	return nil
}
