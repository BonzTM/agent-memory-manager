package service_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestDetectContradictions_CreatesContradictionMemory(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := testService(t)

	createClaimMemory(t, svc, ctx, "amm", "amm uses sqlite.")
	time.Sleep(5 * time.Millisecond)
	createClaimMemory(t, svc, ctx, "amm", "amm uses postgres.")

	runClaimAndContradictionJobs(t, logger, svc, ctx)

	contradictions := contradictionMemoriesFromRecall(t, svc, ctx)
	if len(contradictions) != 1 {
		t.Fatalf("expected 1 contradiction memory, got %d", len(contradictions))
	}
	if contradictions[0].Type != core.MemoryTypeContradiction {
		t.Fatalf("expected contradiction memory type, got %q", contradictions[0].Type)
	}
}

func TestDetectContradictions_SupersedesOlderMemory(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := testService(t)

	older := createClaimMemory(t, svc, ctx, "amm", "amm uses sqlite.")
	time.Sleep(1100 * time.Millisecond)
	newer := createClaimMemory(t, svc, ctx, "amm", "amm uses postgres.")

	runClaimAndContradictionJobs(t, logger, svc, ctx)

	storedOlder, err := svc.GetMemory(ctx, older.ID)
	if err != nil {
		t.Fatalf("GetMemory older: %v", err)
	}
	storedNewer, err := svc.GetMemory(ctx, newer.ID)
	if err != nil {
		t.Fatalf("GetMemory newer: %v", err)
	}

	if storedOlder.Status != core.MemoryStatusSuperseded {
		t.Fatalf("expected older memory status %q, got %q", core.MemoryStatusSuperseded, storedOlder.Status)
	}
	if storedOlder.SupersededBy != storedNewer.ID {
		t.Fatalf("expected older superseded_by=%q, got %q", storedNewer.ID, storedOlder.SupersededBy)
	}
}

func TestDetectContradictions_DeduplicatesByBody(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := testService(t)

	createClaimMemory(t, svc, ctx, "amm", "amm uses sqlite.")
	time.Sleep(5 * time.Millisecond)
	createClaimMemory(t, svc, ctx, "amm", "amm uses postgres.")

	if _, err := svc.RunJob(ctx, "extract_claims"); err != nil {
		t.Fatalf("RunJob extract_claims: %v", err)
	}
	firstDetect, err := svc.RunJob(ctx, "detect_contradictions")
	if err != nil {
		t.Fatalf("RunJob detect_contradictions first: %v", err)
	}
	logger.Info("first detect_contradictions completed", "status", firstDetect.Status, "result", firstDetect.Result)

	secondDetect, err := svc.RunJob(ctx, "detect_contradictions")
	if err != nil {
		t.Fatalf("RunJob detect_contradictions second: %v", err)
	}
	logger.Info("second detect_contradictions completed", "status", secondDetect.Status, "result", secondDetect.Result)

	if got := secondDetect.Result["contradictions_found"]; got != "0" {
		t.Fatalf("expected second detect to find 0 contradictions, got %q", got)
	}

	contradictions := contradictionMemoriesFromRecall(t, svc, ctx)
	if len(contradictions) != 1 {
		t.Fatalf("expected exactly 1 contradiction memory after duplicate run, got %d", len(contradictions))
	}
}

func TestDetectContradictions_IgnoresSameValueClaims(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := testService(t)

	createClaimMemory(t, svc, ctx, "amm", "amm uses sqlite.")
	time.Sleep(5 * time.Millisecond)
	createClaimMemory(t, svc, ctx, "amm", "amm uses sqlite.")

	runClaimAndContradictionJobs(t, logger, svc, ctx)

	contradictions := contradictionMemoriesFromRecall(t, svc, ctx)
	if len(contradictions) != 0 {
		t.Fatalf("expected no contradiction memories for same-value claims, got %d", len(contradictions))
	}
}

func createClaimMemory(t *testing.T, svc core.Service, ctx context.Context, subject, body string) *core.Memory {
	t.Helper()
	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          subject,
		Body:             body,
		TightDescription: body,
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	return mem
}

func runClaimAndContradictionJobs(t *testing.T, logger *slog.Logger, svc core.Service, ctx context.Context) {
	t.Helper()

	claimsJob, err := svc.RunJob(ctx, "extract_claims")
	if err != nil {
		t.Fatalf("RunJob extract_claims: %v", err)
	}
	logger.Info("extract_claims completed", "status", claimsJob.Status, "result", claimsJob.Result)

	contradictionsJob, err := svc.RunJob(ctx, "detect_contradictions")
	if err != nil {
		t.Fatalf("RunJob detect_contradictions: %v", err)
	}
	logger.Info("detect_contradictions completed", "status", contradictionsJob.Status, "result", contradictionsJob.Result)
}

func contradictionMemoriesFromRecall(t *testing.T, svc core.Service, ctx context.Context) []*core.Memory {
	t.Helper()

	result, err := svc.Recall(ctx, "Conflicting claims about", core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 50})
	if err != nil {
		t.Fatalf("Recall contradictions: %v", err)
	}

	var contradictions []*core.Memory
	for _, item := range result.Items {
		if item.Kind != "memory" || item.Type != string(core.MemoryTypeContradiction) {
			continue
		}
		mem, err := svc.GetMemory(ctx, item.ID)
		if err != nil {
			t.Fatalf("GetMemory %s: %v", item.ID, err)
		}
		contradictions = append(contradictions, mem)
	}

	return contradictions
}
