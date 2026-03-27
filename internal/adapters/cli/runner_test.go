package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func captureRun(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW

	runErr := Run(args)

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)
	_ = outR.Close()
	_ = errR.Close()

	return string(outBytes), string(errBytes), runErr
}

func decodeEnvelopeResult(t *testing.T, stdout string) map[string]interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout=%s", err, stdout)
	}
	result, ok := env["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing map result in envelope: %#v", env["result"])
	}
	return result
}

func setTempDBPath(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "amm.db")
	if err := os.Setenv("AMM_DB_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("AMM_DB_PATH")
	})
}

func TestPositionalArgs_BoolFlagDoesNotEatNextArg(t *testing.T) {
	args := []string{"--mode", "ambient", "--limit", "5", "--json", "preference"}
	pos := positionalArgs(args)
	if len(pos) != 1 || pos[0] != "preference" {
		t.Errorf("positionalArgs(%v) = %v, want [preference]", args, pos)
	}
}

func TestPositionalArgs_QueryAfterBoolFlag(t *testing.T) {
	args := []string{"--json", "hello", "world"}
	pos := positionalArgs(args)
	if len(pos) != 2 || pos[0] != "hello" || pos[1] != "world" {
		t.Errorf("positionalArgs(%v) = %v, want [hello world]", args, pos)
	}
}

func TestPositionalArgs_ConsistentWithParseFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantPos []string
	}{
		{"flag then query", []string{"--mode", "facts", "myquery"}, []string{"myquery"}},
		{"bool flag then query", []string{"--json", "myquery"}, []string{"myquery"}},
		{"mixed flags and query", []string{"--mode", "ambient", "--limit", "5", "--json", "my", "query"}, []string{"my", "query"}},
		{"no flags", []string{"hello", "world"}, []string{"hello", "world"}},
		{"all flags", []string{"--mode", "facts", "--limit", "10"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := parseFlags(tt.args)
			pos := positionalArgs(tt.args)

			for _, p := range pos {
				if _, consumed := flags[p]; consumed {
					t.Errorf("positional %q was also consumed as a flag value", p)
				}
			}

			if len(pos) != len(tt.wantPos) {
				t.Errorf("positionalArgs(%v) = %v, want %v", tt.args, pos, tt.wantPos)
				return
			}
			for i, w := range tt.wantPos {
				if pos[i] != w {
					t.Errorf("positionalArgs(%v)[%d] = %q, want %q", tt.args, i, pos[i], w)
				}
			}
		})
	}
}

func TestPolicyCommands(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRun(t, []string{"policy", "add", "--pattern-type", "source", "--pattern", "svc-*", "--mode", "full"})
	if err != nil {
		t.Fatalf("policy add 1 error: %v stderr=%s", err, stderr)
	}
	first := decodeEnvelopeResult(t, stdout)
	firstID, _ := first["id"].(string)
	if firstID == "" {
		t.Fatalf("expected first policy id, got result=%v", first)
	}

	stdout, stderr, err = captureRun(t, []string{"policy", "add", "--pattern-type", "session", "--pattern", "sess-*", "--mode", "read_only"})
	if err != nil {
		t.Fatalf("policy add 2 error: %v stderr=%s", err, stderr)
	}

	stdout, stderr, err = captureRun(t, []string{"policy", "list"})
	if err != nil {
		t.Fatalf("policy list error: %v stderr=%s", err, stderr)
	}
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	list, ok := env["result"].([]interface{})
	if !ok {
		t.Fatalf("expected list result array, got %#v", env["result"])
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(list))
	}

	_, stderr, err = captureRun(t, []string{"policy", "remove", firstID})
	if err != nil {
		t.Fatalf("policy remove error: %v stderr=%s", err, stderr)
	}

	stdout, stderr, err = captureRun(t, []string{"policy", "list"})
	if err != nil {
		t.Fatalf("policy list after remove error: %v stderr=%s", err, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode list envelope after remove: %v", err)
	}
	list, ok = env["result"].([]interface{})
	if !ok {
		t.Fatalf("expected list result array after remove, got %#v", env["result"])
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 policy after remove, got %d", len(list))
	}
}

func TestMemoryUpdateCommand(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "old body", "--tight", "old tight"})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	created := decodeEnvelopeResult(t, stdout)
	memoryID, _ := created["id"].(string)
	if memoryID == "" {
		t.Fatalf("expected memory ID, got result=%v", created)
	}

	stdout, stderr, err = captureRun(t, []string{"memory", "update", memoryID, "--body", "new body", "--tight", "new tight", "--status", "archived", "--type", "decision", "--scope", "project"})
	if err != nil {
		t.Fatalf("memory update error: %v stderr=%s", err, stderr)
	}
	updated := decodeEnvelopeResult(t, stdout)
	if updated["body"] != "new body" {
		t.Fatalf("expected updated body, got %v", updated["body"])
	}
	if updated["tight_description"] != "new tight" {
		t.Fatalf("expected updated tight_description, got %v", updated["tight_description"])
	}
	if updated["status"] != "archived" {
		t.Fatalf("expected updated status archived, got %v", updated["status"])
	}
	if updated["type"] != "decision" {
		t.Fatalf("expected updated type decision, got %v", updated["type"])
	}
	if updated["scope"] != "project" {
		t.Fatalf("expected updated scope project, got %v", updated["scope"])
	}
}

func TestShareCommand(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "share me", "--tight", "share me", "--agent-id", "agent-a"})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	created := decodeEnvelopeResult(t, stdout)
	memoryID, _ := created["id"].(string)
	if memoryID == "" {
		t.Fatalf("expected memory ID, got result=%v", created)
	}

	stdout, stderr, err = captureRun(t, []string{"share", memoryID, "--privacy", "shared"})
	if err != nil {
		t.Fatalf("share error: %v stderr=%s", err, stderr)
	}
	updated := decodeEnvelopeResult(t, stdout)
	if updated["privacy_level"] != "shared" {
		t.Fatalf("expected privacy_level shared, got %v", updated["privacy_level"])
	}

	_, stderr, err = captureRun(t, []string{"share", memoryID, "--privacy", "team_only"})
	if err == nil {
		t.Fatal("expected share validation error")
	}
	assertEnvelope(t, stderr, false, "share")
}

func TestForgetCommand(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "forget me", "--tight", "forget me", "--agent-id", "agent-a"})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	created := decodeEnvelopeResult(t, stdout)
	memoryID, _ := created["id"].(string)
	if memoryID == "" {
		t.Fatalf("expected memory ID, got result=%v", created)
	}

	stdout, stderr, err = captureRun(t, []string{"forget", memoryID})
	if err != nil {
		t.Fatalf("forget error: %v stderr=%s", err, stderr)
	}
	updated := decodeEnvelopeResult(t, stdout)
	if updated["status"] != "retracted" {
		t.Fatalf("expected status retracted, got %v", updated["status"])
	}

	_, stderr, err = captureRun(t, []string{"forget"})
	if err == nil {
		t.Fatal("expected forget validation error")
	}
	assertEnvelope(t, stderr, false, "forget")
}
