package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AnalysisOptions controls how the directory walk enumerates files. Globs use
// fnmatch-style semantics where `*` matches across path separators.
type AnalysisOptions struct {
	IncludeGlobs []string
	ExcludeGlobs []string
}

// DefaultExcludeGlobs is filtered out of every run unless --no-default-excludes
// is passed. Categories:
//
//   - Build / dependency / VCS dirs — never product code.
//   - Generated code — protobuf, gRPC gateways, mocks, and codegen output
//     produce branchy nonsense. (Files carrying the "DO NOT EDIT" header are
//     also skipped by the file analyzer.)
//   - Test code — *_test.go and testdata. A test file's CRAP isn't user-facing
//     risk; analyze it anyway with --no-default-excludes.
//   - Reference fixtures used to benchmark the analyzer itself.
var DefaultExcludeGlobs = []string{
	// Build / dependency / VCS dirs
	"**/vendor/**",
	"**/.git/**",
	"**/node_modules/**",
	"**/.cache/**",
	"**/bin/**",
	// Test code & data
	"**/*_test.go",
	"**/testdata/**",
	// Generated code
	"**/*.pb.go",
	"**/*.pb.gw.go",
	"**/*_grpc.pb.go",
	"**/*.gen.go",
	"**/*_gen.go",
	"**/zz_generated.*.go",
	"**/mock_*.go",
	"**/*_mock.go",
	"**/*_string.go", // stringer output
	// Reference fixtures (analyze on demand with an explicit --path).
	"**/sampleapps/**",
}

// DefaultAnalysisOptions returns options with the built-in excludes and no
// include filter.
func DefaultAnalysisOptions() AnalysisOptions {
	return AnalysisOptions{ExcludeGlobs: append([]string{}, DefaultExcludeGlobs...)}
}

// AnalyzeTree analyzes a directory tree (or a single file) rooted at root and
// returns one FileReport per analyzed file, with Path relative to root
// (forward-slash, no leading "./"), sorted by path.
func AnalyzeTree(root string, options AnalysisOptions) ([]FileReport, error) {
	files, rootPrefix, err := sourceFiles(root, options)
	if err != nil {
		return nil, err
	}
	reports := make([]FileReport, 0, len(files))
	for _, file := range files {
		report, err := AnalyzeFile(file, relativize(file, rootPrefix))
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Path < reports[j].Path })
	return reports, nil
}

// sourceFiles resolves root (a directory or a single file) and lists the Go
// files to scan as absolute paths, together with the directory their
// reported paths are relative to. Both AnalyzeTree and PlanMutants use it, so
// `analyze` and `mutate` always see the same files.
func sourceFiles(root string, options AnalysisOptions) (files []string, rootPrefix string, err error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, "", FileNotFound(root)
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		return nil, "", FileNotFound(rootAbs)
	}
	if !info.IsDir() {
		return []string{rootAbs}, filepath.Dir(rootAbs), nil
	}
	files, err = enumerate(rootAbs, options)
	if err != nil {
		return nil, "", err
	}
	return files, rootAbs, nil
}

func enumerate(rootPath string, options AnalysisOptions) ([]string, error) {
	var results []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return UnreadableFile(dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue // skip hidden files/dirs
			}
			abs := filepath.Join(dir, name)
			rel := relativize(abs, rootPath)
			if entry.IsDir() {
				if !MatchesAny(options.ExcludeGlobs, rel) {
					if err := walk(abs); err != nil {
						return err
					}
				}
				continue
			}
			if !entry.Type().IsRegular() {
				continue // don't follow symlinks
			}
			if shouldAnalyze(rel, options) {
				results = append(results, abs)
			}
		}
		return nil
	}
	if err := walk(rootPath); err != nil {
		return nil, err
	}
	return results, nil
}

func shouldAnalyze(rel string, options AnalysisOptions) bool {
	if !IsGoSource(rel) {
		return false
	}
	if MatchesAny(options.ExcludeGlobs, rel) {
		return false
	}
	if len(options.IncludeGlobs) > 0 && !MatchesAny(options.IncludeGlobs, rel) {
		return false
	}
	return true
}

// relativize returns the forward-slash path of abs under root.
func relativize(abs, root string) string {
	if abs == root {
		return filepath.Base(abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		rel = abs
	}
	return filepath.ToSlash(rel)
}
