package httpapi

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenAPISpecParity(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	sourcePath := filepath.Join(repoRoot, "spec", "v1", "openapi.json")
	embeddedPath := filepath.Join(repoRoot, "internal", "adapters", "http", "openapi_spec.json")

	sourceSpec, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source OpenAPI spec %q: %v", sourcePath, err)
	}
	embeddedSpec, err := os.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("read embedded OpenAPI spec %q: %v", embeddedPath, err)
	}

	if !bytes.Equal(sourceSpec, embeddedSpec) {
		t.Fatalf("OpenAPI specs differ: %s and %s must match byte-for-byte", sourcePath, embeddedPath)
	}
}
