// Package core holds the pure analysis surface of slopguard-go: the wCRAP
// formula, the Go AST complexity analyzer, the data models, and the report
// formatters. It has no I/O beyond reading source files and spawns no
// subprocesses — the coverage subsystem (which drives `go test`) lives in the
// sibling coverage package so this package stays useful in no-coverage modes.
package core

// Version metadata. Edit Version for releases; CI asserts it matches the git
// tag. ToolName and SchemaVersion are stable identifiers shared with the
// TypeScript and Swift siblings (the JSON schema is byte-compatible).
const (
	// Version is the released semantic version of slopguard-go.
	Version = "0.2.0"
	// ToolName is the stable tool identifier emitted in reports.
	ToolName = "slopguard-go"
	// SchemaVersion is the JSON report schema version, shared across all
	// slopguard language ports (slopguard-swift, slopguard-typescript).
	SchemaVersion = "2"
	// MutationSchemaVersion is the schema version of the `mutate` JSON report
	// (reportType "mutation"). It is versioned apart from the CRAP report and
	// shared with every slopguard port.
	MutationSchemaVersion = "1"
)
