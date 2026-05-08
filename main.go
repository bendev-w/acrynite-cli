package main

import (
	"fmt"
	"os"

	"acrynite-cli/internal"
)

func main() {
	if err := internal.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
