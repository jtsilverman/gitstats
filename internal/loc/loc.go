// Package loc walks a working tree and counts lines of code per file extension,
// skipping binary files (null-byte heuristic) and known lockfiles.
package loc

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jtsilverman/gitstats/internal/gitlog"
)

// binaryProbeSize is the prefix length (bytes) scanned for null bytes when
// classifying a file as binary. 8 KiB is large enough to catch common binary
// formats (images, archives, compiled objects) whose headers contain nulls,
// without wasting I/O on binary-heavy repos.
const binaryProbeSize = 8192

// lockfiles is the hardcoded set of generated/lockfile names skipped during
// LOC counting (R9). These are package-manager outputs: huge, machine-written,
// and not representative of a human's line count.
var lockfiles = map[string]struct{}{
	"go.sum":             {},
	"package-lock.json":  {},
	"yarn.lock":          {},
	"Cargo.lock":         {},
	"poetry.lock":        {},
	"Gemfile.lock":       {},
	"composer.lock":      {},
	"Pipfile.lock":       {},
}

// Count walks the working tree at repoPath, counts newlines in every
// non-binary, non-lockfile regular file, and aggregates by extension.
//
// Returns a slice sorted descending by line count (ties broken by extension
// name ascending for stability), plus the sum of lines across all files.
//
// Binary classification uses the null-byte heuristic on the first
// binaryProbeSize bytes (R8). Files are counted from the working tree only —
// no git ls-tree — so untracked files in the working copy are included.
//
// Errors during individual-file reads are logged to stderr and the file is
// skipped (so one unreadable file doesn't abort the whole report).
// Root-level walk errors (path doesn't exist, permission on the repo dir)
// are returned.
func Count(repoPath string) ([]gitlog.ExtensionLOC, int, error) {
	type agg struct {
		lines int
		files int
	}
	byExt := make(map[string]*agg)
	total := 0

	// Validate the root up front so "nonexistent path" is a real error instead
	// of a best-effort warning swallowed by the walk loop.
	if _, err := os.Stat(repoPath); err != nil {
		return nil, 0, fmt.Errorf("stat %s: %w", repoPath, err)
	}

	// R7: only count files tracked in the working tree. We ask git for the
	// set of tracked paths (repo-relative) and admit only those during walk.
	// If `git ls-files` fails (not a repo, git missing), tracked==nil and
	// we fall back to walking everything — this keeps the loc package
	// testable against non-repo fixtures used by its own unit tests.
	tracked := trackedSet(repoPath)

	walkErr := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A permission error on a subdirectory shouldn't abort the walk;
			// log once and skip the offender.
			fmt.Fprintf(os.Stderr, "warning: walk error at %s: %v\n", path, err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			// Skip .git metadata — it's not source. Allow other dotdirs
			// (e.g. .github/, .config/) to be counted.
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks entirely: they can escape the repo or form loops.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		// Skip other non-regular files (sockets, devices, named pipes).
		if !d.Type().IsRegular() {
			return nil
		}

		name := d.Name()
		if _, isLock := lockfiles[name]; isLock {
			return nil
		}

		if tracked != nil {
			rel, relErr := filepath.Rel(repoPath, path)
			if relErr != nil {
				return nil
			}
			// git ls-files uses forward slashes on all platforms; normalise.
			relFwd := filepath.ToSlash(rel)
			if _, ok := tracked[relFwd]; !ok {
				return nil
			}
		}

		lines, isBinary, err := countLines(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot read %s: %v\n", path, err)
			return nil
		}
		if isBinary {
			return nil
		}

		ext := filepath.Ext(name)
		if ext == "" {
			ext = "(none)"
		}
		a, ok := byExt[ext]
		if !ok {
			a = &agg{}
			byExt[ext] = a
		}
		a.lines += lines
		a.files++
		total += lines
		return nil
	})
	if walkErr != nil {
		return nil, 0, fmt.Errorf("walk %s: %w", repoPath, walkErr)
	}

	out := make([]gitlog.ExtensionLOC, 0, len(byExt))
	for ext, a := range byExt {
		out = append(out, gitlog.ExtensionLOC{Extension: ext, Lines: a.lines, Files: a.files})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lines != out[j].Lines {
			return out[i].Lines > out[j].Lines
		}
		return out[i].Extension < out[j].Extension
	})
	return out, total, nil
}

// trackedSet returns the set of git-tracked paths (repo-relative, forward
// slashes) under repoPath, or nil if git is unavailable / repoPath is not a
// repo. Callers treat nil as "no filter" so the loc package stays useful
// against non-repo fixtures in its own unit tests.
func trackedSet(repoPath string) map[string]struct{} {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	out, err := exec.Command("git", "-C", repoPath, "ls-files").Output()
	if err != nil {
		return nil
	}
	set := make(map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			set[line] = struct{}{}
		}
	}
	return set
}

// countLines reads file at path and returns (newline count, isBinary, error).
//
// Implementation: read up to binaryProbeSize bytes via io.ReadFull into a
// stack-sized buffer. If any byte is 0x00, classify as binary and return
// immediately — the rest of the file is never read. Otherwise, count
// newlines in the prefix and stream the remainder in 32 KiB chunks, counting
// newlines. This guarantees binary files are touched for at most 8 KiB
// regardless of their size.
func countLines(path string) (int, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	var probe [binaryProbeSize]byte
	n, err := io.ReadFull(f, probe[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, false, err
	}
	prefix := probe[:n]
	if bytes.IndexByte(prefix, 0) >= 0 {
		return 0, true, nil
	}

	lines := bytes.Count(prefix, []byte{'\n'})

	// Stream the rest (if the file was larger than the probe).
	if n == binaryProbeSize {
		buf := make([]byte, 32*1024)
		for {
			m, rerr := f.Read(buf)
			if m > 0 {
				lines += bytes.Count(buf[:m], []byte{'\n'})
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return 0, false, rerr
			}
		}
	}
	return lines, false, nil
}
