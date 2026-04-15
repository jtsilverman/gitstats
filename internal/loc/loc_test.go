package loc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRepo builds a temp directory mimicking a working tree: a .git dir
// (so .git-skipping is exercised), a couple of text source files, a binary
// file with embedded nulls, a lockfile, a file with no extension, and a
// symlink. Returns the root path.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mkfile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdirall: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Real Git-like metadata to exercise the .git skip.
	mkfile(".git/config", "should not be counted\nshould not\n")

	// Source files.
	mkfile("main.go", "package main\n\nfunc main() {}\n")           // 3 lines, .go
	mkfile("util.go", "package main\n\n// comment\nvar x = 1\n")    // 4 lines, .go
	mkfile("README.md", "# Title\n\nbody\n")                        // 3 lines, .md
	mkfile("nested/thing.go", "package nested\n")                   // 1 line, .go

	// File with no extension.
	mkfile("Makefile", "all:\n\techo hi\n") // 2 lines, (none)

	// A lockfile that MUST be skipped.
	lockContent := strings.Repeat("line\n", 500)
	mkfile("go.sum", lockContent)

	// Binary file — starts with text-looking header, then a null byte in
	// the prefix window. Must be classified binary and skipped.
	binContent := append([]byte("PNG header-ish "), 0x00, 'x', 'y', 'z', '\n')
	if err := os.WriteFile(filepath.Join(dir, "image.png"), binContent, 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	// Empty file — not binary, 0 lines.
	mkfile("empty.txt", "")

	// Symlink — must be skipped entirely.
	if err := os.Symlink("main.go", filepath.Join(dir, "link.go")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	return dir
}

func TestCount_AggregatesByExtension(t *testing.T) {
	dir := fixtureRepo(t)
	result, total, err := Count(dir)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}

	byExt := map[string]struct {
		lines, files int
	}{}
	for _, e := range result {
		byExt[e.Extension] = struct{ lines, files int }{e.Lines, e.Files}
	}

	// main.go (3) + util.go (4) + nested/thing.go (1) = 8 lines across 3 files.
	// link.go (symlink) must NOT add a fourth file.
	if got := byExt[".go"]; got.lines != 8 || got.files != 3 {
		t.Errorf(".go = %+v, want {8, 3}", got)
	}
	// README.md = 3 lines, 1 file.
	if got := byExt[".md"]; got.lines != 3 || got.files != 1 {
		t.Errorf(".md = %+v, want {3, 1}", got)
	}
	// Makefile has no extension.
	if got := byExt["(none)"]; got.lines != 2 || got.files != 1 {
		t.Errorf("(none) = %+v, want {2, 1}", got)
	}
	// empty.txt = 0 lines, 1 file.
	if got := byExt[".txt"]; got.lines != 0 || got.files != 1 {
		t.Errorf(".txt = %+v, want {0, 1}", got)
	}
	// .png (binary) must NOT appear.
	if _, ok := byExt[".png"]; ok {
		t.Errorf(".png present — binary file should have been skipped")
	}
	// .sum (go.sum lockfile) must NOT appear.
	if _, ok := byExt[".sum"]; ok {
		t.Errorf(".sum present — lockfile should have been skipped")
	}
	// .git/config must not be counted — nothing under (no-extension) inflates.
	// If it had been counted, (none) would be 4 lines not 2.

	if total != 8+3+2+0 {
		t.Errorf("total = %d, want %d", total, 8+3+2+0)
	}
}

func TestCount_SortedDescByLines(t *testing.T) {
	dir := fixtureRepo(t)
	result, _, err := Count(dir)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	for i := 1; i < len(result); i++ {
		if result[i-1].Lines < result[i].Lines {
			t.Errorf("result not sorted desc: %+v", result)
		}
	}
}

func TestCount_OnlyBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	// Two binaries; empty LOC table expected, total 0.
	for _, name := range []string{"a.bin", "b.bin"} {
		content := []byte{'h', 'e', 'l', 'l', 'o', 0x00, 'w', 'o'}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	result, total, err := Count(dir)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("result = %+v, want empty", result)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
}

func TestCount_NonexistentPath(t *testing.T) {
	_, _, err := Count(filepath.Join(os.TempDir(), "gitstats-loc-no-such-path-xyz"))
	if err == nil {
		t.Fatalf("expected error on nonexistent path")
	}
}

func TestCountLines_LargeTextFile(t *testing.T) {
	// File bigger than binaryProbeSize, no null bytes. Exercises the
	// streaming branch. 20000 lines of "x\n" = 40000 bytes > 8192 prefix.
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString("x\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lines, isBin, err := countLines(path)
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if isBin {
		t.Errorf("isBinary true for text file")
	}
	if lines != 20000 {
		t.Errorf("lines = %d, want 20000", lines)
	}
}

func TestCountLines_NullByteAtEdge(t *testing.T) {
	// Null byte exactly at the last position of the probe window must still
	// classify as binary.
	dir := t.TempDir()
	path := filepath.Join(dir, "edge.bin")
	content := make([]byte, binaryProbeSize)
	for i := range content {
		content[i] = 'a'
	}
	content[binaryProbeSize-1] = 0x00
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, isBin, err := countLines(path)
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if !isBin {
		t.Errorf("expected binary classification for null byte at prefix edge")
	}
}
