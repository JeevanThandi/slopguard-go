package coverage

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/JeevanThandi/slopguard-go/core"
)

// CoverageMode selects how the pipeline obtains its coverage signal.
type CoverageMode int

const (
	// CoverageAuto drives `go test` to gather coverage (the default).
	CoverageAuto CoverageMode = iota
	// CoveragePrebuilt ingests an existing Go coverage profile.
	CoveragePrebuilt
	// CoverageNone skips coverage entirely; every method reads 0%.
	CoverageNone
)

// CoverageSource bundles the user's coverage choice and its parameters.
type CoverageSource struct {
	Mode CoverageMode
	// ProfileFile is the prebuilt profile to ingest (CoveragePrebuilt).
	ProfileFile string
	// ProjectDir overrides module-root discovery (CoverageAuto).
	ProjectDir string
	// Packages is the `go test` package pattern (CoverageAuto); defaults to
	// "./..." when empty.
	Packages string
}

// PipelineArgs bundles inputs to Run.
type PipelineArgs struct {
	SourcePath string
	Coverage   CoverageSource
	Threshold  float64
	Options    core.AnalysisOptions
	Progress   *core.ProgressReporter
	// Runner is injectable for tests; nil uses a default `go test` runner.
	Runner Runner
}

// Run executes the full analyze → coverage-join → CRAP-report pipeline against
// a directory or single Go source file.
func Run(args PipelineArgs) (core.CrapReport, error) {
	progress := args.Progress
	if progress == nil {
		progress = core.SilentReporter()
	}
	sourcePath, _ := filepath.Abs(args.SourcePath)

	progress.Phase("walking " + sourcePath)
	fileReports, err := core.AnalyzeTree(sourcePath, args.Options)
	if err != nil {
		return core.CrapReport{}, err
	}
	methodCount := 0
	for _, fr := range fileReports {
		methodCount += len(fr.Methods)
	}
	progress.Phase(plural(len(fileReports), "source file", "source files") + ", " +
		plural(methodCount, "method", "methods") + " parsed")

	resolved, err := resolveCoverage(args, sourcePath, progress)
	if err != nil {
		return core.CrapReport{}, err
	}
	defer resolved.cleanup()

	var provider core.CoverageProvider
	var coverageDataPath *string
	notes := append([]string{}, resolved.notes...)

	if resolved.profilePath != "" {
		progress.Phase("parsing coverage data")
		index, perr := loadCoverageIndex(resolved.profilePath, resolved.modulePath, resolved.projectRoot)
		if perr != nil {
			return core.CrapReport{}, perr
		}
		if index.FileCount() == 0 {
			notes = append(notes, "The test run produced no per-file coverage data — "+
				"check that the selected packages contain tests. All methods are being reported at 0%.")
		} else {
			provider = index
			if !resolved.ephemeral {
				abs, _ := filepath.Abs(resolved.profilePath)
				coverageDataPath = &abs
			}
		}
	}

	return core.Aggregate(core.AggregateArgs{
		FileReports:      fileReports,
		SourceRoot:       sourcePath,
		CoverageDataPath: coverageDataPath,
		Threshold:        args.Threshold,
		Coverage:         provider,
		Notes:            notes,
	}), nil
}

type resolvedCoverage struct {
	profilePath string
	ephemeral   bool
	modulePath  string
	projectRoot string
	notes       []string
	cleanup     func()
}

func resolveCoverage(args PipelineArgs, sourcePath string, progress *core.ProgressReporter) (resolvedCoverage, error) {
	noop := func() {}
	switch args.Coverage.Mode {
	case CoverageNone:
		return resolvedCoverage{cleanup: noop}, nil

	case CoveragePrebuilt:
		// A prebuilt profile still needs the module root to resolve its
		// import-path file names to disk; discover it from the source path.
		root, modulePath := projectContext(args.Coverage.ProjectDir, sourcePath)
		return resolvedCoverage{
			profilePath: args.Coverage.ProfileFile,
			projectRoot: root,
			modulePath:  modulePath,
			cleanup:     noop,
		}, nil

	default: // CoverageAuto
		root := args.Coverage.ProjectDir
		if root == "" {
			discovered, ok := DiscoverProjectRoot(sourcePath)
			if !ok {
				return resolvedCoverage{}, core.ProjectRootNotFound(sourcePath)
			}
			root = discovered
		}
		modulePath := ModulePath(root)
		packages := args.Coverage.Packages
		if packages == "" {
			packages = "./..."
		}

		tempDir, err := os.MkdirTemp("", "slopguard-")
		if err != nil {
			return resolvedCoverage{}, core.Unsupported("could not create temp dir: " + err.Error())
		}
		cleanup := func() { os.RemoveAll(tempDir) }

		var runner Runner = args.Runner
		if runner == nil {
			runner = &TestRunner{}
		}
		outcome, err := runner.RunTests(root, tempDir, packages, progress)
		if err != nil {
			cleanup()
			return resolvedCoverage{}, err
		}

		var notes []string
		if !outcome.TestsPassed {
			notes = append(notes, "Some tests failed during the coverage run — coverage reflects the failing run.")
		}
		if outcome.ProfilePath == "" {
			notes = append(notes, "The test run completed but no coverage data was produced — "+
				"either no tests matched the selected packages or coverage tooling is missing. "+
				"All methods are being reported at 0%.")
		}
		return resolvedCoverage{
			profilePath: outcome.ProfilePath,
			ephemeral:   true,
			modulePath:  modulePath,
			projectRoot: root,
			notes:       notes,
			cleanup:     cleanup,
		}, nil
	}
}

// projectContext resolves the module root and module path for prebuilt mode.
func projectContext(projectDir, sourcePath string) (root, modulePath string) {
	root = projectDir
	if root == "" {
		if discovered, ok := DiscoverProjectRoot(sourcePath); ok {
			root = discovered
		} else {
			root, _ = filepath.Abs(sourcePath)
		}
	}
	return root, ModulePath(root)
}

func loadCoverageIndex(profilePath, modulePath, projectRoot string) (*CoverageIndex, error) {
	text, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, core.UnreadableFile(profilePath, err)
	}
	profile, err := ParseProfile(string(text))
	if err != nil {
		return nil, err
	}
	return NewCoverageIndex(profile, modulePath, projectRoot), nil
}

func plural(n int, singular, plural string) string {
	word := plural
	if n == 1 {
		word = singular
	}
	return strconv.Itoa(n) + " " + word
}
