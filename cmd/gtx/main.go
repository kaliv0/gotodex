//go:build unix

package main

import (
	"os"

	"gotodex/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
