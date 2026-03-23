package main

import (
	"fmt"
	"os"

	"github.com/joshd-04/agent-memory-manager/internal/adapters/mcp"
)

func main() {
	if err := mcp.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "amm-mcp: %v\n", err)
		os.Exit(1)
	}
}
