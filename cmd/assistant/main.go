package main

import (
	"os"

	"github.com/nikbrik/coding_writer/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
