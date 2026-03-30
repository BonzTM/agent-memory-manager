package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

func seedCLIShowFixtures(t *testing.T) {
	t.Helper()

	dbPath := os.Getenv("AMM_DB_PATH")
	if dbPath == "" {
		t.Fatal("AMM_DB_PATH must be set")
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open sqlite for fixtures: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate sqlite for fixtures: %v", err)
	}
	repo := &sqlite.SQLiteRepository{DB: db}

	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.InsertSummary(ctx, &core.Summary{
		ID:               "sum_cli_show",
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Body:             "summary body",
		TightDescription: "summary tight",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("insert summary fixture: %v", err)
	}

	if err := repo.InsertEpisode(ctx, &core.Episode{
		ID:               "epi_cli_show",
		Title:            "episode title",
		Summary:          "episode body",
		TightDescription: "episode tight",
		Scope:            core.ScopeGlobal,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("insert episode fixture: %v", err)
	}

	if err := repo.InsertEntity(ctx, &core.Entity{
		ID:            "ent_cli_show",
		Type:          "service",
		CanonicalName: "CLI Fixture Entity",
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("insert entity fixture: %v", err)
	}
}

func TestRunnerCoverage_CommandMatrix(t *testing.T) {
	setTempDBPath(t)
	seedCLIShowFixtures(t)

	stdout, stderr, err := captureRun(t, []string{"init"})
	if err != nil {
		t.Fatalf("init error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "init")

	stdout, stderr, err = captureRun(t, []string{"status"})
	if err != nil {
		t.Fatalf("status error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "status")

	stdout, stderr, err = captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "runner coverage body", "--tight", "runner coverage"})
	if err != nil {
		t.Fatalf("remember error: %v stderr=%s", err, stderr)
	}
	remembered := assertEnvelope(t, stdout, true, "remember")
	memoryID, _ := remembered["result"].(map[string]interface{})["id"].(string)
	if memoryID == "" {
		t.Fatalf("missing memory id from remember result: %+v", remembered)
	}

	_, stderr, err = captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--tight", "missing body"})
	if err == nil {
		t.Fatal("expected remember validation error")
	}
	assertEnvelope(t, stderr, false, "remember")

	for _, mode := range []string{"ambient", "facts", "hybrid"} {
		t.Run("recall mode "+mode, func(t *testing.T) {
			stdout, stderr, err := captureRun(t, []string{"recall", "--mode", mode, "runner"})
			if err != nil {
				t.Fatalf("recall %s error: %v stderr=%s", mode, err, stderr)
			}
			assertEnvelope(t, stdout, true, "recall")
		})
	}

	stdout, stderr, err = captureRun(t, []string{"memory", "show", memoryID})
	if err != nil {
		t.Fatalf("memory show error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "memory")

	for _, command := range [][]string{{"summary", "show", "sum_cli_show"}, {"episode", "show", "epi_cli_show"}, {"entity", "show", "ent_cli_show"}} {
		t.Run(command[0]+" show", func(t *testing.T) {
			stdout, stderr, err := captureRun(t, command)
			if err != nil {
				t.Fatalf("%v error: %v stderr=%s", command, err, stderr)
			}
			assertEnvelope(t, stdout, true, command[0])
		})
	}

	stdout, stderr, err = captureRun(t, []string{"forget", memoryID})
	if err != nil {
		t.Fatalf("forget error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "forget")

	stdout, stderr, err = captureRun(t, []string{"policy", "add", "--pattern-type", "source", "--pattern", "cli-cov-*", "--mode", "full"})
	if err != nil {
		t.Fatalf("policy add error: %v stderr=%s", err, stderr)
	}
	policyAdded := assertEnvelope(t, stdout, true, "policy_add")
	policyID, _ := policyAdded["result"].(map[string]interface{})["id"].(string)
	if policyID == "" {
		t.Fatalf("missing policy id from add result: %+v", policyAdded)
	}

	stdout, stderr, err = captureRun(t, []string{"policy", "list"})
	if err != nil {
		t.Fatalf("policy list error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "policy_list")

	stdout, stderr, err = captureRun(t, []string{"policy", "remove", policyID})
	if err != nil {
		t.Fatalf("policy remove error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "policy_remove")

	stdout, stderr, err = captureRun(t, []string{"project", "add", "--name", "cli-coverage", "--path", "/tmp/cli-coverage", "--description", "coverage project"})
	if err != nil {
		t.Fatalf("project add error: %v stderr=%s", err, stderr)
	}
	projectAdded := assertEnvelope(t, stdout, true, "project_add")
	projectID, _ := projectAdded["result"].(map[string]interface{})["id"].(string)
	if projectID == "" {
		t.Fatalf("missing project id from add result: %+v", projectAdded)
	}

	stdout, stderr, err = captureRun(t, []string{"project", "show", projectID})
	if err != nil {
		t.Fatalf("project show error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "project")

	stdout, stderr, err = captureRun(t, []string{"project", "list"})
	if err != nil {
		t.Fatalf("project list error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "project_list")

	stdout, stderr, err = captureRun(t, []string{"project", "remove", projectID})
	if err != nil {
		t.Fatalf("project remove error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "project_remove")

	stdout, stderr, err = captureRun(t, []string{"relationship", "add", "--from", "ent_cli_show", "--to", "ent_cli_show", "--type", "self"})
	if err != nil {
		t.Fatalf("relationship add error: %v stderr=%s", err, stderr)
	}
	relationshipAdded := assertEnvelope(t, stdout, true, "relationship_add")
	relationshipID, _ := relationshipAdded["result"].(map[string]interface{})["id"].(string)
	if relationshipID == "" {
		t.Fatalf("missing relationship id from add result: %+v", relationshipAdded)
	}

	stdout, stderr, err = captureRun(t, []string{"relationship", "show", relationshipID})
	if err != nil {
		t.Fatalf("relationship show error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "relationship")

	stdout, stderr, err = captureRun(t, []string{"relationship", "list", "--entity-id", "ent_cli_show", "--limit", "10"})
	if err != nil {
		t.Fatalf("relationship list error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "relationship_list")

	stdout, stderr, err = captureRun(t, []string{"relationship", "remove", relationshipID})
	if err != nil {
		t.Fatalf("relationship remove error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "relationship_remove")

	stdout, stderr, err = captureRun(t, []string{"remember", "--type", "fact", "--scope", "global", "--body", "partial update body", "--tight", "partial update"})
	if err != nil {
		t.Fatalf("remember for update error: %v stderr=%s", err, stderr)
	}
	partialID, _ := decodeEnvelopeResult(t, stdout)["id"].(string)
	if partialID == "" {
		t.Fatalf("missing memory id for partial update: %s", stdout)
	}

	stdout, stderr, err = captureRun(t, []string{"memory", "update", partialID, "--status", "archived"})
	if err != nil {
		t.Fatalf("memory update partial error: %v stderr=%s", err, stderr)
	}
	assertEnvelope(t, stdout, true, "memory_update")

	_, stderr, err = captureRunWithStdin(t, []string{"run"}, `{`)
	if err == nil {
		t.Fatal("expected run parse failure")
	}
	assertEnvelope(t, stderr, false, "run")

	_, stderr, err = captureRun(t, []string{"totally-unknown-command"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr envelope for unknown command: %s", stderr)
	}

	for _, test := range []struct {
		name    string
		args    []string
		command string
	}{
		{name: "summary missing id", args: []string{"summary"}, command: "summary"},
		{name: "episode missing id", args: []string{"episode"}, command: "episode"},
		{name: "entity missing id", args: []string{"entity"}, command: "entity"},
		{name: "policy remove missing id", args: []string{"policy", "remove"}, command: "policy_remove"},
		{name: "project remove missing id", args: []string{"project", "remove"}, command: "project_remove"},
		{name: "relationship remove missing id", args: []string{"relationship", "remove"}, command: "relationship_remove"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, err := captureRun(t, test.args)
			if err == nil {
				t.Fatalf("expected validation error for %v", test.args)
			}
			assertEnvelope(t, stderr, false, test.command)
		})
	}
}
