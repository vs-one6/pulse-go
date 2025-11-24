package main

import (
	"fmt"
	"os"

	"github.com/vishnucs/pulse-go/internals"
)

func main() {
	// Check if we're being called as a child process (for container isolation)
	if len(os.Args) > 1 && os.Args[1] == "child" {
		if err := internals.ChildProcess(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Container error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Normal CLI execution
	if err := Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
