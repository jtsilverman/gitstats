package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jtsilverman/gitstats/internal/gitlog"
)

// PrintJSON marshals s to indented JSON and writes it to w, followed by a
// trailing newline (so pipelines like `gitstats --json | jq .` don't warn
// about a missing final newline).
//
// All struct fields already carry JSON tags in gitlog. We guarantee that
// nil slices (empty repo, empty LOC table) are serialised as "[]" rather
// than "null" by normalising them before marshal — that's a friendlier
// contract for downstream JSON consumers.
func PrintJSON(s *gitlog.Stats, w io.Writer) error {
	if s == nil {
		return fmt.Errorf("PrintJSON: nil stats")
	}
	// Normalise nil slices to empty slices so JSON output is never "null".
	out := *s
	if out.TopContributors == nil {
		out.TopContributors = []gitlog.Contributor{}
	}
	if out.LOCByExtension == nil {
		out.LOCByExtension = []gitlog.ExtensionLOC{}
	}

	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stats: %w", err)
	}
	buf = append(buf, '\n')
	if _, err := w.Write(buf); err != nil {
		return err
	}
	return nil
}
