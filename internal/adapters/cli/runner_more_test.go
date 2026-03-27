package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeEnvelope(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode envelope: %v\nraw=%s", err, raw)
	}
	return env
}

func assertEnvelope(t *testing.T, raw string, wantOK bool, wantCommand string) map[string]interface{} {
	t.Helper()
	env := decodeEnvelope(t, raw)
	ok, okType := env["ok"].(bool)
	if !okType || ok != wantOK {
		t.Fatalf("envelope ok = %#v, want %v; env=%v", env["ok"], wantOK, env)
	}
	if cmd, _ := env["command"].(string); cmd != wantCommand {
		t.Fatalf("envelope command = %q, want %q", cmd, wantCommand)
	}
	return env
}

func rememberForTest(t *testing.T, body string) string {
	t.Helper()
	stdout, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", body, "--tight", "tight " + body})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	env := assertEnvelope(t, stdout, true, "remember")
	result, _ := env["result"].(map[string]interface{})
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatalf("missing memory id in result: %v", result)
	}
	return id
}

func TestRunInit(t *testing.T) {
	setTempDBPath(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	stdout, stderr, err := captureRun(t, []string{"init", "--db", dbPath})
	if err != nil {
		t.Fatalf("init error: %v stderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %s", stderr)
	}
	env := assertEnvelope(t, stdout, true, "init")
	result, _ := env["result"].(map[string]interface{})
	if result["status"] != "initialized" {
		t.Fatalf("expected initialized status, got %v", result["status"])
	}
}

func TestRunRememberRecallDescribeExpandGetMemoryExplainRecall(t *testing.T) {
	setTempDBPath(t)

	memoryID := rememberForTest(t, "alpha memory body")

	stdout, stderr, err := captureRun(t, []string{"recall", "alpha"})
	if err != nil {
		t.Fatalf("recall error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "recall")

	stdout, stderr, err = captureRun(t, []string{"describe", memoryID})
	if err != nil {
		t.Fatalf("describe error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "describe")

	stdout, stderr, err = captureRun(t, []string{"expand", memoryID})
	if err != nil {
		t.Fatalf("expand error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "expand")

	stdout, stderr, err = captureRun(t, []string{"memory", "show", memoryID})
	if err != nil {
		t.Fatalf("memory show error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "memory")

	stdout, stderr, err = captureRun(t, []string{"memory", memoryID})
	if err != nil {
		t.Fatalf("memory <id> error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "memory")

	stdout, stderr, err = captureRun(t, []string{"explain-recall", "--query", "alpha", "--item-id", memoryID})
	if err != nil {
		t.Fatalf("explain-recall error: %v stderr=%s", err, stderr)
	}
	env := assertEnvelope(t, stdout, true, "explain_recall")
	result, _ := env["result"].(map[string]interface{})
	if result["item_id"] != memoryID {
		t.Fatalf("expected explain item_id %q, got %v", memoryID, result["item_id"])
	}
}

func TestRunIngestEventTranscriptHistoryJobStatusRepair(t *testing.T) {
	setTempDBPath(t)

	eventPath := filepath.Join(t.TempDir(), "event.json")
	eventJSON := `{"kind":"message_user","source_system":"test","content":"history alpha","occurred_at":"2026-03-24T12:00:00Z"}`
	if err := os.WriteFile(eventPath, []byte(eventJSON), 0o600); err != nil {
		t.Fatalf("write event file: %v", err)
	}

	stdout, stderr, err := captureRun(t, []string{"ingest", "event", "--in", eventPath})
	if err != nil {
		t.Fatalf("ingest event error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "ingest_event")

	wrappedPath := filepath.Join(t.TempDir(), "wrapped.json")
	wrapped := `{"events":[{"kind":"message_assistant","source_system":"test","content":"wrapped one"}]}`
	if err := os.WriteFile(wrappedPath, []byte(wrapped), 0o600); err != nil {
		t.Fatalf("write wrapped transcript file: %v", err)
	}

	stdout, stderr, err = captureRun(t, []string{"ingest", "transcript", "--in", wrappedPath})
	if err != nil {
		t.Fatalf("ingest transcript wrapped error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "ingest_transcript")

	arrayPath := filepath.Join(t.TempDir(), "array.json")
	arrayJSON := `[{"kind":"message_user","source_system":"test","content":"array one"},{"kind":"message_assistant","source_system":"test","content":"array two"}]`
	if err := os.WriteFile(arrayPath, []byte(arrayJSON), 0o600); err != nil {
		t.Fatalf("write array transcript file: %v", err)
	}

	stdout, stderr, err = captureRun(t, []string{"ingest", "transcript", "--in", arrayPath})
	if err != nil {
		t.Fatalf("ingest transcript array error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "ingest_transcript")

	jsonlPath := filepath.Join(t.TempDir(), "jsonl.json")
	jsonl := strings.Join([]string{
		`{"kind":"message_user","source_system":"test","content":"jsonl one"}`,
		`{"kind":"message_assistant","source_system":"test","content":"jsonl two"}`,
	}, "\n")
	if err := os.WriteFile(jsonlPath, []byte(jsonl), 0o600); err != nil {
		t.Fatalf("write jsonl transcript file: %v", err)
	}

	stdout, stderr, err = captureRun(t, []string{"ingest", "transcript", "--in", jsonlPath})
	if err != nil {
		t.Fatalf("ingest transcript jsonl error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "ingest_transcript")

	stdout, stderr, err = captureRun(t, []string{"history", "history", "alpha"})
	if err != nil {
		t.Fatalf("history error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "history")

	stdout, stderr, err = captureRun(t, []string{"jobs", "run", "reflect"})
	if err != nil {
		t.Fatalf("jobs run reflect error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "jobs_run")

	stdout, stderr, err = captureRun(t, []string{"status"})
	if err != nil {
		t.Fatalf("status error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "status")

	stdout, stderr, err = captureRun(t, []string{"repair", "--check"})
	if err != nil {
		t.Fatalf("repair --check error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "repair")
}

func TestRunErrorPaths(t *testing.T) {
	setTempDBPath(t)

	t.Run("unknown command", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"nope-command"})
		if err == nil {
			t.Fatal("expected unknown command error")
		}
		if stderr != "" {
			t.Fatalf("unknown command should not emit envelope, got stderr=%s", stderr)
		}
	})

	t.Run("remember validation failure", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--tight", "missing body"})
		if err == nil {
			t.Fatal("expected remember validation error")
		}
		assertEnvelope(t, stderr, false, "remember")
	})

	t.Run("expand missing id", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"expand"})
		if err == nil {
			t.Fatal("expected expand missing id error")
		}
		assertEnvelope(t, stderr, false, "expand")
	})

	t.Run("memory show missing id", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"memory", "show"})
		if err == nil {
			t.Fatal("expected memory show missing id error")
		}
		assertEnvelope(t, stderr, false, "memory")
	})

	t.Run("jobs run missing kind", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"jobs", "run"})
		if err == nil {
			t.Fatal("expected jobs run missing kind error")
		}
		assertEnvelope(t, stderr, false, "jobs_run")
	})

	t.Run("jobs run reprocess flag", func(t *testing.T) {
		stdout, stderr, err := captureRun(t, []string{"jobs", "run", "--reprocess"})
		if err != nil {
			t.Fatalf("jobs run --reprocess error: %v stderr=%s", err, stderr)
		}
		assertEnvelope(t, stdout, true, "jobs_run")
	})

	t.Run("jobs run reprocess-all flag", func(t *testing.T) {
		stdout, stderr, err := captureRun(t, []string{"jobs", "run", "--reprocess-all"})
		if err != nil {
			t.Fatalf("jobs run --reprocess-all error: %v stderr=%s", err, stderr)
		}
		assertEnvelope(t, stdout, true, "jobs_run")
	})

	t.Run("jobs run conflicting reprocess flags", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"jobs", "run", "--reprocess", "--reprocess-all"})
		if err == nil {
			t.Fatal("expected jobs run conflicting reprocess flags error")
		}
		assertEnvelope(t, stderr, false, "jobs_run")
	})

	t.Run("jobs run positional plus reprocess flag", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"jobs", "run", "reflect", "--reprocess"})
		if err == nil {
			t.Fatal("expected jobs run positional plus reprocess error")
		}
		assertEnvelope(t, stderr, false, "jobs_run")
	})

	t.Run("ingest event validation failure", func(t *testing.T) {
		badPath := filepath.Join(t.TempDir(), "bad-event.json")
		badJSON := `{"kind":"message_user","source_system":"test"}`
		if err := os.WriteFile(badPath, []byte(badJSON), 0o600); err != nil {
			t.Fatalf("write bad event file: %v", err)
		}

		_, stderr, err := captureRun(t, []string{"ingest", "event", "--in", badPath})
		if err == nil {
			t.Fatal("expected ingest event validation error")
		}
		assertEnvelope(t, stderr, false, "ingest_event")
	})

	t.Run("ingest event invalid occurred_at", func(t *testing.T) {
		badPath := filepath.Join(t.TempDir(), "bad-occurred-at.json")
		badJSON := `{"kind":"message_user","source_system":"test","content":"x","occurred_at":"not-a-time"}`
		if err := os.WriteFile(badPath, []byte(badJSON), 0o600); err != nil {
			t.Fatalf("write bad occurred_at event file: %v", err)
		}

		_, stderr, err := captureRun(t, []string{"ingest", "event", "--in", badPath})
		if err == nil {
			t.Fatal("expected ingest event occurred_at validation error")
		}
		assertEnvelope(t, stderr, false, "ingest_event")
	})

	t.Run("ingest transcript parse failure", func(t *testing.T) {
		badPath := filepath.Join(t.TempDir(), "bad-transcript.json")
		if err := os.WriteFile(badPath, []byte("not json"), 0o600); err != nil {
			t.Fatalf("write bad transcript file: %v", err)
		}

		_, stderr, err := captureRun(t, []string{"ingest", "transcript", "--in", badPath})
		if err == nil {
			t.Fatal("expected ingest transcript parse error")
		}
		assertEnvelope(t, stderr, false, "ingest_transcript")
	})

	t.Run("policy missing subcommand", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"policy"})
		if err == nil {
			t.Fatal("expected policy subcommand error")
		}
		if stderr != "" {
			t.Fatalf("policy command error should not emit envelope, got %s", stderr)
		}
	})

	t.Run("policy remove missing id", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"policy", "remove"})
		if err == nil {
			t.Fatal("expected policy remove missing id error")
		}
		assertEnvelope(t, stderr, false, "policy_remove")
	})

	t.Run("jobs missing subcommand", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"jobs"})
		if err == nil {
			t.Fatal("expected jobs subcommand error")
		}
		if stderr != "" {
			t.Fatalf("jobs command error should not emit envelope, got %s", stderr)
		}
	})
}

func TestRunAdditionalBranches(t *testing.T) {
	setTempDBPath(t)

	t.Run("recall explicit mode", func(t *testing.T) {
		stdout, stderr, err := captureRun(t, []string{"recall", "--mode", "facts", "alpha"})
		if err != nil {
			t.Fatalf("recall explicit mode error: %v stderr=%s", err, stderr)
		}
		assertEnvelope(t, stdout, true, "recall")
	})

	t.Run("expand explicit kind", func(t *testing.T) {
		id := rememberForTest(t, "expand explicit")
		stdout, stderr, err := captureRun(t, []string{"expand", id, "--kind", "memory"})
		if err != nil {
			t.Fatalf("expand explicit kind error: %v stderr=%s", err, stderr)
		}
		assertEnvelope(t, stdout, true, "expand")
	})

	t.Run("memory get error branch", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"memory", "show", "mem_missing"})
		if err == nil {
			t.Fatal("expected memory get error")
		}
		assertEnvelope(t, stderr, false, "memory")
	})

	t.Run("jobs invalid kind branch", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"jobs", "run", "not_a_job"})
		if err == nil {
			t.Fatal("expected invalid job kind error")
		}
		assertEnvelope(t, stderr, false, "jobs_run")
	})

	t.Run("repair invalid fix branch", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"repair", "--fix", "unknown_fix"})
		if err == nil {
			t.Fatal("expected repair invalid fix error")
		}
		assertEnvelope(t, stderr, false, "repair")
	})

	t.Run("ingest event parse branch", func(t *testing.T) {
		badPath := filepath.Join(t.TempDir(), "bad-event-parse.json")
		if err := os.WriteFile(badPath, []byte("not-json"), 0o600); err != nil {
			t.Fatalf("write bad parse event file: %v", err)
		}

		_, stderr, err := captureRun(t, []string{"ingest", "event", "--in", badPath})
		if err == nil {
			t.Fatal("expected ingest event parse error")
		}
		assertEnvelope(t, stderr, false, "ingest_event")
	})

	t.Run("service error branches", func(t *testing.T) {
		badDB := t.TempDir()
		if err := os.Setenv("AMM_DB_PATH", badDB); err != nil {
			t.Fatalf("set bad db path: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("AMM_DB_PATH") })

		commands := [][]string{
			{"init"},
			{"remember", "--type", "fact", "--scope", "global", "--body", "x", "--tight", "y"},
			{"status"},
			{"recall", "q"},
			{"describe", "mem_any"},
			{"history", "q"},
			{"jobs", "run", "reflect"},
			{"repair", "--check"},
			{"explain-recall", "--query", "q", "--item-id", "mem_any"},
			{"ingest", "event", "--in", "-"},
			{"ingest", "transcript", "--in", "-"},
		}

		for _, cmd := range commands {
			_, stderr, err := captureRun(t, cmd)
			if err == nil {
				t.Fatalf("expected service error for command %v", cmd)
			}
			if stderr == "" {
				t.Fatalf("expected stderr envelope for command %v", cmd)
			}
			env := decodeEnvelope(t, stderr)
			ok, _ := env["ok"].(bool)
			if ok {
				t.Fatalf("expected ok=false for command %v; env=%v", cmd, env)
			}
		}
	})

	t.Run("expand inferred kind branches", func(t *testing.T) {
		for _, id := range []string{"sum_missing", "ep_missing", "x_missing"} {
			_, stderr, err := captureRun(t, []string{"expand", id})
			if err == nil {
				t.Fatalf("expected expand error for %s", id)
			}
			assertEnvelope(t, stderr, false, "expand")
		}
	})

	t.Run("memory update validation and get error branches", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"memory", "update"})
		if err == nil {
			t.Fatal("expected memory update missing id error")
		}
		assertEnvelope(t, stderr, false, "memory_update")

		_, stderr, err = captureRun(t, []string{"memory", "update", "mem_missing", "--body", "x"})
		if err == nil {
			t.Fatal("expected memory update get error")
		}
		assertEnvelope(t, stderr, false, "memory_update")

		_, stderr, err = captureRun(t, []string{"memory", "update", "mem_missing", "--status", "bad_status"})
		if err == nil {
			t.Fatal("expected memory update validation error")
		}
		assertEnvelope(t, stderr, false, "memory_update")

		mid := rememberForTest(t, "memory update invalid type")
		_, stderr, err = captureRun(t, []string{"memory", "update", mid, "--type", "not_a_valid_type"})
		if err == nil {
			t.Fatal("expected memory update invalid type validation error")
		}
		assertEnvelope(t, stderr, false, "memory_update")
	})

	t.Run("policy add validation branch", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"policy", "add", "--pattern-type", "source", "--pattern", "x"})
		if err == nil {
			t.Fatal("expected policy add validation error")
		}
		assertEnvelope(t, stderr, false, "policy_add")

		_, stderr, err = captureRun(t, []string{"policy", "remove", "pol_missing"})
		if err == nil {
			t.Fatal("expected policy remove missing id to fail")
		}
		assertEnvelope(t, stderr, false, "policy_remove")
	})

	t.Run("reset-derived requires confirm", func(t *testing.T) {
		_, stderr, err := captureRun(t, []string{"reset-derived"})
		if err == nil {
			t.Fatal("expected reset-derived validation error without --confirm")
		}
		assertEnvelope(t, stderr, false, "reset_derived")
	})
}

func TestRunResetDerived(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "to reset", "--tight", "to reset"})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "remember")

	stdout, stderr, err = captureRun(t, []string{"reset-derived", "--confirm"})
	if err != nil {
		t.Fatalf("reset-derived error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "reset_derived")
}

func TestPrintUsageAndRunHelp(t *testing.T) {
	setTempDBPath(t)

	func() {
		orig := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stdout = w
		defer func() {
			_ = w.Close()
			os.Stdout = orig
			_ = r.Close()
		}()

		printUsage()
	}()

	stdout, stderr, err := captureRun(t, nil)
	if err != nil {
		t.Fatalf("run with no args error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Usage: amm") {
		t.Fatalf("expected usage output for empty args, got %q", stdout)
	}

	stdout, stderr, err = captureRun(t, []string{"help"})
	if err != nil {
		t.Fatalf("run help error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Commands:") {
		t.Fatalf("expected commands output for help, got %q", stdout)
	}
}
