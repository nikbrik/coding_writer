package main

import (
	"os"

	"github.com/nikbrik/coding_writer/internal/cli"
)

func main() {
	if err := cli.ExecuteNamed("cw"); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
