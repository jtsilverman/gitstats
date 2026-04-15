package gitlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo creates a throw-away git repo in t.TempDir() with the given
// commits. Each commit is passed as "author|date|msg" so we can build
// deterministic fixtures without depending on the current user's git
// identity. Uses --allow-empty so we don't touch the filesystem.
// The "|" separator avoids colliding with colons inside ISO-8601 dates.
func makeRepo(t *testing.T, commits []string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed, skipping repo-dependent test")
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
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(nil, "init", "--initial-branch=main")
	run(nil, "config", "user.email", "test@example.com")
	run(nil, "config", "user.name", "Test Default")
	// Disable gpg signing that some user configs force globally.
	run(nil, "config", "commit.gpgsign", "false")

	for _, spec := range commits {
		parts := strings.SplitN(spec, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("bad commit spec: %q (want author|date|msg)", spec)
		}
		author, date, msg := parts[0], parts[1], parts[2]
		env := []string{
			"GIT_AUTHOR_NAME=" + author,
			"GIT_AUTHOR_EMAIL=" + strings.ReplaceAll(strings.ToLower(author), " ", "_") + "@example.com",
			"GIT_AUTHOR_DATE=" + date,
			"GIT_COMMITTER_NAME=" + author,
			"GIT_COMMITTER_EMAIL=" + strings.ReplaceAll(strings.ToLower(author), " ", "_") + "@example.com",
			"GIT_COMMITTER_DATE=" + date,
		}
		run(env, "commit", "--allow-empty", "-m", msg)
	}
	return dir
}

func TestCollect_BasicCounts(t *testing.T) {
	repo := makeRepo(t, []string{
		"Alice|2024-01-01T10:00:00+00:00|first",
		"Bob|2024-02-01T10:00:00+00:00|second",
		"Alice|2024-03-01T10:00:00+00:00|third",
		"Alice|2024-04-01T10:00:00+00:00|fourth",
		"Carol|2024-05-01T10:00:00+00:00|fifth",
	})
	s, err := Collect(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if s.TotalCommits != 5 {
		t.Errorf("TotalCommits = %d, want 5", s.TotalCommits)
	}
	if s.UniqueAuthors != 3 {
		t.Errorf("UniqueAuthors = %d, want 3", s.UniqueAuthors)
	}
	if s.FirstCommit != "2024-01-01" {
		t.Errorf("FirstCommit = %q, want 2024-01-01", s.FirstCommit)
	}
	if s.LastCommit != "2024-05-01" {
		t.Errorf("LastCommit = %q, want 2024-05-01", s.LastCommit)
	}
	if len(s.TopContributors) != 3 {
		t.Fatalf("TopContributors len = %d, want 3", len(s.TopContributors))
	}
	if s.TopContributors[0].Name != "Alice" || s.TopContributors[0].Commits != 3 {
		t.Errorf("top[0] = %+v, want Alice/3", s.TopContributors[0])
	}
}

func TestCollect_EmptyRepo(t *testing.T) {
	// git init with no commits must return zero counts, not an error.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	s, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect on empty repo: unexpected err %v", err)
	}
	if s.TotalCommits != 0 {
		t.Errorf("empty repo TotalCommits = %d, want 0", s.TotalCommits)
	}
	if s.UniqueAuthors != 0 {
		t.Errorf("empty repo UniqueAuthors = %d, want 0", s.UniqueAuthors)
	}
	if s.FirstCommit != "" || s.LastCommit != "" {
		t.Errorf("empty repo dates = %q/%q, want empty", s.FirstCommit, s.LastCommit)
	}
}

func TestCollect_NotARepo(t *testing.T) {
	// A real directory that isn't a git repo must produce an error.
	dir := t.TempDir()
	_, err := Collect(dir)
	if err == nil {
		t.Fatalf("Collect on non-repo: expected error, got nil")
	}
}

func TestCollect_NonexistentPath(t *testing.T) {
	_, err := Collect(filepath.Join(os.TempDir(), "definitely-does-not-exist-gitstats-xyz"))
	if err == nil {
		t.Fatalf("Collect on nonexistent path: expected error, got nil")
	}
}

func TestParseLog_TopNCapped(t *testing.T) {
	// 7 unique authors — TopContributors must be capped at 5.
	input := strings.Join([]string{
		"A\x002024-01-01T00:00:00Z",
		"A\x002024-01-02T00:00:00Z",
		"A\x002024-01-03T00:00:00Z",
		"B\x002024-01-04T00:00:00Z",
		"B\x002024-01-05T00:00:00Z",
		"C\x002024-01-06T00:00:00Z",
		"D\x002024-01-07T00:00:00Z",
		"E\x002024-01-08T00:00:00Z",
		"F\x002024-01-09T00:00:00Z",
		"G\x002024-01-10T00:00:00Z",
		"",
	}, "\n")
	s := parseLog([]byte(input))
	if s.TotalCommits != 10 {
		t.Errorf("TotalCommits = %d, want 10", s.TotalCommits)
	}
	if s.UniqueAuthors != 7 {
		t.Errorf("UniqueAuthors = %d, want 7", s.UniqueAuthors)
	}
	if len(s.TopContributors) != 5 {
		t.Errorf("TopContributors len = %d, want 5 (capped)", len(s.TopContributors))
	}
	// A (3), B (2), then C/D/E/F/G all at 1 — ties broken alphabetically,
	// so third through fifth must be C, D, E.
	if s.TopContributors[0].Name != "A" || s.TopContributors[1].Name != "B" ||
		s.TopContributors[2].Name != "C" || s.TopContributors[3].Name != "D" ||
		s.TopContributors[4].Name != "E" {
		t.Errorf("tie-break order wrong: %+v", s.TopContributors)
	}
}

func TestParseLog_UnicodeAuthorName(t *testing.T) {
	// Author with accented characters — must count as one unique author.
	input := "Zoë\x002024-01-01T00:00:00Z\nZoë\x002024-02-01T00:00:00Z\n"
	s := parseLog([]byte(input))
	if s.UniqueAuthors != 1 {
		t.Errorf("UniqueAuthors = %d, want 1", s.UniqueAuthors)
	}
	if s.TopContributors[0].Name != "Zoë" {
		t.Errorf("name = %q, want Zoë", s.TopContributors[0].Name)
	}
}
