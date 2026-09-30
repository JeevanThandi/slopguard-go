package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// snippetLimit is the longest original/replacement shown in the text
// listing, in code points.
const snippetLimit = 40

// mutantSections are the text-report sections that list mutants, in display
// order. Every port uses the same titles.
var mutantSections = []struct {
	status MutantStatus
	title  string // Printf format with the count
}{
	{StatusSurvived, "Survived (%d) — tests still pass with these changes"},
	{StatusNoCoverage, "No coverage (%d) — no test runs these lines"},
	{StatusTimeout, "Timed out (%d) — counted as killed"},
	{StatusPending, "Mutants (%d, not run)"},
}

// PrettyMutationReport renders a MutationReport as text: a header, optional
// notes, the summary, then the survived, no-coverage and timed-out mutants
// (and, in a dry run, the pending ones). The layout is shared with every port.
func PrettyMutationReport(report MutationReport) string {
	var b strings.Builder
	b.WriteString(mutationHeader(report))
	b.WriteString("\n")
	if len(report.Notes) > 0 {
		b.WriteString("Notes\n")
		for _, note := range report.Notes {
			fmt.Fprintf(&b, "  • %s\n", note)
		}
		b.WriteString("\n")
	}
	b.WriteString(mutationSummarySection(report.Summary))
	for _, section := range mutantSections {
		var listed []Mutant
		for _, m := range report.Mutants {
			if m.Status == section.status {
				listed = append(listed, m)
			}
		}
		if len(listed) == 0 {
			continue
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, section.title+"\n", len(listed))
		for _, m := range listed {
			fmt.Fprintf(&b, "  %s\n", mutantListingLine(m))
		}
	}
	return b.String()
}

// JSONMutationReport encodes the report as indented JSON. Field order is
// fixed by the struct definitions (alphabetical). HTML escaping is off, so
// operators such as `<` and `&&` stay readable.
func JSONMutationReport(report MutationReport) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func mutationHeader(report MutationReport) string {
	const notRun = "(not run)"
	project, runner, timeout := notRun, notRun, notRun
	if report.ProjectRoot != nil {
		project = *report.ProjectRoot
	}
	if report.Runner != nil {
		runner = *report.Runner
	}
	if report.TimeoutSeconds != nil {
		timeout = FormatSeconds(*report.TimeoutSeconds) + "s per mutant"
	}
	return fmt.Sprintf("%s %s — mutation report (schema %s)\n", ToolName, report.ToolVersion, report.SchemaVersion) +
		fmt.Sprintf("source:    %s\n", report.SourceRoot) +
		fmt.Sprintf("project:   %s\n", project) +
		fmt.Sprintf("runner:    %s\n", runner) +
		fmt.Sprintf("timeout:   %s\n", timeout)
}

func mutationSummarySection(s MutationSummary) string {
	type row struct {
		label string
		value string
	}
	rows := []row{
		{"files:", strconv.Itoa(s.FileCount)},
		{"mutants:", strconv.Itoa(s.MutantCount)},
		{"killed:", strconv.Itoa(s.Killed)},
		{"timed out:", strconv.Itoa(s.TimedOut)},
		{"survived:", strconv.Itoa(s.Survived)},
		{"no coverage:", strconv.Itoa(s.NoCoverage)},
		{"compile errors:", strconv.Itoa(s.CompileErrors)},
		{"ignored:", strconv.Itoa(s.Ignored)},
	}
	if s.Pending > 0 {
		rows = append(rows, row{"pending:", strconv.Itoa(s.Pending)})
	}
	score := "n/a"
	if s.MutationScore != nil {
		score = fmt.Sprintf("%.2f%%", *s.MutationScore)
	}
	rows = append(rows, row{"score:", score})

	var b strings.Builder
	b.WriteString("Summary\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-16s%s\n", r.label, r.value)
	}
	return b.String()
}

func mutantListingLine(m Mutant) string {
	line := fmt.Sprintf("%s:%d:%d  %s  `%s` → `%s`", m.File, m.Line, m.Column, m.Operator,
		mutantSnippet(m.Original), mutantSnippet(m.Replacement))
	if m.Method != nil {
		line += "  " + *m.Method
	}
	return line
}

// mutantSnippet collapses whitespace runs to one space, so a multi-line
// statement stays on one line, and caps the result at snippetLimit code
// points (the first snippetLimit-1 plus `…`).
func mutantSnippet(text string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	flat := []rune(b.String())
	if len(flat) <= snippetLimit {
		return string(flat)
	}
	return string(flat[:snippetLimit-1]) + "…"
}

// FormatSeconds renders a number of seconds without a trailing ".0": 37 or
// 2.5, the way the JSON report encodes it.
func FormatSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}
