package coverage

import (
	"path/filepath"
	"strings"

	"github.com/JeevanThandi/slopguard-go/core"
)

// indexedFile holds the per-line coverage view for one source file.
type indexedFile struct {
	absPath  string
	basename string
	// lineHits maps a 1-based source line to whether it was covered. A line is
	// covered when at least one block spanning it ran (count > 0).
	lineHits        map[int]bool
	executableLines int
	coveredLines    int
}

// CoverageIndex is a fast lookup over a parsed Go coverage profile, built once
// after the test run and queried per method by the aggregator.
//
// A Go profile names files by import path (module path + relative path). To
// answer queries keyed by on-disk absolute path, the index resolves each
// profile entry to disk using the module path and project root, and keeps a
// basename map as a fallback for path mismatches (CI checkouts vs local clones,
// symlinks) — picking the candidate sharing the longest path suffix.
type CoverageIndex struct {
	byAbsPath  map[string]*indexedFile
	byBasename map[string][]*indexedFile

	TotalExecutableLines int
	TotalCoveredLines    int
}

var _ core.CoverageProvider = (*CoverageIndex)(nil)

// NewCoverageIndex builds an index from a profile, resolving the profile's
// import-path file names to absolute disk paths using modulePath (from go.mod)
// rooted at projectRoot.
func NewCoverageIndex(profile *Profile, modulePath, projectRoot string) *CoverageIndex {
	idx := &CoverageIndex{
		byAbsPath:  map[string]*indexedFile{},
		byBasename: map[string][]*indexedFile{},
	}
	for name, blocks := range profile.Files {
		lineHits := map[int]bool{}
		for _, blk := range blocks {
			hit := blk.Count > 0
			for ln := blk.StartLine; ln <= blk.EndLine; ln++ {
				lineHits[ln] = lineHits[ln] || hit
			}
		}
		covered := 0
		for _, hit := range lineHits {
			if hit {
				covered++
			}
		}
		abs := resolveDiskPath(name, modulePath, projectRoot)
		f := &indexedFile{
			absPath:         abs,
			basename:        filepath.Base(abs),
			lineHits:        lineHits,
			executableLines: len(lineHits),
			coveredLines:    covered,
		}
		idx.TotalExecutableLines += f.executableLines
		idx.TotalCoveredLines += f.coveredLines
		idx.byAbsPath[abs] = f
		idx.byBasename[f.basename] = append(idx.byBasename[f.basename], f)
	}
	return idx
}

// FileCount reports how many files the index covers.
func (idx *CoverageIndex) FileCount() int { return len(idx.byAbsPath) }

// MethodCoverage returns line coverage in [0, 100] for [line, endLine], or
// ok=false if the file is unknown or no executable line falls in the span.
func (idx *CoverageIndex) MethodCoverage(absolutePath string, line, endLine int) (float64, bool) {
	f := idx.lookup(absolutePath)
	if f == nil {
		return 0, false
	}
	executable, covered := 0, 0
	for ln, hit := range f.lineHits {
		if ln < line || ln > endLine {
			continue
		}
		executable++
		if hit {
			covered++
		}
	}
	if executable == 0 {
		return 0, false
	}
	return float64(covered) / float64(executable) * 100, true
}

// FileCoverage returns whole-file line coverage in [0, 100], or ok=false if
// unknown.
func (idx *CoverageIndex) FileCoverage(absolutePath string) (float64, bool) {
	f := idx.lookup(absolutePath)
	if f == nil || f.executableLines == 0 {
		return 0, false
	}
	return float64(f.coveredLines) / float64(f.executableLines) * 100, true
}

func (idx *CoverageIndex) lookup(absolutePath string) *indexedFile {
	if f, ok := idx.byAbsPath[absolutePath]; ok {
		return f
	}
	candidates := idx.byBasename[filepath.Base(absolutePath)]
	if len(candidates) == 1 {
		return candidates[0]
	}
	var best *indexedFile
	bestOverlap := 0
	for _, c := range candidates {
		overlap := sharedSuffixLen(absolutePath, c.absPath)
		if overlap > bestOverlap {
			best = c
			bestOverlap = overlap
		}
	}
	return best
}

// resolveDiskPath maps a profile file identifier (import path) to an absolute
// disk path. When the name lives under the module path, the module-relative
// remainder is joined onto projectRoot; otherwise the name is returned as-is
// (the basename fallback still allows a match).
func resolveDiskPath(name, modulePath, projectRoot string) string {
	if modulePath != "" {
		if rel, ok := strings.CutPrefix(name, modulePath+"/"); ok {
			return filepath.Join(projectRoot, filepath.FromSlash(rel))
		}
		if name == modulePath {
			return projectRoot
		}
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(projectRoot, filepath.FromSlash(name))
}

func sharedSuffixLen(a, b string) int {
	count := 0
	ai, bi := len(a)-1, len(b)-1
	for ai >= 0 && bi >= 0 && a[ai] == b[bi] {
		count++
		ai--
		bi--
	}
	return count
}
