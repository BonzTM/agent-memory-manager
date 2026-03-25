package main

import (
	"fmt"
	"os"

	"github.com/bonztm/agent-memory-manager/internal/adapters/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "amm: %v\n", err)
		os.Exit(1)
	}
}
