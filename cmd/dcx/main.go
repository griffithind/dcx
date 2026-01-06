// Package main provides the entry point for the dcx CLI.
package main

import (
	"os"

	"github.com/griffithind/dcx/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
