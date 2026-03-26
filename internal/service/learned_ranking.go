package service

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const rankingPriorStrength = 100.0

type signalAggregate struct {
	shownSum    float64
	expandedSum float64
	shownCount  float64
	expandedCnt float64
}

func (s *AMMService) UpdateRankingWeights(ctx context.Context) (int, error) {
	stats, err := s.repo.ListMemoryAccessStats(ctx, time.Unix(0, 0).UTC())
	if err != nil {
		return 0, err
	}
	if len(stats) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	aggs := map[string]*signalAggregate{
		"extraction_quality":   {},
		"recency":              {},
		"importance":           {},
		"temporal_validity":    {},
		"structural_proximity": {},
		"freshness":            {},
	}

	totalShown := 0.0
	for _, stat := range stats {
		if stat.AccessCount <= 0 {
			continue
		}
		memory, err := s.repo.GetMemory(ctx, stat.MemoryID)
		if err != nil || memory == nil {
			continue
		}

		shownCount := float64(stat.AccessCount)
		expandedCount, err := s.countExpandedFeedback(ctx, memory.ID)
		if err != nil {
			return 0, err
		}
		if expandedCount > shownCount {
			expandedCount = shownCount
		}

		candidate := MemoryToCandidate(*memory, 0)
		signals := map[string]float64{
			"extraction_quality":   signalExtractionQuality(candidate),
			"recency":              signalRecency(candidate, now),
			"importance":           signalImportance(candidate),
			"temporal_validity":    signalTemporalValidity(candidate, now),
			"structural_proximity": signalStructuralProximity(candidate),
			"freshness":            signalFreshness(candidate, now),
		}
		for key, value := range signals {
			agg := aggs[key]
			agg.shownSum += value * shownCount
			agg.expandedSum += value * expandedCount
			agg.shownCount += shownCount
			agg.expandedCnt += expandedCount
		}
		totalShown += shownCount
	}

	if totalShown == 0 {
		return 0, nil
	}

	prior := s.getScoringWeights()
	updated := prior
	updated.ExtractionQuality = bayesianSignalWeight(prior.ExtractionQuality, aggs["extraction_quality"], totalShown)
	updated.Recency = bayesianSignalWeight(prior.Recency, aggs["recency"], totalShown)
	updated.Importance = bayesianSignalWeight(prior.Importance, aggs["importance"], totalShown)
	updated.TemporalValidity = bayesianSignalWeight(prior.TemporalValidity, aggs["temporal_validity"], totalShown)
	updated.StructuralProximity = bayesianSignalWeight(prior.StructuralProximity, aggs["structural_proximity"], totalShown)
	updated.Freshness = bayesianSignalWeight(prior.Freshness, aggs["freshness"], totalShown)

	normalizePositiveWeights(&updated, totalPositiveWeight(DefaultScoringWeights()))

	updatesApplied := countWeightDiffs(prior, updated)
	if updatesApplied > 0 {
		s.setScoringWeights(updated)
	}
	return updatesApplied, nil
}

func (s *AMMService) countExpandedFeedback(ctx context.Context, memoryID string) (float64, error) {
	entries, err := s.repo.ListRelevanceFeedback(ctx, memoryID)
	if err != nil {
		return 0, err
	}
	count := 0.0
	for _, entry := range entries {
		if entry.Action == "expanded" {
			count += 1.0
		}
	}
	return count, nil
}

func bayesianSignalWeight(prior float64, agg *signalAggregate, dataCount float64) float64 {
	if agg == nil || agg.shownCount == 0 || dataCount == 0 {
		return prior
	}
	shownAvg := agg.shownSum / agg.shownCount
	expandedAvg := shownAvg
	if agg.expandedCnt > 0 {
		expandedAvg = agg.expandedSum / agg.expandedCnt
	}
	lift := expandedAvg / math.Max(shownAvg, 1e-9)
	if lift < 0.5 {
		lift = 0.5
	}
	if lift > 1.5 {
		lift = 1.5
	}
	dataWeight := prior * lift
	return (prior*rankingPriorStrength + dataWeight*dataCount) / (rankingPriorStrength + dataCount)
}

func totalPositiveWeight(w ScoringWeights) float64 {
	return w.Lexical + w.ExtractionQuality + w.Semantic + w.EntityOverlap + w.ScopeFit + w.Recency + w.Importance + w.TemporalValidity + w.StructuralProximity + w.Freshness
}

func normalizePositiveWeights(w *ScoringWeights, target float64) {
	if w == nil {
		return
	}
	current := totalPositiveWeight(*w)
	if current <= 0 || target <= 0 {
		defaults := DefaultScoringWeights()
		w.Lexical = defaults.Lexical
		w.ExtractionQuality = defaults.ExtractionQuality
		w.Semantic = defaults.Semantic
		w.EntityOverlap = defaults.EntityOverlap
		w.ScopeFit = defaults.ScopeFit
		w.Recency = defaults.Recency
		w.Importance = defaults.Importance
		w.TemporalValidity = defaults.TemporalValidity
		w.StructuralProximity = defaults.StructuralProximity
		w.Freshness = defaults.Freshness
		return
	}
	scale := target / current
	w.Lexical *= scale
	w.ExtractionQuality *= scale
	w.Semantic *= scale
	w.EntityOverlap *= scale
	w.ScopeFit *= scale
	w.Recency *= scale
	w.Importance *= scale
	w.TemporalValidity *= scale
	w.StructuralProximity *= scale
	w.Freshness *= scale
}

func countWeightDiffs(a, b ScoringWeights) int {
	count := 0
	for _, pair := range [][2]float64{{a.Lexical, b.Lexical}, {a.ExtractionQuality, b.ExtractionQuality}, {a.Semantic, b.Semantic}, {a.EntityOverlap, b.EntityOverlap}, {a.ScopeFit, b.ScopeFit}, {a.Recency, b.Recency}, {a.Importance, b.Importance}, {a.TemporalValidity, b.TemporalValidity}, {a.StructuralProximity, b.StructuralProximity}, {a.Freshness, b.Freshness}, {a.RepetitionPenalty, b.RepetitionPenalty}} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			count++
		}
	}
	return count
}

func (s *AMMService) loadScoringWeights(ctx context.Context) error {
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{Kind: "update_ranking_weights", Status: "completed", Limit: 20})
	if err != nil {
		return err
	}

	for _, job := range jobs {
		raw := job.Result["scoring_weights"]
		if raw == "" {
			continue
		}
		var weights ScoringWeights
		if err := json.Unmarshal([]byte(raw), &weights); err != nil {
			continue
		}
		if totalPositiveWeight(weights) <= 0 {
			continue
		}
		s.setScoringWeights(weights)
		return nil
	}

	s.setScoringWeights(DefaultScoringWeights())
	return nil
}

func (s *AMMService) scoringWeightsJSON() string {
	weights := s.getScoringWeights()
	data, err := json.Marshal(weights)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *AMMService) getScoringWeights() ScoringWeights {
	s.scoringWeightsMu.RLock()
	defer s.scoringWeightsMu.RUnlock()
	return s.scoringWeights
}

func (s *AMMService) setScoringWeights(weights ScoringWeights) {
	s.scoringWeightsMu.Lock()
	defer s.scoringWeightsMu.Unlock()
	s.scoringWeights = weights
}
