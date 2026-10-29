package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const probeNamespace = "cluster-probe"

// probePod is a pod that asks for the GPU the way every GPU workload here must: the RuntimeClass
// and a nvidia.com/gpu limit. Pass rc or gpu false to build the two shapes the admission policy
// must reject.
func probePod(name string, rc, gpu bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  restartPolicy: Never\n", name, probeNamespace)
	if rc {
		b.WriteString("  runtimeClassName: nvidia\n")
	}
	b.WriteString("  containers:\n    - name: probe\n      image: ubuntu:24.04\n      command: [nvidia-smi, -L]\n")
	b.WriteString("      env:\n        - {name: NVIDIA_DRIVER_CAPABILITIES, value: utility}\n")
	if gpu {
		b.WriteString("      resources:\n        limits: {nvidia.com/gpu: 1}\n")
	}
	return b.String()
}

func kubectl(ctx context.Context, kctx, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--context", kctx}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\r", "")), err
}

// probe proves the GPU path end to end: a correctly shaped pod lists the GPU, and the two
// half-shaped pods are rejected by the admission policy. It cleans up after itself.
func probe(ctx context.Context, kctx string) []check {
	var cs []check
	add := func(l level, layer, format string, a ...any) {
		cs = append(cs, check{l, layer, fmt.Sprintf(format, a...)})
	}
	_, _ = kubectl(ctx, kctx, "", "create", "namespace", probeNamespace)
	defer func() { _, _ = kubectl(ctx, kctx, "", "delete", "namespace", probeNamespace, "--wait=false") }()

	if out, err := kubectl(ctx, kctx, probePod("gpu", true, true), "apply", "-f", "-"); err != nil {
		add(failure, "probe pod", "apply: %s", out)
	} else if out, err := kubectl(ctx, kctx, "", "-n", probeNamespace, "wait", "pod/gpu",
		"--for=jsonpath={.status.phase}=Succeeded", "--timeout=180s"); err != nil {
		add(failure, "probe pod", "did not succeed: %s", out)
	} else if logs, _ := kubectl(ctx, kctx, "", "-n", probeNamespace, "logs", "gpu"); strings.Contains(logs, "GPU 0:") {
		add(pass, "probe pod", "%s", logs)
	} else {
		add(failure, "probe pod", "nvidia-smi -L printed %q", logs)
	}

	for _, p := range []struct {
		name    string
		rc, gpu bool
		why     string
	}{
		{"rc-only", true, false, "RuntimeClass without a GPU request"},
		{"gpu-only", false, true, "GPU request without the RuntimeClass"},
	} {
		out, err := kubectl(ctx, kctx, probePod(p.name, p.rc, p.gpu), "apply", "--dry-run=server", "-f", "-")
		if err != nil && strings.Contains(out, "gpu-request-and-runtimeclass-together") {
			add(pass, "policy", "rejects %s", p.why)
		} else {
			add(failure, "policy", "accepted %s: %s", p.why, out)
		}
	}
	return cs
}
