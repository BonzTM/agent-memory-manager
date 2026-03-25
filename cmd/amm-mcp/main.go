package main

import (
	"fmt"
	"os"

	"github.com/bonztm/agent-memory-manager/internal/adapters/mcp"
)

func main() {
	if err := mcp.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "amm-mcp: %v\n", err)
		os.Exit(1)
	}
}
