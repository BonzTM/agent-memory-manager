package service

import (
	"context"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestRecall_ContradictionsMode(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "ordinary fact",
		TightDescription: "ordinary fact",
		Importance:       0.5,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeContradiction,
		Body:             "Contradiction one",
		TightDescription: "Contradiction one",
		Importance:       0.8,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeContradiction,
		Body:             "Contradiction two",
		TightDescription: "Contradiction two",
		Importance:       0.8,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Recall(ctx, "Contradiction", core.RecallOptions{Mode: "contradictions", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected contradiction-mode recall to return contradiction memories")
	}
	for _, item := range result.Items {
		if item.Kind != "memory" {
			t.Fatalf("expected memory items only, got kind=%q", item.Kind)
		}
		if item.Type != string(core.MemoryTypeContradiction) {
			t.Fatalf("expected contradiction items only, got type=%q", item.Type)
		}
	}
}
