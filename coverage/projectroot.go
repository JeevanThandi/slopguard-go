package coverage

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverProjectRoot walks up from a source path looking for the nearest
// go.mod — the module root that `go test` should execute from when the user
// hasn't said otherwise. Returns ("", false) when no go.mod is found.
//
// Treating the analyzed path as a hint to find the real module root means
// `slopguard-go analyze --path ./internal/foo` runs the tests of the module
// that owns that directory, not whatever the current working directory is.
func DiscoverProjectRoot(searchingFrom string) (string, bool) {
	dir, err := filepath.Abs(searchingFrom)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for i := 0; i < 64; i++ {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// ModulePath reads the `module` directive from the go.mod at projectRoot.
// Returns "" if it can't be read.
func ModulePath(projectRoot string) string {
	f, err := os.Open(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
