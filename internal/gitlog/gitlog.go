// Package gitlog shells out to git to collect commit and author statistics.
//
// This package also hosts the shared data types (Stats, Contributor,
// ExtensionLOC) used across gitlog, loc, and report. They live here so
// loc and report can import them without either package needing to
// import the other. gitlog imports nothing from the project, so no
// circular dep is possible.
package gitlog

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Stats is the top-level report structure.
type Stats struct {
	TotalCommits    int            `json:"total_commits"`
	UniqueAuthors   int            `json:"unique_authors"`
	TopContributors []Contributor  `json:"top_contributors"`
	FirstCommit    string          `json:"first_commit"` // YYYY-MM-DD
	LastCommit     string          `json:"last_commit"`  // YYYY-MM-DD
	LOCByExtension []ExtensionLOC  `json:"loc_by_extension"`
	TotalLOC       int             `json:"total_loc"`
}

// Contributor is an author with their commit count.
type Contributor struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

// ExtensionLOC is line count for a single file extension.
type ExtensionLOC struct {
	Extension string `json:"extension"`
	Lines     int    `json:"lines"`
	Files     int    `json:"files"`
}

// ErrGitNotFound indicates the `git` binary was not on PATH.
var ErrGitNotFound = errors.New("git executable not found on PATH")

// topN is the number of contributors to include in TopContributors.
const topN = 5

// Collect runs `git -C repoPath log --format=%aN\x00%aI --all` and parses the
// output to populate commit count, unique author count, top-N contributors,
// and first/last commit dates.
//
// An empty repo (no commits yet) is a valid state and returns zero counts
// with empty slices and empty date strings — not an error. That way callers
// can still produce a useful report.
//
// If `git` is not installed, returns ErrGitNotFound. Any other exec error
// (path not a repo, permission denied) is wrapped and returned.
func Collect(repoPath string) (*Stats, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, ErrGitNotFound
	}

	cmd := exec.Command("git", "-C", repoPath, "log", "--format=%aN%x00%aI", "--all")
	out, err := cmd.Output()
	if err != nil {
		// exec.ExitError carries stderr — include it so callers see "not a
		// git repository" messages from git itself.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("git log failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return parseLog(out), nil
}

// parseLog is split out for direct unit testing without needing a real repo.
// Input format: one line per commit, "AuthorName\x00ISO8601Date".
func parseLog(out []byte) *Stats {
	s := &Stats{TopContributors: []Contributor{}}

	// Lines may end with \n; Split leaves a trailing empty. We skip blanks.
	lines := strings.Split(string(out), "\n")
	authorCounts := make(map[string]int)
	var firstDate, lastDate time.Time

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			// Malformed row (shouldn't happen with the stable %x00 separator),
			// skip rather than crash — we'd rather under-count than blow up.
			continue
		}
		name, dateStr := parts[0], parts[1]
		s.TotalCommits++
		authorCounts[name]++

		// time.RFC3339 accepts %aI output, e.g. 2025-01-15T12:34:56-08:00.
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			continue
		}
		if firstDate.IsZero() || t.Before(firstDate) {
			firstDate = t
		}
		if lastDate.IsZero() || t.After(lastDate) {
			lastDate = t
		}
	}

	s.UniqueAuthors = len(authorCounts)
	s.TopContributors = rankTopN(authorCounts, topN)

	if !firstDate.IsZero() {
		s.FirstCommit = firstDate.Format("2006-01-02")
	}
	if !lastDate.IsZero() {
		s.LastCommit = lastDate.Format("2006-01-02")
	}
	return s
}

// rankTopN returns up to n contributors sorted by commit count descending,
// breaking ties by name ascending (so results are stable across runs).
func rankTopN(counts map[string]int, n int) []Contributor {
	all := make([]Contributor, 0, len(counts))
	for name, c := range counts {
		all = append(all, Contributor{Name: name, Commits: c})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Commits != all[j].Commits {
			return all[i].Commits > all[j].Commits
		}
		return all[i].Name < all[j].Name
	})
	if len(all) > n {
		all = all[:n]
	}
	return all
}
