// Package report formats gitstats output for humans (terminal) and machines (JSON).
package report

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/jtsilverman/gitstats/internal/gitlog"
)

// maxNameWidth truncates extremely long author names (e.g. 200-char pasted
// names from broken author-ident fixtures) in the terminal report. Spec Test
// Strategy edge case: terminal display truncates to 40, JSON keeps full name.
const maxNameWidth = 40

// Lipgloss auto-detects NO_COLOR and non-TTY environments, stripping ANSI
// when appropriate. These styles are safe to use unconditionally.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	labelStyle  = lipgloss.NewStyle().Faint(true)
	numberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
)

// PrintTerminal writes a color-coded human-readable report to w. Returns
// error only if the underlying writer fails; otherwise always nil.
func PrintTerminal(s *gitlog.Stats, w io.Writer) error {
	var b strings.Builder

	// --- Summary block ----------------------------------------------------
	b.WriteString(headerStyle.Render("gitstats report") + "\n\n")

	fmt.Fprintf(&b, "%s %s\n",
		labelStyle.Render("Total commits:   "),
		numberStyle.Render(fmt.Sprintf("%d", s.TotalCommits)),
	)
	fmt.Fprintf(&b, "%s %s\n",
		labelStyle.Render("Unique authors:  "),
		numberStyle.Render(fmt.Sprintf("%d", s.UniqueAuthors)),
	)

	dateRange := "(no commits)"
	if s.FirstCommit != "" && s.LastCommit != "" {
		dateRange = fmt.Sprintf("%s → %s", s.FirstCommit, s.LastCommit)
	}
	fmt.Fprintf(&b, "%s %s\n",
		labelStyle.Render("Date range:      "),
		numberStyle.Render(dateRange),
	)

	// --- Top contributors -------------------------------------------------
	b.WriteString("\n" + headerStyle.Render("Top contributors") + "\n")
	if len(s.TopContributors) == 0 {
		b.WriteString(labelStyle.Render("  (none)") + "\n")
	} else {
		for i, c := range s.TopContributors {
			name := truncate(c.Name, maxNameWidth)
			fmt.Fprintf(&b, "  %d. %-*s %s\n",
				i+1,
				maxNameWidth, name,
				numberStyle.Render(fmt.Sprintf("%d commits", c.Commits)),
			)
		}
	}

	// --- LOC by extension -------------------------------------------------
	b.WriteString("\n" + headerStyle.Render("Lines of code by extension") + "\n")
	if len(s.LOCByExtension) == 0 {
		b.WriteString(labelStyle.Render("  (none)") + "\n")
	} else {
		// Column layout: extension (left) | files (right) | lines (right).
		fmt.Fprintf(&b, "  %-12s %8s %10s\n",
			labelStyle.Render("ext"),
			labelStyle.Render("files"),
			labelStyle.Render("lines"),
		)
		for _, e := range s.LOCByExtension {
			fmt.Fprintf(&b, "  %-12s %8d %10s\n",
				e.Extension, e.Files,
				numberStyle.Render(fmt.Sprintf("%d", e.Lines)),
			)
		}
		fmt.Fprintf(&b, "  %-12s %8s %10s\n",
			labelStyle.Render("total"),
			"",
			numberStyle.Render(fmt.Sprintf("%d", s.TotalLOC)),
		)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return nil
}

// truncate shortens s to n runes, appending an ellipsis when truncated. Rune-
// aware so multi-byte author names don't get chopped mid-character.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}
