package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestLearnedRanking_DefaultWeights(t *testing.T) {
	weights := DefaultScoringWeights()
	want := ScoringWeights{
		Lexical:             0.14 * scoringNormalizationFactor,
		ExtractionQuality:   0.08 * scoringNormalizationFactor,
		Semantic:            0.18 * scoringNormalizationFactor,
		EntityOverlap:       0.18 * scoringNormalizationFactor,
		ScopeFit:            0.10 * scoringNormalizationFactor,
		Recency:             0.06 * scoringNormalizationFactor,
		Importance:          0.07 * scoringNormalizationFactor,
		TemporalValidity:    0.05 * scoringNormalizationFactor,
		StructuralProximity: 0.05 * scoringNormalizationFactor,
		Freshness:           0.04 * scoringNormalizationFactor,
		SourceTrust:         0.05 * scoringNormalizationFactor,
		RepetitionPenalty:   0.10,
	}

	for name, pair := range map[string][2]float64{
		"lexical":              {weights.Lexical, want.Lexical},
		"extraction_quality":   {weights.ExtractionQuality, want.ExtractionQuality},
		"semantic":             {weights.Semantic, want.Semantic},
		"entity_overlap":       {weights.EntityOverlap, want.EntityOverlap},
		"scope_fit":            {weights.ScopeFit, want.ScopeFit},
		"recency":              {weights.Recency, want.Recency},
		"importance":           {weights.Importance, want.Importance},
		"temporal_validity":    {weights.TemporalValidity, want.TemporalValidity},
		"structural_proximity": {weights.StructuralProximity, want.StructuralProximity},
		"freshness":            {weights.Freshness, want.Freshness},
		"source_trust":         {weights.SourceTrust, want.SourceTrust},
		"repetition_penalty":   {weights.RepetitionPenalty, want.RepetitionPenalty},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-12 {
			t.Fatalf("%s mismatch: got=%f want=%f", name, pair[0], pair[1])
		}
	}
}

func TestLearnedRanking_LoadableWeights(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	custom := ScoringWeights{
		Importance: 1.0,
	}
	encoded, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal custom weights: %v", err)
	}

	now := time.Now().UTC()
	if err := repo.InsertJob(ctx, &core.Job{
		ID:        "job_custom_weights",
		Kind:      "update_ranking_weights",
		Status:    "completed",
		Result:    map[string]string{"scoring_weights": string(encoded)},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert learned ranking job: %v", err)
	}

	if err := svc.loadScoringWeights(ctx); err != nil {
		t.Fatalf("load scoring weights: %v", err)
	}

	if math.Abs(svc.scoringWeights.Importance-1.0) > 1e-12 || svc.scoringWeights.Lexical != 0 {
		t.Fatalf("unexpected loaded weights: %+v", svc.scoringWeights)
	}

	itemHighLexLowImportance := ScoringCandidate{
		ID:          "a",
		Kind:        "memory",
		Scope:       core.ScopeGlobal,
		Importance:  0.0,
		CreatedAt:   now,
		UpdatedAt:   now,
		FTSPosition: 0,
	}
	itemLowLexHighImportance := ScoringCandidate{
		ID:          "b",
		Kind:        "memory",
		Scope:       core.ScopeGlobal,
		Importance:  1.0,
		CreatedAt:   now,
		UpdatedAt:   now,
		FTSPosition: 20,
	}

	defaultCtx := ScoringContext{Now: now}
	loadedCtx := ScoringContext{Now: now, Weights: &svc.scoringWeights}

	defaultHighLex := ScoreItem(itemHighLexLowImportance, defaultCtx)
	defaultHighImportance := ScoreItem(itemLowLexHighImportance, defaultCtx)
	if defaultHighLex.FinalScore <= defaultHighImportance.FinalScore {
		t.Fatalf("expected default lexical-heavy ranking to prefer lexical match: lexical=%f importance=%f", defaultHighLex.FinalScore, defaultHighImportance.FinalScore)
	}

	loadedHighLex := ScoreItem(itemHighLexLowImportance, loadedCtx)
	loadedHighImportance := ScoreItem(itemLowLexHighImportance, loadedCtx)
	if loadedHighImportance.FinalScore <= loadedHighLex.FinalScore {
		t.Fatalf("expected loaded custom weights to prefer importance: lexical=%f importance=%f", loadedHighLex.FinalScore, loadedHighImportance.FinalScore)
	}
}

func TestLearnedRanking_StrongPrior(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	high, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "high-importance memory",
		TightDescription: "high",
		Importance:       0.9,
	})
	if err != nil {
		t.Fatalf("remember high memory: %v", err)
	}
	low, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "low-importance memory",
		TightDescription: "low",
		Importance:       0.1,
	})
	if err != nil {
		t.Fatalf("remember low memory: %v", err)
	}

	if err := repo.RecordRecall(ctx, "sess_strong_prior", high.ID, "memory"); err != nil {
		t.Fatalf("record high recall: %v", err)
	}
	if err := repo.RecordRecall(ctx, "sess_strong_prior", low.ID, "memory"); err != nil {
		t.Fatalf("record low recall: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_strong_prior", high.ID, "memory", "expanded"); err != nil {
		t.Fatalf("record expanded feedback: %v", err)
	}

	before := svc.scoringWeights
	updates, err := svc.UpdateRankingWeights(ctx)
	if err != nil {
		t.Fatalf("update ranking weights: %v", err)
	}
	if updates == 0 {
		t.Fatal("expected at least one weight to be updated")
	}

	after := svc.scoringWeights
	if after.Importance <= before.Importance {
		t.Fatalf("expected importance weight to increase: before=%f after=%f", before.Importance, after.Importance)
	}
	changeRatio := math.Abs(after.Importance-before.Importance) / math.Max(before.Importance, 1e-9)
	if changeRatio > 0.02 {
		t.Fatalf("expected strong prior to keep update small, ratio=%f before=%f after=%f", changeRatio, before.Importance, after.Importance)
	}
}

func TestLearnedRanking_RunJob(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "job update ranking memory",
		TightDescription: "job update",
		Importance:       0.9,
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}
	if err := repo.RecordRecall(ctx, "sess_job", mem.ID, "memory"); err != nil {
		t.Fatalf("record recall: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_job", mem.ID, "memory", "expanded"); err != nil {
		t.Fatalf("insert relevance feedback: %v", err)
	}

	job, err := svc.RunJob(ctx, "update_ranking_weights")
	if err != nil {
		t.Fatalf("run update_ranking_weights job: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed job status, got %+v", job)
	}
	if job.Result["action"] != "update_ranking_weights" {
		t.Fatalf("unexpected job action: %+v", job.Result)
	}

	rawWeights := job.Result["scoring_weights"]
	if rawWeights == "" {
		t.Fatalf("expected scoring_weights in job result: %+v", job.Result)
	}
	var persisted ScoringWeights
	if err := json.Unmarshal([]byte(rawWeights), &persisted); err != nil {
		t.Fatalf("unmarshal persisted scoring weights: %v", err)
	}
	if totalPositiveWeight(persisted) <= 0 {
		t.Fatalf("expected persisted weights to include positive signal mass, got %+v", persisted)
	}
}
