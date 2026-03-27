package service

import (
	"math"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// ---------------------------------------------------------------------------
// Entity extraction
// ---------------------------------------------------------------------------

func TestExtractEntities(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAny  []string // at least these must be present
		wantNone []string // none of these should appear
	}{
		{
			name:    "single capitalized name",
			input:   "Talk to Alice about the project",
			wantAny: []string{"Alice"},
		},
		{
			name:    "multi-word name",
			input:   "We met John Smith yesterday",
			wantAny: []string{"John Smith", "John", "Smith"},
		},
		{
			name:     "excludes common words",
			input:    "The quick brown fox",
			wantNone: []string{"The"},
		},
		{
			name:     "single character excluded",
			input:    "I went to the store",
			wantNone: []string{"I"},
		},
		{
			name:    "multiple entities",
			input:   "Alice and Bob discussed Redis",
			wantAny: []string{"Alice", "Bob", "Redis"},
		},
		{
			name:  "empty input",
			input: "",
		},
		{
			name:     "no capitalized words",
			input:    "all lowercase words here",
			wantNone: []string{"all", "lowercase"},
		},
		{
			name:    "punctuation stripped",
			input:   "Ask Alice, then Bob.",
			wantAny: []string{"Alice", "Bob"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractEntities(tc.input)
			set := make(map[string]bool, len(got))
			for _, e := range got {
				set[e] = true
			}
			for _, want := range tc.wantAny {
				if !set[want] {
					t.Errorf("expected entity %q in result %v", want, got)
				}
			}
			for _, bad := range tc.wantNone {
				if set[bad] {
					t.Errorf("did not expect entity %q in result %v", bad, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Alias matching
// ---------------------------------------------------------------------------

func TestMatchEntityAliases(t *testing.T) {
	entities := []core.Entity{
		{ID: "ent_1", CanonicalName: "Alice", Aliases: []string{"alice", "A"}},
		{ID: "ent_2", CanonicalName: "Bob", Aliases: []string{"Robert"}},
		{ID: "ent_3", CanonicalName: "Redis", Aliases: nil},
	}

	t.Run("exact canonical match", func(t *testing.T) {
		ids := MatchEntityAliases([]string{"Bob"}, entities)
		if len(ids) != 1 || ids[0] != "ent_2" {
			t.Errorf("expected [ent_2], got %v", ids)
		}
	})

	t.Run("case insensitive alias match", func(t *testing.T) {
		ids := MatchEntityAliases([]string{"robert"}, entities)
		if len(ids) != 1 || ids[0] != "ent_2" {
			t.Errorf("expected [ent_2], got %v", ids)
		}
	})

	t.Run("multiple matches", func(t *testing.T) {
		ids := MatchEntityAliases([]string{"Alice", "Redis"}, entities)
		if len(ids) != 2 {
			t.Errorf("expected 2 matches, got %v", ids)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		ids := MatchEntityAliases([]string{"Unknown"}, entities)
		if len(ids) != 0 {
			t.Errorf("expected 0 matches, got %v", ids)
		}
	})

	t.Run("empty extracted", func(t *testing.T) {
		ids := MatchEntityAliases(nil, entities)
		if ids != nil {
			t.Errorf("expected nil, got %v", ids)
		}
	})

	t.Run("empty entities", func(t *testing.T) {
		ids := MatchEntityAliases([]string{"Alice"}, nil)
		if ids != nil {
			t.Errorf("expected nil, got %v", ids)
		}
	})
}

// ---------------------------------------------------------------------------
// Individual scoring signals
// ---------------------------------------------------------------------------

func TestScoreItem_Lexical(t *testing.T) {
	highPos := signalLexical(0)
	lowPos := signalLexical(10)

	if highPos != 1.0 {
		t.Errorf("expected lexical(0)=1.0, got %f", highPos)
	}
	if lowPos >= highPos {
		t.Errorf("expected lexical(10) < lexical(0), got %f >= %f", lowPos, highPos)
	}
	if lowPos <= 0 {
		t.Errorf("expected lexical(10) > 0, got %f", lowPos)
	}
}

func TestScoreItem_ScopeFit(t *testing.T) {
	sctxProject := ScoringContext{ProjectID: "proj_1"}
	sctxNoProject := ScoringContext{}

	t.Run("project match", func(t *testing.T) {
		item := ScoringCandidate{ProjectID: "proj_1", Scope: core.ScopeProject}
		score := signalScopeFit(item, sctxProject)
		if score != 1.0 {
			t.Errorf("expected 1.0 for project match, got %f", score)
		}
	})

	t.Run("global with project context", func(t *testing.T) {
		item := ScoringCandidate{Scope: core.ScopeGlobal}
		score := signalScopeFit(item, sctxProject)
		if score != 0.5 {
			t.Errorf("expected 0.5 for global in project context, got %f", score)
		}
	})

	t.Run("different project", func(t *testing.T) {
		item := ScoringCandidate{ProjectID: "proj_other", Scope: core.ScopeProject}
		score := signalScopeFit(item, sctxProject)
		if score != 0.3 {
			t.Errorf("expected 0.3 for different project, got %f", score)
		}
	})

	t.Run("no project context global item", func(t *testing.T) {
		item := ScoringCandidate{Scope: core.ScopeGlobal}
		score := signalScopeFit(item, sctxNoProject)
		if score != 1.0 {
			t.Errorf("expected 1.0 for global without project context, got %f", score)
		}
	})

	t.Run("no project context project item", func(t *testing.T) {
		item := ScoringCandidate{ProjectID: "proj_1", Scope: core.ScopeProject}
		score := signalScopeFit(item, sctxNoProject)
		if score != 0.5 {
			t.Errorf("expected 0.5 for project item without project context, got %f", score)
		}
	})
}

func TestScoreItem_Recency(t *testing.T) {
	now := time.Now().UTC()

	recent := ScoringCandidate{CreatedAt: now, UpdatedAt: now}
	old := ScoringCandidate{CreatedAt: now.AddDate(0, 0, -60), UpdatedAt: now.AddDate(0, 0, -60)}

	recentScore := signalRecency(recent, now)
	oldScore := signalRecency(old, now)

	if recentScore <= oldScore {
		t.Errorf("expected recent score (%f) > old score (%f)", recentScore, oldScore)
	}
	if recentScore < 0.9 {
		t.Errorf("expected recent item to score near 1.0, got %f", recentScore)
	}
	if oldScore <= 0 || oldScore >= 1 {
		t.Errorf("expected old score in (0,1), got %f", oldScore)
	}
}

func TestScoreItem_Importance(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{0.7, 0.7},
		{0.0, 0.0},
		{1.0, 1.0},
		{-0.5, 0.0}, // clamped
		{1.5, 1.0},  // clamped
	}
	for _, tc := range tests {
		item := ScoringCandidate{Importance: tc.input}
		got := signalImportance(item)
		if got != tc.want {
			t.Errorf("signalImportance(%f) = %f, want %f", tc.input, got, tc.want)
		}
	}
}

func TestScoreItem_TemporalValidity(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)

	t.Run("expired", func(t *testing.T) {
		item := ScoringCandidate{ValidTo: &past}
		score := signalTemporalValidity(item, now)
		if score != 0.0 {
			t.Errorf("expected 0.0 for expired item, got %f", score)
		}
	})

	t.Run("superseded", func(t *testing.T) {
		item := ScoringCandidate{SupersededBy: "mem_xyz"}
		score := signalTemporalValidity(item, now)
		if score != 0.5 {
			t.Errorf("expected 0.5 for superseded item, got %f", score)
		}
	})

	t.Run("active", func(t *testing.T) {
		item := ScoringCandidate{}
		score := signalTemporalValidity(item, now)
		if score != 1.0 {
			t.Errorf("expected 1.0 for active item, got %f", score)
		}
	})
}

func TestScoreItem_RepetitionPenalty(t *testing.T) {
	sctx := ScoringContext{
		RecentRecalls: map[string]bool{"item_1": true},
	}

	t.Run("recently shown", func(t *testing.T) {
		item := ScoringCandidate{ID: "item_1"}
		score := signalRepetitionPenalty(item, sctx)
		if score != 1.0 {
			t.Errorf("expected 1.0 for recently shown, got %f", score)
		}
	})

	t.Run("not recently shown", func(t *testing.T) {
		item := ScoringCandidate{ID: "item_2"}
		score := signalRepetitionPenalty(item, sctx)
		if score != 0.0 {
			t.Errorf("expected 0.0 for not shown, got %f", score)
		}
	})
}

func TestSourceTrust_ExplicitRememberHighest(t *testing.T) {
	item := ScoringCandidate{SourceSystem: "remember"}
	got := signalSourceTrust(item)
	if got != 1.0 {
		t.Fatalf("expected source trust 1.0 for remember, got %f", got)
	}
}

func TestSourceTrust_HeuristicLowest(t *testing.T) {
	item := ScoringCandidate{SourceSystem: "heuristic"}
	got := signalSourceTrust(item)
	if got != 0.5 {
		t.Fatalf("expected source trust 0.5 for heuristic, got %f", got)
	}
}

func TestSourceTrust_AgentSourceHigh(t *testing.T) {
	item := ScoringCandidate{SourceSystem: "codex"}
	got := signalSourceTrust(item)
	if got != 0.9 {
		t.Fatalf("expected source trust 0.9 for agent source, got %f", got)
	}
}

func TestScoreItem_SourceTrustIntegration(t *testing.T) {
	now := time.Now().UTC()
	weights := ScoringWeights{SourceTrust: 1.0}
	sctx := ScoringContext{Now: now, Weights: &weights}

	remember := ScoringCandidate{
		ID:           "remember",
		Kind:         "memory",
		SourceSystem: "remember",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	heuristic := remember
	heuristic.ID = "heuristic"
	heuristic.SourceSystem = "heuristic"

	bRemember := ScoreItem(remember, sctx)
	bHeuristic := ScoreItem(heuristic, sctx)

	if bRemember.SourceTrust != 1.0 {
		t.Fatalf("expected source_trust signal 1.0, got %f", bRemember.SourceTrust)
	}
	if bHeuristic.SourceTrust != 0.5 {
		t.Fatalf("expected source_trust signal 0.5, got %f", bHeuristic.SourceTrust)
	}
	if bRemember.FinalScore <= bHeuristic.FinalScore {
		t.Fatalf("expected higher final score for trusted source: remember=%f heuristic=%f", bRemember.FinalScore, bHeuristic.FinalScore)
	}
}

func TestCosineSimilarity(t *testing.T) {
	t.Run("identical vectors", func(t *testing.T) {
		got, ok := cosineSimilarity([]float32{1, 2, 3}, []float32{1, 2, 3})
		if !ok {
			t.Fatal("expected cosine to be available")
		}
		if math.Abs(got-1.0) > 1e-9 {
			t.Fatalf("expected cosine=1, got %f", got)
		}
	})

	t.Run("orthogonal vectors", func(t *testing.T) {
		got, ok := cosineSimilarity([]float32{1, 0}, []float32{0, 1})
		if !ok {
			t.Fatal("expected cosine to be available")
		}
		if math.Abs(got) > 1e-9 {
			t.Fatalf("expected cosine=0, got %f", got)
		}
	})

	t.Run("missing vector returns unavailable", func(t *testing.T) {
		if _, ok := cosineSimilarity([]float32{1, 0}, nil); ok {
			t.Fatal("expected unavailable cosine for missing vector")
		}
	})
}

func TestScoreItem_MissingEmbeddingsDoNotChangeScore(t *testing.T) {
	now := time.Now().UTC()
	item := ScoringCandidate{
		ID:          "item_1",
		Kind:        "memory",
		Subject:     "postgres notes",
		Body:        "postgres durability notes",
		Importance:  0.7,
		ProjectID:   "proj_1",
		Scope:       core.ScopeProject,
		CreatedAt:   now,
		UpdatedAt:   now,
		FTSPosition: 0,
	}

	base := ScoringContext{
		Query:         "postgres durability",
		QueryEntities: []string{"Postgres"},
		ProjectID:     "proj_1",
		RecentRecalls: map[string]bool{},
		Now:           now,
	}
	withMissingSemantic := base
	withMissingSemantic.QueryEmbedding = []float32{1, 0}

	bBase := ScoreItem(item, base)
	bMissing := ScoreItem(item, withMissingSemantic)
	if math.Abs(bBase.FinalScore-bMissing.FinalScore) > 1e-9 {
		t.Fatalf("expected score unchanged when semantic is unavailable: base=%f missing=%f", bBase.FinalScore, bMissing.FinalScore)
	}
	if bMissing.Semantic != 0 {
		t.Fatalf("expected semantic=0 with missing candidate embedding, got %f", bMissing.Semantic)
	}
}

func TestScoreItem_FinalScore(t *testing.T) {
	now := time.Now().UTC()
	sctx := ScoringContext{
		Query:         "test query about Alice",
		QueryEntities: []string{"Alice"},
		ProjectID:     "proj_1",
		RecentRecalls: map[string]bool{},
		Now:           now,
	}

	item := ScoringCandidate{
		ID:          "item_1",
		Kind:        "memory",
		Subject:     "Alice project notes",
		Body:        "Alice mentioned something important",
		Importance:  0.7,
		ProjectID:   "proj_1",
		Scope:       core.ScopeProject,
		CreatedAt:   now,
		UpdatedAt:   now,
		FTSPosition: 0,
	}

	b := ScoreItem(item, sctx)

	if b.FinalScore < 0 || b.FinalScore > 1 {
		t.Errorf("expected final score in [0,1], got %f", b.FinalScore)
	}
	if b.FinalScore == 0 {
		t.Error("expected non-zero final score for a well-matching item")
	}

	// Verify individual signals are populated.
	if b.Lexical == 0 {
		t.Error("expected non-zero lexical signal")
	}
	if b.ScopeFit == 0 {
		t.Error("expected non-zero scope_fit signal")
	}
	if b.Importance == 0 {
		t.Error("expected non-zero importance signal")
	}

	// A penalized item should score lower.
	sctxPenalized := sctx
	sctxPenalized.RecentRecalls = map[string]bool{"item_1": true}
	bPenalized := ScoreItem(item, sctxPenalized)
	if bPenalized.FinalScore >= b.FinalScore {
		t.Errorf("expected penalized score (%f) < unpenalized (%f)", bPenalized.FinalScore, b.FinalScore)
	}
}

func TestScoreItem_KindBoostPrioritizesMemoriesOverHistoryNodes(t *testing.T) {
	now := time.Now().UTC()
	sctx := ScoringContext{
		Query:         "sqlite migration decision",
		QueryEntities: []string{"SQLite"},
		ProjectID:     "proj_1",
		RecentRecalls: map[string]bool{},
		Now:           now,
	}

	base := ScoringCandidate{
		ID:          "item_1",
		Subject:     "SQLite migration",
		Body:        "We decided to keep SQLite migrations append-only",
		Importance:  0.9,
		ProjectID:   "proj_1",
		Scope:       core.ScopeProject,
		CreatedAt:   now,
		UpdatedAt:   now,
		FTSPosition: 0,
	}

	memory := base
	memory.Kind = "memory"
	history := base
	history.Kind = "history-node"

	memoryScore := ScoreItem(memory, sctx)
	historyScore := ScoreItem(history, sctx)

	if memoryScore.FinalScore <= historyScore.FinalScore {
		t.Fatalf("expected memory score (%f) > history-node score (%f)", memoryScore.FinalScore, historyScore.FinalScore)
	}
}

func TestShouldIncludeRecallCandidate(t *testing.T) {
	t.Run("filters low score", func(t *testing.T) {
		candidate := ScoringCandidate{Kind: "memory", Confidence: 0.9}
		include := shouldIncludeRecallCandidate(candidate, SignalBreakdown{FinalScore: 0.1}, defaultRecallFilterOptions())
		if include {
			t.Fatal("expected low-score candidate to be filtered")
		}
	})

	t.Run("filters low confidence memories", func(t *testing.T) {
		candidate := ScoringCandidate{Kind: "memory", Confidence: 0.1}
		include := shouldIncludeRecallCandidate(candidate, SignalBreakdown{FinalScore: 0.9}, defaultRecallFilterOptions())
		if include {
			t.Fatal("expected low-confidence memory to be filtered")
		}
	})

	t.Run("suppresses tool results in hybrid", func(t *testing.T) {
		candidate := ScoringCandidate{Kind: "history-node", Type: "tool_result"}
		include := shouldIncludeRecallCandidate(candidate, SignalBreakdown{FinalScore: 0.95}, hybridRecallFilterOptions())
		if include {
			t.Fatal("expected tool_result history nodes to be filtered in hybrid recall")
		}
	})

	t.Run("keeps tool results in history mode", func(t *testing.T) {
		candidate := ScoringCandidate{Kind: "history-node", Type: "tool_result"}
		include := shouldIncludeRecallCandidate(candidate, SignalBreakdown{FinalScore: 0.2}, historyRecallFilterOptions())
		if !include {
			t.Fatal("expected history mode to keep tool_result events")
		}
	})
}

func TestScoring_PenalizesProvisionalMemories(t *testing.T) {
	now := time.Now().UTC()
	sctx := ScoringContext{
		Query:         "sqlite decision",
		QueryEntities: []string{"SQLite"},
		RecentRecalls: map[string]bool{},
		ProjectID:     "proj_1",
		Now:           now,
	}

	base := ScoringCandidate{
		ID:               "mem_verified",
		Kind:             "memory",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_1",
		Subject:          "database",
		Body:             "We decided to use SQLite",
		TightDescription: "Use SQLite",
		Importance:       0.8,
		Confidence:       0.9,
		CreatedAt:        now,
		UpdatedAt:        now,
		FTSPosition:      0,
	}

	verified := base
	verified.ExtractionQuality = "verified"
	provisional := base
	provisional.ID = "mem_provisional"
	provisional.ExtractionQuality = "provisional"

	verifiedBreakdown := ScoreItem(verified, sctx)
	provisionalBreakdown := ScoreItem(provisional, sctx)

	if provisionalBreakdown.FinalScore >= verifiedBreakdown.FinalScore {
		t.Fatalf("expected provisional score (%f) < verified score (%f)", provisionalBreakdown.FinalScore, verifiedBreakdown.FinalScore)
	}
}
