package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jtsilverman/gitstats/internal/gitlog"
)

// decodeJSON is a tiny test helper so the round-trip test reads cleanly.
func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }

func sampleStats() *gitlog.Stats {
	return &gitlog.Stats{
		TotalCommits:  42,
		UniqueAuthors: 3,
		TopContributors: []gitlog.Contributor{
			{Name: "Alice", Commits: 20},
			{Name: "Bob", Commits: 15},
			{Name: "Carol", Commits: 7},
		},
		FirstCommit: "2024-01-01",
		LastCommit:  "2024-12-31",
		LOCByExtension: []gitlog.ExtensionLOC{
			{Extension: ".go", Lines: 1200, Files: 8},
			{Extension: ".md", Lines: 50, Files: 2},
		},
		TotalLOC: 1250,
	}
}

func TestPrintTerminal_ContainsKeyFacts(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintTerminal(sampleStats(), &buf); err != nil {
		t.Fatalf("PrintTerminal: %v", err)
	}
	out := buf.String()
	wantSubstrings := []string{
		"gitstats report",
		"42",   // total commits
		"3",    // unique authors
		"2024-01-01",
		"2024-12-31",
		"Alice",
		"Bob",
		"Carol",
		".go",
		"1200",
		".md",
		"50",
		"1250", // total LOC
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q. got:\n%s", want, out)
		}
	}
}

func TestPrintTerminal_EmptyStats(t *testing.T) {
	// Empty repo — must not panic, must render the "(no commits)"/"(none)"
	// placeholders.
	s := &gitlog.Stats{TopContributors: []gitlog.Contributor{}, LOCByExtension: []gitlog.ExtensionLOC{}}
	var buf bytes.Buffer
	if err := PrintTerminal(s, &buf); err != nil {
		t.Fatalf("PrintTerminal: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "no commits") {
		t.Errorf("expected 'no commits' placeholder, got:\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected '(none)' placeholder, got:\n%s", out)
	}
}

func TestTruncate_LongNameShortened(t *testing.T) {
	long := strings.Repeat("x", 60)
	got := truncate(long, maxNameWidth)
	// maxNameWidth runes total (including the ellipsis), so character count
	// equals maxNameWidth.
	if n := len([]rune(got)); n != maxNameWidth {
		t.Errorf("len = %d, want %d", n, maxNameWidth)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

func TestTruncate_UnicodeRuneAware(t *testing.T) {
	// Four-rune, multi-byte input; truncate to 3 should preserve runes, not
	// slice mid-byte.
	got := truncate("αβγδ", 3)
	if len([]rune(got)) != 3 {
		t.Errorf("rune count = %d, want 3", len([]rune(got)))
	}
}

// nullWriter returns io.ErrShortWrite to exercise the error path.
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errShort }

var errShort = &customErr{"short write"}

type customErr struct{ s string }

func (e *customErr) Error() string { return e.s }

func TestPrintTerminal_WriterError(t *testing.T) {
	err := PrintTerminal(sampleStats(), errWriter{})
	if err == nil {
		t.Fatalf("expected writer error, got nil")
	}
}

func TestPrintJSON_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	orig := sampleStats()
	if err := PrintJSON(orig, &buf); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}

	var decoded gitlog.Stats
	if err := decodeJSON(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\nraw:%s", err, buf.String())
	}

	if decoded.TotalCommits != orig.TotalCommits {
		t.Errorf("TotalCommits = %d, want %d", decoded.TotalCommits, orig.TotalCommits)
	}
	if decoded.UniqueAuthors != orig.UniqueAuthors {
		t.Errorf("UniqueAuthors = %d, want %d", decoded.UniqueAuthors, orig.UniqueAuthors)
	}
	if len(decoded.TopContributors) != len(orig.TopContributors) {
		t.Errorf("TopContributors len = %d, want %d", len(decoded.TopContributors), len(orig.TopContributors))
	}
	if len(decoded.LOCByExtension) != len(orig.LOCByExtension) {
		t.Errorf("LOCByExtension len = %d, want %d", len(decoded.LOCByExtension), len(orig.LOCByExtension))
	}
	if decoded.TotalLOC != orig.TotalLOC {
		t.Errorf("TotalLOC = %d, want %d", decoded.TotalLOC, orig.TotalLOC)
	}
	if decoded.FirstCommit != orig.FirstCommit || decoded.LastCommit != orig.LastCommit {
		t.Errorf("dates = %q/%q, want %q/%q",
			decoded.FirstCommit, decoded.LastCommit, orig.FirstCommit, orig.LastCommit)
	}
}

func TestPrintJSON_NilSlicesRenderAsEmpty(t *testing.T) {
	// Nil slices must serialise as "[]" not "null" — downstream jq consumers
	// care.
	var buf bytes.Buffer
	s := &gitlog.Stats{} // all nil slices
	if err := PrintJSON(s, &buf); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "null") {
		t.Errorf("JSON contains 'null' for nil slices:\n%s", out)
	}
	for _, want := range []string{`"top_contributors": []`, `"loc_by_extension": []`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestPrintJSON_NilStats(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSON(nil, &buf); err == nil {
		t.Fatalf("expected error on nil stats")
	}
}
