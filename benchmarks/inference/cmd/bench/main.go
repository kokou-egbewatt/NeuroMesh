// Command bench records the stub-path baseline: it boots the runtime and the
// gateway through the integration harness, runs the matrix in
// benchmarks/inference, prints a markdown table for BASELINE.md and writes the
// same results as JSON.
//
//	bench -out results/run1.json -commit abc1234
//	bench -compare results/run1.json,results/run2.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/klauspost/cpuid/v2"

	"github.com/kokou-egbewatt/NeuroMesh/benchmarks/inference"
	"github.com/kokou-egbewatt/NeuroMesh/packages/logging"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// Meta is the environment a run is only comparable within.
type Meta struct {
	Date       string `json:"date"`
	Commit     string `json:"commit"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	CPU        string `json:"cpu"`
	LogicalCPU int    `json:"logical_cpus"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	Warmup     int    `json:"warmup"`
	Requests   int    `json:"requests"`
	PerWorker  int    `json:"min_per_worker"`
}

// Report is what -out writes and -compare reads.
type Report struct {
	Meta Meta            `json:"meta"`
	Rows []inference.Row `json:"rows"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func run() error {
	requests := flag.Int("requests", 20000, "recorded requests per case, raised to -per-worker times the concurrency when that is larger")
	perWorker := flag.Int("per-worker", 1000, "minimum recorded requests per worker, so high-concurrency cases run long enough to average out noise")
	warmup := flag.Int("warmup", 2000, "discarded warmup requests per case")
	out := flag.String("out", "", "write the results as JSON to this path")
	commit := flag.String("commit", "", "service commit to record (default: the build's VCS revision)")
	compare := flag.String("compare", "", "compare two result files, a.json,b.json, and fail on a p50 drift of 10% or more")
	tolerance := flag.Float64("tolerance", 0.10, "p50 drift -compare accepts")
	flag.Parse()

	if *compare != "" {
		return runCompare(os.Stdout, *compare, *tolerance)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The services log at info in JSON, as deployed, into io.Discard: the
	// formatting cost stays in the measured path, the terminal I/O does not.
	log, err := logging.New(io.Discard, "bench", "info", "json")
	if err != nil {
		return err
	}
	stack, err := harness.Start(ctx, harness.Options{Logger: log})
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	meta := environment(*commit, *warmup, *requests, *perWorker)
	fmt.Fprintf(os.Stderr, "bench: %s on %s, %d logical CPUs, commit %s\n", meta.GoVersion, meta.CPU, meta.LogicalCPU, meta.Commit)

	rep := Report{Meta: meta}
	for _, c := range inference.Matrix {
		t0 := time.Now()
		row, err := inference.RunCase(ctx, stack, c, *warmup, max(*requests, *perWorker*c.Concurrency))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "bench: %-12s p50 %8.1fµs  p99 %8.1fµs  %8.0f req/s  (%s)\n", row.Key(), row.P50us, row.P99us, row.RPS, time.Since(t0).Round(time.Millisecond))
		rep.Rows = append(rep.Rows, row)
	}

	if err := writeSummary(os.Stdout, rep); err != nil {
		return err
	}
	if *out != "" {
		return writeJSON(*out, rep)
	}
	return nil
}

func environment(commit string, warmup, requests, perWorker int) Meta {
	if commit == "" {
		commit = vcsRevision()
	}
	return Meta{
		Date:       time.Now().UTC().Format(time.DateOnly),
		Commit:     commit,
		GoVersion:  goruntime.Version(),
		OS:         goruntime.GOOS,
		Arch:       goruntime.GOARCH,
		CPU:        cpuid.CPU.BrandName,
		LogicalCPU: goruntime.NumCPU(),
		GOMAXPROCS: goruntime.GOMAXPROCS(0),
		Warmup:     warmup,
		Requests:   requests,
		PerWorker:  perWorker,
	}
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "unknown", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
			if len(rev) > 7 {
				rev = rev[:7]
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

func writeSummary(w io.Writer, rep Report) error {
	m := rep.Meta
	if _, err := fmt.Fprintf(w, "\n- Date: %s\n- Commit: `%s`\n- Go: %s %s/%s\n- CPU: %s (%d logical, GOMAXPROCS %d)\n- Requests per case: %d recorded (at least %d per worker) after %d warmup\n\n",
		m.Date, m.Commit, m.GoVersion, m.OS, m.Arch, m.CPU, m.LogicalCPU, m.GOMAXPROCS, m.Requests, m.PerWorker, m.Warmup); err != nil {
		return err
	}
	return inference.WriteMarkdown(w, rep.Rows)
}

func writeJSON(path string, rep Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func readJSON(path string) (Report, error) {
	var rep Report
	b, err := os.ReadFile(path)
	if err != nil {
		return rep, err
	}
	return rep, json.Unmarshal(b, &rep)
}

func runCompare(w io.Writer, spec string, tolerance float64) error {
	paths := strings.Split(spec, ",")
	if len(paths) != 2 {
		return fmt.Errorf("-compare wants two paths separated by a comma, got %q", spec)
	}
	a, err := readJSON(paths[0])
	if err != nil {
		return err
	}
	b, err := readJSON(paths[1])
	if err != nil {
		return err
	}
	all, over, err := inference.Compare(a.Rows, b.Rows, tolerance)
	if err != nil {
		return err
	}
	for _, d := range all {
		mark := "ok"
		if math.Abs(d.Change) >= tolerance {
			mark = "DRIFT"
		}
		if _, err := fmt.Fprintf(w, "%-12s p50 %8.1fµs -> %8.1fµs  %+6.1f%%  %s\n", d.Key, d.A, d.B, d.Change*100, mark); err != nil {
			return err
		}
	}
	if len(over) > 0 {
		return fmt.Errorf("%d of %d rows drifted by %.0f%% or more on p50: the runs are not repeatable on this machine", len(over), len(all), tolerance*100)
	}
	_, err = fmt.Fprintf(w, "repeatable: every p50 within %.0f%%\n", tolerance*100)
	return err
}
