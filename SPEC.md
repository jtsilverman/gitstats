---
mode: build
project: gitstats
branch: feat/gitstats
---

# gitstats

## Problem

Developers frequently need a quick snapshot of repository health and contributor distribution, but `git log` output is raw and requires manual piping through `awk`/`sort`/`uniq` to extract useful stats. Existing tools like `cloc` handle LOC but ignore contributor data, while `git-fame` is slow and Python-dependent. There is no single lightweight binary that combines contributor stats with LOC-by-extension in one pass.

## Solution

A Go CLI that takes a local git repo path and prints a clean terminal report: total commits, unique authors, top 5 contributors by commit count, first/last commit dates, and lines-of-code grouped by file extension. It shells out to `git log` for commit/author data (simpler and more reliable than go-git for read-only stats) and walks the working tree for LOC counting. Binary files are detected via null-byte heuristic in the first 8KB without reading full contents. Ships as a single binary via `go install`.

## Requirements

- **R1:** Accept a single positional argument (path to a local git repo). If omitted, default to current working directory.
- **R2:** Validate the path is a git repository (contains `.git/` or is inside one). Exit with code 1 and a clear error if not.
- **R3:** Report total commit count across all branches reachable from HEAD.
- **R4:** Report count of unique authors (by name, not email).
- **R5:** Report top 5 contributors ranked by commit count, showing name and count.
- **R6:** Report first and last commit dates in RFC 3339 format (YYYY-MM-DD).
- **R7:** Report lines of code grouped by file extension, sorted descending by line count. Only count files tracked in the working tree.
- **R8:** Skip binary files during LOC counting. A file is binary if the first 8192 bytes contain a null byte (0x00).
- **R9:** Skip lockfiles and generated files during LOC counting: `go.sum`, `package-lock.json`, `yarn.lock`, `Cargo.lock`, `poetry.lock`, `Gemfile.lock`, `composer.lock`, `Pipfile.lock`.
- **R10:** Support `--json` flag to output all stats as a single JSON object to stdout.
- **R11:** Exit with code 0 on success, code 1 on all errors. Errors go to stderr, stats go to stdout.
- **R12:** Complete execution in under 5 seconds for repos with fewer than 100,000 files in the working tree.

## Scope

**Building:**
- Go CLI binary `gitstats` (R1-R12)
- Terminal output with color via lipgloss (R5, R7)
- JSON output mode (R10)
- MIT license, README with install/usage/sample-output, `go install` target

**Not building:**
- Blame-based per-author LOC attribution (too slow for a quick stats tool)
- Remote repo support (clone-then-analyze; users can clone themselves)
- Historical LOC tracking over time (different tool, different scope)
- Web UI or TUI (terminal report is sufficient)
- Config files or `.gitstatsrc` (no configuration needed)

**Ship target:** Public GitHub repo `jtsilverman/gitstats` with README, MIT license, `go install github.com/jtsilverman/gitstats@latest`

## Stack

- **Language:** Go 1.24 (`~/go-sdk/go/bin`)
- **Module path:** `github.com/jtsilverman/gitstats`
- **Dependencies:** lipgloss (terminal colors). Everything else is stdlib.
- **Why Go:** Single binary, fast execution, good for CLI tools. Adds another Go project to Jake's portfolio alongside council and skillscore. Stdlib `os/exec` for git, `filepath.WalkDir` for tree walking, `encoding/json` for JSON output.

## Architecture

```
gitstats/
├── cmd/
│   └── gitstats/
│       └── main.go              # CLI entrypoint, flag parsing, orchestration
├── internal/
│   ├── gitlog/
│   │   ├── gitlog.go            # Shell out to git log, parse commit/author data
│   │   └── gitlog_test.go
│   ├── loc/
│   │   ├── loc.go               # Tree walk, binary detection, line counting
│   │   └── loc_test.go
│   └── report/
│       ├── terminal.go          # Color-coded terminal output
│       ├── json.go              # JSON output formatter
│       └── report_test.go
├── testdata/                    # Test fixtures (mini git repos, binary files)
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

### Data Model

```go
// Stats is the top-level report structure.
type Stats struct {
    TotalCommits   int              `json:"total_commits"`
    UniqueAuthors  int              `json:"unique_authors"`
    TopContributors []Contributor   `json:"top_contributors"`
    FirstCommit    string           `json:"first_commit"`  // YYYY-MM-DD
    LastCommit     string           `json:"last_commit"`   // YYYY-MM-DD
    LOCByExtension []ExtensionLOC  `json:"loc_by_extension"`
    TotalLOC       int              `json:"total_loc"`
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
```

### Git Log Strategy

Single `git log` invocation with a custom format string to extract all needed data in one pass:

```bash
git -C <path> log --format="%aN%x00%aI" --all
```

This produces `AuthorName\0ISO8601Date` per line. Parse once to get: commit count (line count), unique authors (deduplicated set), per-author counts (map), first/last dates (min/max of parsed dates).

### Binary Detection Strategy

For each file encountered during tree walk:
1. Open file
2. Read first 8192 bytes into a buffer
3. If any byte is `0x00`, classify as binary and skip
4. Otherwise, count newlines in the already-read buffer, then continue reading the rest of the file counting newlines
5. This means binary files are detected after reading at most 8KB, never their full contents

## Tasks

### Task 1: Project foundation and data types

**Files:** `go.mod` (create), `cmd/gitstats/main.go` (create), `internal/gitlog/gitlog.go` (create), `internal/loc/loc.go` (create), `internal/report/terminal.go` (create), `internal/report/json.go` (create)
**Do:** Initialize Go module as `github.com/jtsilverman/gitstats`. Create directory structure. Define data types (`Stats`, `Contributor`, `ExtensionLOC`) in a shared location (top of `gitlog.go` or a separate `types.go` -- implementer's choice, but types must be importable by both `gitlog` and `loc` packages; if they live in `gitlog`, `loc` would have a circular dep, so put them in a `internal/stats/types.go` package or in `cmd/`). Stub all package files with function signatures that return zero values. Wire `main.go` to parse a positional path argument and `--json` flag using stdlib `flag`. Validate path exists and contains `.git/`. Print "not implemented" and exit 0.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go build ./cmd/gitstats/ && ./gitstats . 2>&1 | grep -q "not implemented" && echo "ok"`
**Dependencies:** none
**Covers:** R1, R2 (partial)

### Task 2: Git log parsing

**Files:** `internal/gitlog/gitlog.go` (modify), `internal/gitlog/gitlog_test.go` (create)
**Do:** Implement `Collect(repoPath string) (*Stats, error)` that shells out to `git -C <path> log --format="%aN%x00%aI" --all` via `os/exec`. Parse output: count lines for total commits, build a `map[string]int` for author commit counts, extract min/max dates for first/last commit, sort authors descending and take top 5. Handle edge cases: empty repo (no commits), git not installed (exec error), path not a repo. Write tests using a temporary git repo created in `TestMain` or test setup (run `git init`, `git commit --allow-empty` with fake authors/dates in a temp dir).
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go test ./internal/gitlog/ -v -count=1`
**Dependencies:** Task 1
**Covers:** R3, R4, R5, R6, R11

### Task 3: LOC counting with binary detection

**Files:** `internal/loc/loc.go` (modify), `internal/loc/loc_test.go` (create), `testdata/` (create test fixtures)
**Do:** Implement `Count(repoPath string) ([]ExtensionLOC, int, error)` that walks the working tree using `filepath.WalkDir`. Skip `.git/` directory, skip symlinks, skip lockfiles (hardcoded set from R9). For each regular file: open it, read first 8192 bytes, check for null bytes. If binary, skip. Otherwise count newlines (continue reading past the initial buffer). Aggregate by extension (files without extensions go under `(none)`). Sort results descending by line count. Create test fixtures in `testdata/`: a small text file, a binary file (with embedded null bytes), a lockfile named `go.sum`. Write tests that create a temp directory with known files and verify counts.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go test ./internal/loc/ -v -count=1`
**Dependencies:** Task 1
**Covers:** R7, R8, R9, R12

### Task 4: Terminal output formatting

**Files:** `internal/report/terminal.go` (modify), `internal/report/report_test.go` (create)
**Do:** Implement `PrintTerminal(stats *Stats, w io.Writer)` using lipgloss for color. Layout: header with repo name, commit stats block (total commits, unique authors, date range), top contributors as a numbered list with commit counts, LOC table with extension, files, and lines columns. Use lipgloss styles: bold white for headers, cyan for numbers, dim for labels. Right-align numeric columns. Test by capturing output to a `bytes.Buffer` and asserting key strings are present (contributor names, extensions, numbers). Do not test exact ANSI codes.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go test ./internal/report/ -v -count=1`
**Dependencies:** Task 1
**Covers:** R5, R7

### Task 5: JSON output formatting

**Files:** `internal/report/json.go` (modify)
**Do:** Implement `PrintJSON(stats *Stats, w io.Writer) error` that marshals the Stats struct to indented JSON and writes to `w`. This is straightforward (`json.MarshalIndent`), but ensure all fields use the correct JSON tags from the data model. Add a test case in `report_test.go` that round-trips: marshal then unmarshal and compare.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go test ./internal/report/ -v -count=1`
**Dependencies:** Task 4
**Covers:** R10

### Task 6: Wire everything in main.go

**Files:** `cmd/gitstats/main.go` (modify)
**Do:** Replace the stub with real orchestration: parse args, validate repo path (R2), call `gitlog.Collect()`, call `loc.Count()`, merge results into a single `Stats` struct, call either `PrintJSON` or `PrintTerminal` based on `--json` flag. All errors to stderr with exit code 1. Handle: no git installed, not a repo, empty repo (print zeros, not an error). Add `--version` flag that prints version string.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go build ./cmd/gitstats/ && ./gitstats /Users/rock/Rock && ./gitstats --json /Users/rock/Rock | python3 -c "import sys,json; json.load(sys.stdin); print('valid json')" && ./gitstats /nonexistent 2>&1; test $? -eq 1 && echo "exit code ok"`
**Dependencies:** Task 2, Task 3, Task 5
**Covers:** R1, R2, R10, R11

### Task 7: End-to-end tests

**Files:** `cmd/gitstats/main_test.go` (create)
**Do:** Integration tests that build and run the binary against real and synthetic repos. Tests: (1) run against a temp repo with known commits and files, verify output contains expected numbers; (2) `--json` output parses as valid JSON with correct fields; (3) nonexistent path returns exit code 1; (4) path that exists but is not a git repo returns exit code 1; (5) empty repo (git init, no commits) returns zero counts without error; (6) repo with binary files shows them excluded from LOC; (7) repo with lockfiles shows them excluded from LOC. Use `os/exec` to run the built binary, or call main's internal functions directly.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && export PATH="$HOME/go-sdk/go/bin:$PATH" && go test ./... -v -count=1`
**Dependencies:** Task 6
**Covers:** R1-R12

### Task 8: README, LICENSE, and ship preparation

**Files:** `README.md` (create), `LICENSE` (create)
**Do:** Write README with sections: problem statement (one paragraph on why existing tools fall short), install (`go install github.com/jtsilverman/gitstats@latest`), usage (basic examples with and without `--json`), sample output (run gitstats against the Rock repo, capture and paste terminal output), how it works (brief: git log parsing + tree walk + binary detection), "What I Learned" (null-byte heuristic for binary detection, Go os/exec patterns, lipgloss terminal formatting). Write MIT license file with Jake Silverman 2026.
**Validate:** `cd /Users/rock/Rock/projects/gitstats && test -f README.md && test -f LICENSE && head -1 LICENSE | grep -q "MIT" && echo "ok"`
**Dependencies:** Task 6
**Covers:** ship target

### Task 9: Create GitHub repo and push

**Files:** n/a (git operations)
**Do:** Initialize git repo in `/Users/rock/Rock/projects/gitstats/`, create `.gitignore` (ignore binary, `gitstats` executable). Create GitHub repo `jtsilverman/gitstats` with description "A Go CLI that prints contributor and LOC statistics for any git repo." Push all code. Verify `go install github.com/jtsilverman/gitstats@latest` works (may need to tag v0.1.0 and push the tag for go install to resolve). Verify README renders on GitHub.
**Validate:** `gh repo view jtsilverman/gitstats --json name,description && cd /tmp && export PATH="$HOME/go-sdk/go/bin:$PATH" && go install github.com/jtsilverman/gitstats/cmd/gitstats@latest && echo "install ok"`
**Dependencies:** Task 7, Task 8
**Covers:** ship target

## One Hard Thing

**Accurate binary detection without reading full file contents.**

Why it is hard:
- Files can be gigabytes (disk images, model weights, video). Reading them fully to count lines wastes time and memory.
- The null-byte heuristic must be applied to a prefix (first 8KB), which means a file that starts with valid UTF-8 but contains null bytes at byte 9000 could be misclassified as text. In practice this is vanishingly rare because binary formats (images, compiled objects, archives) contain null bytes in their headers.
- The 8KB threshold matters: too small (e.g., 512 bytes) misses binary files with large text-like headers; too large wastes I/O on binary-heavy repos.
- Must handle files that are shorter than 8KB (read returns fewer bytes, that is still valid).
- Must not follow symlinks into infinite loops during tree walk.

Approach:
- Read exactly `min(8192, fileSize)` bytes via a single `io.ReadFull` call into a fixed buffer.
- Scan the buffer with `bytes.ContainsRune(buf, 0)` or a simple byte loop.
- If no null byte found, the buffer already contains counted lines; continue streaming the rest via `bufio.Scanner` or manual newline counting.
- Use `filepath.WalkDir` with `d.Type()&fs.ModeSymlink != 0` check to skip symlinks entirely.

Fallback:
- If the null-byte heuristic produces false positives on certain text files (e.g., UTF-16 encoded files contain null bytes), add a secondary check for known binary extensions (`.png`, `.jpg`, `.exe`, `.o`, `.so`, `.wasm`, etc.) as a fast path before the byte scan. This trades precision for speed but covers the common case.

## Test Strategy

**Failure modes:**
- Git not installed on the system: `exec.LookPath("git")` returns error. Test by mocking exec or checking error message. (R11)
- Path does not exist: `os.Stat` fails. Return exit 1 with "path does not exist." (R2, R11)
- Path exists but is not a git repo: no `.git/` found. Return exit 1 with "not a git repository." (R2, R11)
- Empty repo (no commits): `git log` returns empty output. Return Stats with zero counts, do not error. (R3, R4)
- Permission denied on a file during tree walk: skip the file, log a warning to stderr, continue counting. (R7)

**Edge cases:**
- File with no extension: aggregated under `(none)`. (R7)
- File with multiple dots (e.g., `foo.test.js`): extension is `.js` (last segment). (R7)
- File exactly 8192 bytes: binary detection reads full file, no second read needed. (R8)
- File with 0 bytes (empty): not binary, contributes 0 lines. (R7, R8)
- Repo with only binary files: LOC table is empty, total LOC is 0. (R7, R8)
- Author names with unicode characters (e.g., accented names): handled correctly by string comparison. (R4, R5)
- Extremely long author name: no truncation in JSON, truncate to 40 chars in terminal display. (R5)

**Integration boundaries:**
- `git log` output format contract: if git changes `--format` behavior, parsing breaks. Pinned to `%aN%x00%aI` which has been stable across git versions. (R3-R6)
- `filepath.WalkDir` behavior on macOS vs Linux: case-sensitivity of extensions, handling of `.DS_Store`. Skip dotfiles in `.git/` but not other dotfiles. (R7)

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| `git log --all` is slow on repos with millions of commits (e.g., linux kernel) | Medium | Accept the latency for now; this is a read-only stats tool, not a daemon. Document in README that very large repos may take longer. Could add `--branch` flag later to limit scope. |
| Lockfile skip list goes stale as new package managers emerge | Low | Hardcoded list covers the top 8 ecosystems. Easy to extend; users can open an issue. |
| lipgloss output breaks on terminals without color support | Low | lipgloss auto-detects `NO_COLOR` env var and dumb terminals. JSON mode is always available as a fallback. |
| `go install` requires a tagged release to resolve `@latest` | Low | Tag v0.1.0 immediately after push. Verify install works before marking ship complete. |

## Non-Obvious Production Risks

- **Shallow clone with truncated history**: A CI environment checks out `--depth=1`. `git log --all` returns exactly 1 commit. `gitstats` reports "1 total commit, 1 author" which is technically correct for the shallow history but misleading. User thinks the repo has 1 commit. **Mitigation:** Detect shallow repos by checking for `.git/shallow` file. If present, print a warning to stderr: "Warning: shallow clone detected, commit stats may be incomplete." Do not error, just warn. (R3, R4, R6)

- **Orphan branches with disjoint histories**: A repo has `main` and an orphan `gh-pages` branch with auto-generated HTML. `--all` traverses both, inflating commit count and including generated HTML in author stats. LOC walk only sees the checked-out branch, so commit stats and LOC stats describe different scopes. **Mitigation:** Document that commit stats cover all reachable refs while LOC stats cover the working tree only. Accept this as a known asymmetry; fixing it would require `git ls-tree` per-branch LOC counting which is a different (slower) tool.

- **Non-UTF-8 filenames on Linux**: A repo contains files named with raw bytes (Latin-1, Shift-JIS). `filepath.WalkDir` returns the raw byte string as the filename. `filepath.Ext()` works on bytes, so extension extraction is fine, but terminal output may render garbage. JSON output will contain invalid UTF-8, which `encoding/json` escapes as `\uFFFD` replacement characters. **Mitigation:** Sanitize filenames for terminal display using `strings.ToValidUTF8(name, "?")`. JSON marshaling handles it automatically via Go's built-in replacement.

- **Symlink loop in working tree**: A repo contains `a/b -> ../a`, creating an infinite directory loop. `filepath.WalkDir` does not follow symlinks by default, but if the repo uses hardlinks or bind mounts, the walk could revisit directories. **Mitigation:** `filepath.WalkDir` skips symlinks when checking `d.Type()`. For defense in depth, track visited inode numbers (via `os.SameFile`) and skip revisits. This also prevents double-counting hardlinked files.

- **Concurrent git operations during scan**: User runs `gitstats .` while `git rebase` is in progress. The working tree is in a dirty state with conflict markers, `.git/rebase-merge/` exists, and some files are partially written. `git log` still works (it reads the object store, not the working tree), but LOC counts may include conflict marker lines (`<<<<<<<`, `=======`, `>>>>>>>`). **Mitigation:** Check for `.git/rebase-merge/`, `.git/rebase-apply/`, or `.git/MERGE_HEAD`. If present, print a warning: "Warning: rebase/merge in progress, LOC counts may include conflict markers."
