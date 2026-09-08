package main

import (
	"errors"
	"fmt"
	"os"

	"stcompare/internal/bench"
)

func main() {
	if err := bench.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var exitErr *bench.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
