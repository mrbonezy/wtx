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
	maybeStartInvocationUpdateCheck(args)
	cmd := newRootCommand(args)
	return cmd.Execute()
}
