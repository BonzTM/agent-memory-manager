package service

import (
	"context"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestExtractMemoryIDsFromContradiction(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "standard contradiction body",
			body: `Conflicting claims about "auth": memory mem_abc123 says "use JWT", memory mem_def456 says "use sessions"`,
			want: []string{"mem_abc123", "mem_def456"},
		},
		{
			name: "no memory IDs",
			body: "This is a regular memory body",
			want: nil,
		},
		{
			name: "single memory ID",
			body: `memory mem_abc123 says "something"`,
			want: []string{"mem_abc123"},
		},
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "nested mem_ ID in quoted body is ignored",
			body: `Conflicting claims about "notes": memory mem_aaa says "I saw memory mem_nested says something", memory mem_bbb says "other"`,
			want: []string{"mem_aaa", "mem_bbb"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMemoryIDsFromContradiction(tt.body)
			if len(got) != len(tt.want) {
				t.Errorf("extractMemoryIDsFromContradiction() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractMemoryIDsFromContradiction()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRecall_ContradictionSurfacing(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Create two memories that will be referenced in a contradiction.
	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "auth",
		Body:             "We use JWT for authentication",
		TightDescription: "JWT authentication",
		Importance:       0.7,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "auth",
		Body:             "We use session cookies for authentication",
		TightDescription: "session cookie authentication",
		Importance:       0.7,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a contradiction that references both memories.
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeContradiction,
		Subject:          "auth",
		Body:             contradictionBody("auth", core.Memory{ID: memA.ID, Body: "We use JWT for authentication"}, core.Memory{ID: memB.ID, Body: "We use session cookies for authentication"}, ""),
		TightDescription: "Contradiction: auth",
		Importance:       0.8,
		Confidence:       0.7,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Recall in hybrid mode — both facts should surface with ConflictsWith.
	result, err := svc.Recall(ctx, "authentication", core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}

	foundConflict := false
	for _, item := range result.Items {
		if item.ID == memA.ID || item.ID == memB.ID {
			if len(item.ConflictsWith) > 0 {
				foundConflict = true
			}
		}
	}
	if !foundConflict {
		t.Error("expected at least one recalled memory to have ConflictsWith populated")
	}
}

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
