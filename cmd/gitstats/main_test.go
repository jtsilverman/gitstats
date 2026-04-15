package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkGitRepo creates a fixture repo with deterministic commits and files.
// Returns the repo root. Skips the test if git isn't installed.
func mkGitRepo(t *testing.T, opts repoOpts) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if env != nil {
			cmd.Env = append(os.Environ(), env...)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(nil, "init", "--initial-branch=main")
	run(nil, "config", "user.email", "test@example.com")
	run(nil, "config", "user.name", "Tester")
	run(nil, "config", "commit.gpgsign", "false")

	if opts.empty {
		return dir
	}

	// Write files.
	for path, content := range opts.files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// Write binary files.
	for path, content := range opts.binaries {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatalf("write bin %s: %v", path, err)
		}
	}
	if len(opts.files)+len(opts.binaries) > 0 {
		run(nil, "add", "-A")
	}

	for i, author := range opts.authors {
		env := []string{
			"GIT_AUTHOR_NAME=" + author,
			"GIT_AUTHOR_EMAIL=" + strings.ReplaceAll(strings.ToLower(author), " ", "_") + "@example.com",
			"GIT_AUTHOR_DATE=2024-01-0" + string(rune('1'+i%9)) + "T12:00:00+00:00",
			"GIT_COMMITTER_NAME=" + author,
			"GIT_COMMITTER_EMAIL=" + strings.ReplaceAll(strings.ToLower(author), " ", "_") + "@example.com",
			"GIT_COMMITTER_DATE=2024-01-0" + string(rune('1'+i%9)) + "T12:00:00+00:00",
		}
		if i == 0 {
			run(env, "commit", "-m", "commit "+author)
		} else {
			run(env, "commit", "--allow-empty", "-m", "commit "+author)
		}
	}
	return dir
}

type repoOpts struct {
	files    map[string]string
	binaries map[string][]byte
	authors  []string
	empty    bool
}

// runCLI exercises the run() entrypoint directly — no exec of a built
// binary — and returns (exitCode, stdout, stderr).
func runCLI(args []string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestE2E_NonexistentPath(t *testing.T) {
	code, _, stderr := runCLI([]string{filepath.Join(os.TempDir(), "gitstats-e2e-no-such-xyz")})
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "does not exist") {
		t.Errorf("stderr missing 'does not exist': %q", stderr)
	}
}

func TestE2E_NotARepo(t *testing.T) {
	dir := t.TempDir() // empty, no .git
	code, _, stderr := runCLI([]string{dir})
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not a git repository") {
		t.Errorf("stderr missing 'not a git repository': %q", stderr)
	}
}

func TestE2E_EmptyRepoZeroCounts(t *testing.T) {
	repo := mkGitRepo(t, repoOpts{empty: true})
	code, stdout, _ := runCLI([]string{"--json", repo})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var s struct {
		TotalCommits  int `json:"total_commits"`
		UniqueAuthors int `json:"unique_authors"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("bad json: %v\n%s", err, stdout)
	}
	if s.TotalCommits != 0 || s.UniqueAuthors != 0 {
		t.Errorf("got commits=%d authors=%d, want 0/0", s.TotalCommits, s.UniqueAuthors)
	}
}

func TestE2E_RealRepoJSONOutput(t *testing.T) {
	repo := mkGitRepo(t, repoOpts{
		files: map[string]string{
			"main.go":   "package main\nfunc main(){}\n",
			"README.md": "# hi\n",
		},
		authors: []string{"Alice", "Bob", "Alice"},
	})
	code, stdout, _ := runCLI([]string{"--json", repo})
	if code != 0 {
		t.Fatalf("exit=%d; stdout=%s", code, stdout)
	}
	var s struct {
		TotalCommits    int `json:"total_commits"`
		UniqueAuthors   int `json:"unique_authors"`
		TopContributors []struct {
			Name    string `json:"name"`
			Commits int    `json:"commits"`
		} `json:"top_contributors"`
		LOCByExtension []struct {
			Extension string `json:"extension"`
			Lines     int    `json:"lines"`
			Files     int    `json:"files"`
		} `json:"loc_by_extension"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if s.TotalCommits != 3 {
		t.Errorf("commits = %d, want 3", s.TotalCommits)
	}
	if s.UniqueAuthors != 2 {
		t.Errorf("authors = %d, want 2", s.UniqueAuthors)
	}
	if len(s.TopContributors) == 0 || s.TopContributors[0].Name != "Alice" {
		t.Errorf("top[0] = %+v, want Alice", s.TopContributors)
	}
	// LOC: .go and .md must appear.
	extSet := map[string]bool{}
	for _, e := range s.LOCByExtension {
		extSet[e.Extension] = true
	}
	if !extSet[".go"] || !extSet[".md"] {
		t.Errorf("missing .go or .md in LOC output: %+v", s.LOCByExtension)
	}
}

func TestE2E_BinaryFilesExcluded(t *testing.T) {
	// Embed a null byte so the binary heuristic fires.
	bin := append([]byte("hdr"), 0x00, 'x', 'y', 'z')
	repo := mkGitRepo(t, repoOpts{
		files:    map[string]string{"main.go": "package main\n"},
		binaries: map[string][]byte{"image.png": bin},
		authors:  []string{"Alice"},
	})
	code, stdout, _ := runCLI([]string{"--json", repo})
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, stdout)
	}
	if strings.Contains(stdout, `"extension": ".png"`) {
		t.Errorf("png reported in LOC — binary should be excluded:\n%s", stdout)
	}
}

func TestE2E_LockfilesExcluded(t *testing.T) {
	repo := mkGitRepo(t, repoOpts{
		files: map[string]string{
			"main.go": "package main\n",
			"go.sum":  strings.Repeat("lockfile-line\n", 200),
		},
		authors: []string{"Alice"},
	})
	code, stdout, _ := runCLI([]string{"--json", repo})
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, stdout)
	}
	// go.sum has ext ".sum"; if not skipped, the .sum extension would appear.
	if strings.Contains(stdout, `"extension": ".sum"`) {
		t.Errorf(".sum appears — lockfile not skipped:\n%s", stdout)
	}
}

func TestE2E_VersionFlag(t *testing.T) {
	code, stdout, _ := runCLI([]string{"--version"})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "gitstats") {
		t.Errorf("stdout missing 'gitstats': %q", stdout)
	}
}

func TestE2E_TerminalDefaultOutput(t *testing.T) {
	// Default (no --json) mode against a real repo prints the summary block.
	repo := mkGitRepo(t, repoOpts{
		files:   map[string]string{"main.go": "package main\n"},
		authors: []string{"Alice"},
	})
	code, stdout, _ := runCLI([]string{repo})
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	for _, want := range []string{"gitstats report", "Total commits", "Top contributors"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in terminal output", want)
		}
	}
}
