// Command gitstats prints contributor and LOC statistics for a local git repo.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jtsilverman/gitstats/internal/gitlog"
	"github.com/jtsilverman/gitstats/internal/loc"
	"github.com/jtsilverman/gitstats/internal/report"
)

// version is set at build time via -ldflags '-X main.version=...'; defaults
// to "dev" for `go build` / `go install` from source.
var version = "dev"

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// run is separated from main for testability: callers can pass args, capture
// output, and inspect the exit code without shelling out to a built binary.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gitstats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit stats as JSON on stdout")
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: gitstats [--json] [--version] [repo-path]\n")
		fmt.Fprintf(stderr, "  repo-path: local git repository (default: current directory)\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		// flag already printed the error via stderr.
		return 1
	}

	if *showVersion {
		fmt.Fprintf(stdout, "gitstats %s\n", version)
		return 0
	}

	path := "."
	if fs.NArg() >= 1 {
		path = fs.Arg(0)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// R2: path must exist, be a directory, and contain a .git/ entry.
	info, err := os.Stat(abs)
	if err != nil {
		fmt.Fprintf(stderr, "error: path does not exist: %s\n", abs)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(stderr, "error: path is not a directory: %s\n", abs)
		return 1
	}
	gitPath := filepath.Join(abs, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		fmt.Fprintf(stderr, "error: not a git repository: %s\n", abs)
		return 1
	}

	// Non-obvious production risks — warn but don't fail.
	if _, err := os.Stat(filepath.Join(gitPath, "shallow")); err == nil {
		fmt.Fprintf(stderr, "warning: shallow clone detected, commit stats may be incomplete\n")
	}
	for _, marker := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		if _, err := os.Stat(filepath.Join(gitPath, marker)); err == nil {
			fmt.Fprintf(stderr, "warning: rebase/merge in progress, LOC counts may include conflict markers\n")
			break
		}
	}

	// Collect commit / author stats via `git log`.
	stats, err := gitlog.Collect(abs)
	if err != nil {
		if errors.Is(err, gitlog.ErrGitNotFound) {
			fmt.Fprintf(stderr, "error: git executable not found on PATH\n")
		} else {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 1
	}

	// Count LOC by walking the working tree.
	locs, totalLOC, err := loc.Count(abs)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	stats.LOCByExtension = locs
	stats.TotalLOC = totalLOC

	// Report.
	var writeErr error
	if *jsonOut {
		writeErr = report.PrintJSON(stats, stdout)
	} else {
		writeErr = report.PrintTerminal(stats, stdout)
	}
	if writeErr != nil {
		fmt.Fprintf(stderr, "error writing report: %v\n", writeErr)
		return 1
	}
	return 0
}

// ensure exec package isn't dropped by goimports if Collect's LookPath branch
// is later inlined — harmless reference, compiled away.
var _ = exec.Command
