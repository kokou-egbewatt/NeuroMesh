package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// renameKubeconfig turns k3s's kubeconfig (everything named "default", server 127.0.0.1) into
// one named after the node, served on localhost. WSL forwards localhost to the distro, so the
// Windows side does not depend on the node IP that moves on every WSL restart.
func renameKubeconfig(raw []byte, name string) ([]byte, error) {
	var kc map[string]any
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("k3s kubeconfig: %w", err)
	}
	for _, list := range []string{"clusters", "users", "contexts"} {
		items, _ := kc[list].([]any)
		if len(items) != 1 {
			return nil, fmt.Errorf("k3s kubeconfig: expected one entry in %s, found %d", list, len(items))
		}
		item, _ := items[0].(map[string]any)
		item["name"] = name
		if list == "clusters" {
			if cl, ok := item["cluster"].(map[string]any); ok {
				if s, ok := cl["server"].(string); ok {
					cl["server"] = strings.Replace(s, "://127.0.0.1:", "://localhost:", 1)
				}
			}
		}
		if list == "contexts" {
			if ctx, ok := item["context"].(map[string]any); ok {
				ctx["cluster"] = name
				ctx["user"] = name
			}
		}
	}
	kc["current-context"] = name
	return yaml.Marshal(kc)
}

// kubeconfigPath is the file Windows kubectl reads: the first entry of KUBECONFIG, or
// ~/.kube/config.
func kubeconfigPath() (string, error) {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return filepath.SplitList(v)[0], nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// mergeKubeconfig writes ours into target, replacing entries with the same name and keeping every
// other context. kubectl does the merge; ours is listed first so its entries win.
func mergeKubeconfig(ctx context.Context, target string, ours []byte) (changed bool, err error) {
	before, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, err
	}
	merged := ours
	if len(before) > 0 {
		tmp, err := os.CreateTemp("", "cluster-kubeconfig-*.yaml")
		if err != nil {
			return false, err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if _, err := tmp.Write(ours); err != nil {
			_ = tmp.Close()
			return false, err
		}
		if err := tmp.Close(); err != nil {
			return false, err
		}
		cmd := exec.CommandContext(ctx, "kubectl", "config", "view", "--flatten", "--raw")
		cmd.Env = append(os.Environ(), "KUBECONFIG="+tmp.Name()+string(os.PathListSeparator)+target)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		merged, err = cmd.Output()
		if err != nil {
			return false, fmt.Errorf("kubectl config view: %w: %s", err, stderr.String())
		}
	}
	if bytes.Equal(before, merged) {
		return false, nil
	}
	return true, os.WriteFile(target, merged, 0o600)
}

// removeKubeconfig deletes the node's context, cluster and user from target, if present.
func removeKubeconfig(ctx context.Context, target, name string) error {
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	for _, kind := range []string{"delete-context", "delete-cluster", "delete-user"} {
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", target, "config", kind, name)
		if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "not") {
			return fmt.Errorf("kubectl config %s %s: %w: %s", kind, name, err, out)
		}
	}
	return nil
}
