package main

import (
	"fmt"
	"os"

	"github.com/ivanohotnikov/markdown-editor/cmd/markdown-editor/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
