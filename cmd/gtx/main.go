//go:build unix

// Package main is the gtx CLI entrypoint.
package main

import (
	"os"

	"gotodex/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
