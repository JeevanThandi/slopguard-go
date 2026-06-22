// Command slopguard-go is the CLI entry point. It is a thin shim over the cli
// package so the wiring stays testable in-process.
package main

import (
	"os"

	"github.com/JeevanThandi/slopguard-go/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
