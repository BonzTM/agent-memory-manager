package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "amm-integration-*")
	if err != nil {
		panic(err)
	}

	binaryPath = filepath.Join(tmpDir, "amm")
	goBin, err := exec.LookPath("go")
	if err != nil {
		fallback := "/usr/local/go/bin/go"
		if _, statErr := os.Stat(fallback); statErr == nil {
			goBin = fallback
		} else {
			_ = os.RemoveAll(tmpDir)
			panic("go binary not found in PATH and no fallback at /usr/local/go/bin/go")
		}
	}

	cmd := exec.Command(goBin, "build", "-tags", "fts5", "-o", binaryPath, "./cmd/amm")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("build failed: " + string(out) + ": " + err.Error())
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func runAMM(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "AMM_DB_PATH="+dbPath)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parseEnvelope(t *testing.T, out string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		idx := strings.Index(out, "{\n  \"ok\"")
		if idx < 0 {
			idx = strings.Index(out, "{\"ok\"")
		}
		if idx < 0 {
			t.Fatalf("failed to parse JSON envelope: %v\noutput=%s", err, out)
		}

		dec := json.NewDecoder(bytes.NewBufferString(out[idx:]))
		if decErr := dec.Decode(&env); decErr != nil {
			t.Fatalf("failed to parse extracted JSON envelope: %v\noutput=%s", decErr, out)
		}
	}
	return env
}

func envelopeResult(t *testing.T, out string, wantCommand string) map[string]any {
	t.Helper()
	env := parseEnvelope(t, out)
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("expected ok=true envelope: %s", out)
	}
	if cmd, _ := env["command"].(string); cmd != wantCommand {
		t.Fatalf("expected command %q, got %q", wantCommand, cmd)
	}
	result, _ := env["result"].(map[string]any)
	if result == nil {
		t.Fatalf("expected result payload in envelope: %s", out)
	}
	return result
}

func envelopeError(t *testing.T, out string, wantCommand string) map[string]any {
	t.Helper()
	env := parseEnvelope(t, out)
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("expected ok=false envelope: %s", out)
	}
	if cmd, _ := env["command"].(string); cmd != wantCommand {
		t.Fatalf("expected error command %q, got %q", wantCommand, cmd)
	}
	errObj, _ := env["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error payload in envelope: %s", out)
	}
	return errObj
}

func TestCLI_Init(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	out, err := runAMM(t, dbPath, "init")
	if err != nil {
		t.Fatalf("init failed: %s: %v", out, err)
	}

	result := envelopeResult(t, out, "init")
	if result["status"] != "initialized" {
		t.Fatalf("expected initialized status, got %v", result["status"])
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}
}

func TestCLI_RememberAndRecall(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	if out, err := runAMM(t, dbPath, "init"); err != nil {
		t.Fatalf("init: %s: %v", out, err)
	}

	out, err := runAMM(t, dbPath, "remember",
		"--type", "fact",
		"--scope", "global",
		"--body", "Go uses goroutines for concurrency",
		"--tight", "Go concurrency model",
		"--json")
	if err != nil {
		t.Fatalf("remember: %s: %v", out, err)
	}
	_ = envelopeResult(t, out, "remember")

	out, err = runAMM(t, dbPath, "recall", "goroutines", "--json")
	if err != nil {
		t.Fatalf("recall: %s: %v", out, err)
	}
	result := envelopeResult(t, out, "recall")

	serialized, mErr := json.Marshal(result)
	if mErr != nil {
		t.Fatalf("marshal recall result: %v", mErr)
	}
	text := strings.ToLower(string(serialized))
	if !strings.Contains(text, "goroutines") && !strings.Contains(text, "concurrency") {
		t.Fatalf("recall output should contain stored memory, got: %s", out)
	}
}

func TestCLI_IngestEvent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	if out, err := runAMM(t, dbPath, "init"); err != nil {
		t.Fatalf("init: %s: %v", out, err)
	}

	envelope := `{"version":"amm.v1","command":"ingest_event","payload":{"kind":"message_user","source_system":"test","content":"Hello world"}}`
	envFile := filepath.Join(tmpDir, "req.json")
	if err := os.WriteFile(envFile, []byte(envelope), 0o644); err != nil {
		t.Fatalf("write envelope: %v", err)
	}

	out, err := runAMM(t, dbPath, "run", "--in", envFile)
	if err != nil {
		t.Fatalf("ingest event envelope run: %s: %v", out, err)
	}
	result := envelopeResult(t, out, "run")
	if _, ok := result["id"].(string); !ok {
		t.Fatalf("expected ingest_event result with id, got: %s", out)
	}
}

func TestCLI_Status(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	if out, err := runAMM(t, dbPath, "init"); err != nil {
		t.Fatalf("init: %s: %v", out, err)
	}

	out, err := runAMM(t, dbPath, "status", "--json")
	if err != nil {
		t.Fatalf("status: %s: %v", out, err)
	}
	result := envelopeResult(t, out, "status")
	initialized, _ := result["initialized"].(bool)
	if !initialized {
		t.Fatalf("expected initialized=true in status output, got: %s", out)
	}
}

func TestCLI_RunJob(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	if out, err := runAMM(t, dbPath, "init"); err != nil {
		t.Fatalf("init: %s: %v", out, err)
	}

	out, err := runAMM(t, dbPath, "jobs", "run", "reflect", "--json")
	if err != nil {
		t.Fatalf("jobs run: %s: %v", out, err)
	}
	result := envelopeResult(t, out, "jobs_run")
	if result["kind"] != "reflect" {
		t.Fatalf("expected reflect job kind, got: %s", out)
	}
}

func TestCLI_ErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()

	badDBPath := t.TempDir()
	out, err := runAMM(t, badDBPath, "recall", "test")
	if err == nil {
		t.Fatal("recall against invalid DB path should fail")
	}
	_ = envelopeError(t, out, "recall")

	dbPath := filepath.Join(tmpDir, "test.db")
	if out, err := runAMM(t, dbPath, "init"); err != nil {
		t.Fatalf("init: %s: %v", out, err)
	}

	out, err = runAMM(t, dbPath, "jobs", "run", "invalid_job_kind")
	if err == nil {
		t.Fatal("invalid job kind should fail")
	}
	_ = envelopeError(t, out, "jobs_run")
}
