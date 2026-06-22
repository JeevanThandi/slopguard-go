// Package coverage drives `go test` to produce a coverage profile, parses it,
// and joins per-method line coverage onto the analysis. Coverage is treated as
// an artifact slopguard generates — never a user input — mirroring how
// slopguard-swift drives xcodebuild and slopguard-typescript drives vitest/jest.
package coverage

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"

	"github.com/JeevanThandi/slopguard-go/core"
)

// ProfileBlock is one coverage block from a Go coverage profile: a contiguous
// span of statements with a hit count.
type ProfileBlock struct {
	StartLine int
	EndLine   int
	NumStmts  int
	Count     int
}

// Profile is a parsed Go coverage profile. Files are keyed by the profile's
// own file identifier, which is the package import path plus the file's
// basename (e.g. "github.com/you/mod/pkg/file.go").
type Profile struct {
	Mode  string
	Files map[string][]ProfileBlock
}

// profileLine matches a single data line of a Go coverage profile:
//
//	<name>:<startLine>.<startCol>,<endLine>.<endCol> <numStmts> <count>
var profileLine = regexp.MustCompile(`^(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)$`)

// ParseProfile parses the textual Go coverage profile format emitted by
// `go test -coverprofile`.
func ParseProfile(text string) (*Profile, error) {
	p := &Profile{Files: map[string][]ProfileBlock{}}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	first := true
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				p.Mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
				continue
			}
			// Tolerate a missing mode line rather than failing the run.
		}
		m := profileLine.FindStringSubmatch(line)
		if m == nil {
			return nil, core.CoverageDecodeFailed(&malformedLine{line})
		}
		name := m[1]
		startLine, _ := strconv.Atoi(m[2])
		endLine, _ := strconv.Atoi(m[3])
		numStmts, _ := strconv.Atoi(m[4])
		count, _ := strconv.Atoi(m[5])
		p.Files[name] = append(p.Files[name], ProfileBlock{
			StartLine: startLine,
			EndLine:   endLine,
			NumStmts:  numStmts,
			Count:     count,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, core.CoverageDecodeFailed(err)
	}
	return p, nil
}

type malformedLine struct{ line string }

func (e *malformedLine) Error() string {
	return "malformed coverage profile line: " + e.line
}
