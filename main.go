package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "wtx error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := newRootCommand(args)
	return cmd.Execute()
}
