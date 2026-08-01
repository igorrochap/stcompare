package main

import (
	"errors"
	"os"

	"stcompare/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		var exitErr *cli.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
