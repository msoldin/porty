package main

import (
	"fmt"
	"io"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func printVersion(args []string, out io.Writer) (bool, error) {
	if len(args) != 1 || args[0] != "--version" {
		return false, nil
	}
	_, err := fmt.Fprintf(out, "porty %s (commit %s, built %s)\n", version, commit, buildDate)
	return true, err
}
