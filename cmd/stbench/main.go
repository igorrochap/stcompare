package main

import (
	"errors"
	"os"

	"stcompare/internal/bench"
)

func main() {
	if err := bench.NewRootCommand().Execute(); err != nil {
		var exitErr *bench.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
