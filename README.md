# gitstats

A single-binary Go CLI that prints contributor and lines-of-code statistics for any local git repository.

## Problem

`git log` is raw — extracting top contributors or per-language LOC requires manual pipelines through `awk`, `sort`, `uniq`. `cloc` handles LOC but ignores authors. `git-fame` does both but is slow and Python-dependent. `gitstats` is a fast, dependency-light Go binary that produces both in one pass: commit counts, unique authors, top 5 contributors, first/last commit dates, and LOC grouped by file extension — all from a single command.

## Install

```bash
go install github.com/jtsilverman/gitstats/cmd/gitstats@latest
```

Requires Go 1.24+ and `git` on `PATH`.

## Usage

```bash
# Report on current directory
gitstats

# Report on a specific repo
gitstats /path/to/repo

# Emit JSON for piping to jq, a dashboard, or a CI report
gitstats --json /path/to/repo | jq '.top_contributors'
```

Flags:

| Flag | Meaning |
|------|---------|
| `--json` | Print a single JSON object instead of the terminal report. |
| `--version` | Print version and exit. |

Exit code: `0` on success, `1` on any error (path doesn't exist, not a git repo, git missing, etc).

## Sample Output

Run against a medium-sized polyglot repo:

```
gitstats report

Total commits:    438
Unique authors:   4
Date range:       2026-03-20 → 2026-04-15

Top contributors
  1. Rock                                     384 commits
  2. jtsilverman                              28 commits
  3. Jake Silverman                           24 commits
  4. git stash                                2 commits

Lines of code by extension
  ext             files      lines
  .py               223      68119
  .md               147      18280
  .ts                23       4006
  .rs                31       3534
  .json              21       1892
  .sh                33       1860
  .go                 9       1437
  .plist             27        841
  .js                 7        460
  .html               1        335
  total                     101300
```

Equivalent `--json`:

```json
{
  "total_commits": 438,
  "unique_authors": 4,
  "top_contributors": [
    { "name": "Rock", "commits": 384 },
    { "name": "jtsilverman", "commits": 28 }
  ],
  "first_commit": "2026-03-20",
  "last_commit": "2026-04-15",
  "loc_by_extension": [
    { "extension": ".py", "lines": 68119, "files": 223 }
  ],
  "total_loc": 101300
}
```

## How It Works

Two passes, orchestrated in `cmd/gitstats/main.go`.

**1. Commit and author stats** (`internal/gitlog`). A single invocation of

```
git -C <path> log --format="%aN%x00%aI" --all
```

produces one line per commit with `AuthorName\0ISO8601Date`. Parsed once: total commits, deduplicated author set, per-author counts, min/max dates.

**2. Lines of code by extension** (`internal/loc`). `git ls-files` gives the set of tracked paths. `filepath.WalkDir` walks the tree; untracked files, lockfiles (`go.sum`, `package-lock.json`, `yarn.lock`, `Cargo.lock`, `poetry.lock`, `Gemfile.lock`, `composer.lock`, `Pipfile.lock`), symlinks, and binaries are skipped. Line counts are aggregated by `filepath.Ext`.

## What I Learned

- **Binary detection via null-byte heuristic.** Reading gigabyte-sized files just to classify them as binary is a bad time. `gitstats` reads at most 8192 bytes per file, scans for `0x00`, and aborts the read the instant a null byte appears. This is the same heuristic `grep` and `git diff` use. It misclassifies UTF-16 text files (which legitimately contain nulls), but the tradeoff is worth it for 100x speedup on binary-heavy repos. See `internal/loc/loc.go:countLines`.
- **`git log --format=%aN%x00%aI`.** The `%x00` (null byte) separator is safe against every character that can appear in an author name or ISO-8601 date. Commas, tabs, and pipes all fail on at least one real-world input.
- **lipgloss + `NO_COLOR` awareness.** `lipgloss` auto-detects `NO_COLOR` and non-TTY stdout, so the same code path works for interactive use and for `| less` or CI logs without a flag.
- **Worktree vs tracked-tree asymmetry.** `git log --all` sees every reachable commit; the working-tree walk only sees the checked-out branch. The report documents this: commit stats cover all refs, LOC stats cover the current working tree only.

## License

MIT — see [LICENSE](LICENSE).
