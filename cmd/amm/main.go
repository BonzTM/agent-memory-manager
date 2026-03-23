package main

import (
	"fmt"
	"os"

	"github.com/joshd-04/agent-memory-manager/internal/adapters/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "amm: %v\n", err)
		os.Exit(1)
	}
}
