package service

import (
	"math"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// Recall scoring uses a weighted blend of positive signals plus a repetition
// penalty. Semantic similarity is optional and only participates when both query
// and candidate embeddings are available. When semantic is absent, positive
// weights are scaled UP to redistribute semantic's share, keeping total
// contribution stable.
//
// Weight budget (raw, before normalization):
//   Lexical 0.14, ExtractionQuality 0.08, Semantic 0.18, EntityOverlap 0.20,
//   ScopeFit 0.10, Recency 0.08, Importance 0.07, TemporalValidity 0.05,
//   StructuralProximity 0.05, SourceTrust 0.05  → total 1.00
//
// Recency and freshness were collapsed into a single signal. The former
// freshness weight (0.04) was redistributed: +0.02 to entity overlap
// (0.18→0.20) and +0.02 to recency (0.06→0.08).

// ScoringWeights stores the weighted blend used for recall scoring.
type ScoringWeights struct {
	Lexical             float64 `json:"lexical"`
	ExtractionQuality   float64 `json:"extraction_quality"`
	Semantic            float64 `json:"semantic"`
	EntityOverlap       float64 `json:"entity_overlap"`
	ScopeFit            float64 `json:"scope_fit"`
	Recency             float64 `json:"recency"`
	Importance          float64 `json:"importance"`
	TemporalValidity    float64 `json:"temporal_validity"`
	StructuralProximity float64 `json:"structural_proximity"`
	SourceTrust         float64 `json:"source_trust"`
	KindBoost           float64 `json:"kind_boost"`
	RepetitionPenalty   float64 `json:"repetition_penalty"`
}

// DefaultScoringWeights returns the scoring constants.
// Raw weights sum to 1.0 (including Semantic). No pre-normalization factor
// needed — renormalization at score time handles the optional semantic signal.
func DefaultScoringWeights() ScoringWeights {
	return ScoringWeights{
		Lexical:             0.14,
		ExtractionQuality:   0.08,
		Semantic:            0.18,
		EntityOverlap:       0.20,
		ScopeFit:            0.10,
		Recency:             0.08,
		Importance:          0.07,
		TemporalValidity:    0.05,
		StructuralProximity: 0.05,
		SourceTrust:         0.05,
		KindBoost:           0.15,
		RepetitionPenalty:   0.10,
	}
}

// recencyHalfLifeDays controls exponential decay for the recency signal.
// Recency uses the most recent meaningful timestamp (creation, observation,
// update, or last confirmation).
const recencyHalfLifeDays = 14.0

// ScoringContext carries the query-time signals needed to rank recall
// candidates.
type ScoringContext struct {
	Query              string
	QueryEmbedding     []float32
	QueryEntities      []string // extracted from query
	QueryEntityWeights map[string]float64
	ProjectID          string
	SessionID          string
	RecentRecalls      map[string]bool // item IDs shown recently
	Now                time.Time
	Weights            *ScoringWeights
}

// SignalBreakdown records the per-signal contributions used to explain a final
// recall score.
type SignalBreakdown struct {
	Lexical             float64 `json:"lexical"`
	ExtractionQuality   float64 `json:"extraction_quality"`
	Semantic            float64 `json:"semantic"`
	EntityOverlap       float64 `json:"entity_overlap"`
	ScopeFit            float64 `json:"scope_fit"`
	Recency             float64 `json:"recency"`
	Importance          float64 `json:"importance"`
	TemporalValidity    float64 `json:"temporal_validity"`
	StructuralProximity float64 `json:"structural_proximity"`
	SourceTrust         float64 `json:"source_trust"`
	RepetitionPenalty   float64 `json:"repetition_penalty"`
	FinalScore          float64 `json:"final_score"`
}

func (b SignalBreakdown) ToMap() map[string]float64 {
	return map[string]float64{
		"lexical":              b.Lexical,
		"extraction_quality":   b.ExtractionQuality,
		"semantic":             b.Semantic,
		"entity_overlap":       b.EntityOverlap,
		"scope_fit":            b.ScopeFit,
		"recency":              b.Recency,
		"importance":           b.Importance,
		"temporal_validity":    b.TemporalValidity,
		"structural_proximity": b.StructuralProximity,
		"source_trust":         b.SourceTrust,
		"repetition_penalty":   b.RepetitionPenalty,
		"final_score":          b.FinalScore,
	}
}

// ScoringCandidate is the normalized representation of a memory-like item used
// by the recall scoring engine.
type ScoringCandidate struct {
	ID                string
	Kind              string // memory, summary, episode, history-node
	Type              string
	Scope             core.Scope
	Subject           string
	Body              string
	TightDescription  string
	Importance        float64
	Confidence        float64
	Tags              []string
	ProjectID         string
	Status            string
	ObservedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ValidFrom         *time.Time
	ValidTo           *time.Time
	LastConfirmedAt   *time.Time
	SupersededBy      string
	SourceEventIDs    []string
	EntityNames       []string
	EntityAliases     []string
	Embedding         []float32
	ExtractionQuality string
	SourceSystem      string
	FTSPosition       int // position in FTS results (0-based)
}

// ScoreItem computes the weighted recall score and signal breakdown for item.
func ScoreItem(item ScoringCandidate, sctx ScoringContext) SignalBreakdown {
	var b SignalBreakdown
	weights := sctx.Weights
	if weights == nil {
		defaults := DefaultScoringWeights()
		weights = &defaults
	}

	b.Lexical = signalLexical(item.FTSPosition)
	b.ExtractionQuality = signalExtractionQuality(item)
	b.Semantic = signalSemantic(item, sctx)
	b.EntityOverlap = signalEntityOverlap(item, sctx.QueryEntities, sctx.QueryEntityWeights)
	b.ScopeFit = signalScopeFit(item, sctx)
	b.Recency = signalRecency(item, sctx.Now)
	b.Importance = signalImportance(item)
	b.TemporalValidity = signalTemporalValidity(item, sctx.Now)
	b.StructuralProximity = signalStructuralProximity(item)
	b.SourceTrust = signalSourceTrust(item)
	b.RepetitionPenalty = signalRepetitionPenalty(item, sctx)

	// totalPositive includes Semantic weight — it represents the full budget.
	// When semantic is absent, activePositive < totalPositive, so renorm > 1.0,
	// redistributing the missing semantic share across present signals.
	totalPositive := weights.Lexical + weights.ExtractionQuality + weights.Semantic + weights.EntityOverlap + weights.ScopeFit + weights.Recency + weights.Importance + weights.TemporalValidity + weights.StructuralProximity + weights.SourceTrust
	activePositive := totalPositive
	if !semanticSignalAvailable(item, sctx) {
		activePositive -= weights.Semantic
	}

	renorm := 1.0
	if activePositive > 0 {
		renorm = totalPositive / activePositive
	}

	b.FinalScore = renorm*(weights.Lexical*b.Lexical+
		weights.ExtractionQuality*b.ExtractionQuality+
		weights.Semantic*b.Semantic+
		weights.EntityOverlap*b.EntityOverlap+
		weights.ScopeFit*b.ScopeFit+
		weights.Recency*b.Recency+
		weights.Importance*b.Importance+
		weights.TemporalValidity*b.TemporalValidity+
		weights.StructuralProximity*b.StructuralProximity+
		weights.SourceTrust*b.SourceTrust) - weights.RepetitionPenalty*b.RepetitionPenalty

	kindMultiplier := signalKindBoost(item.Kind)
	b.FinalScore *= (1 - weights.KindBoost) + (weights.KindBoost * kindMultiplier)

	// Clamp to [0, 1].
	if b.FinalScore < 0 {
		b.FinalScore = 0
	}
	if b.FinalScore > 1 {
		b.FinalScore = 1
	}

	return b
}

func signalKindBoost(kind string) float64 {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "memory":
		return 1.0
	case "episode":
		return 0.80
	case "summary":
		return 0.60
	case "event", "history-node":
		return 0.45
	default:
		return 0.60
	}
}

// --- signal implementations ---

// signalLexical maps FTS result position to a 0-1 score.
// Position 0 (best) yields 1.0, decaying with the same formula used by positionScore.
func signalLexical(ftsPosition int) float64 {
	if ftsPosition <= 0 {
		return 1.0
	}
	return 1.0 / (1.0 + float64(ftsPosition)*0.2)
}

func signalExtractionQuality(item ScoringCandidate) float64 {
	switch strings.ToLower(strings.TrimSpace(item.ExtractionQuality)) {
	case "provisional":
		return 0.5
	case "upgraded":
		return 0.9
	default:
		return 0.7
	}
}

// signalSemantic computes cosine similarity between query and candidate
// embeddings. Missing embeddings produce an absent semantic signal (0.0).
func signalSemantic(item ScoringCandidate, sctx ScoringContext) float64 {
	cos, ok := cosineSimilarity(sctx.QueryEmbedding, item.Embedding)
	if !ok {
		return 0.0
	}
	if cos < 0 {
		return 0.0
	}
	if cos > 1 {
		return 1.0
	}
	return cos
}

func semanticSignalAvailable(item ScoringCandidate, sctx ScoringContext) bool {
	_, ok := cosineSimilarity(sctx.QueryEmbedding, item.Embedding)
	return ok
}

func cosineSimilarity(a, b []float32) (float64, bool) {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0, false
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), true
}

// signalEntityOverlap counts how many query entities appear in the item text.
func signalEntityOverlap(item ScoringCandidate, queryEntities []string, queryEntityWeights map[string]float64) float64 {
	if len(queryEntities) == 0 && len(queryEntityWeights) == 0 {
		return 0.0
	}

	// Build a single lower-case haystack from subject, body, tight_description, and tags.
	var sb strings.Builder
	sb.WriteString(strings.ToLower(item.Subject))
	sb.WriteByte(' ')
	sb.WriteString(strings.ToLower(item.Body))
	sb.WriteByte(' ')
	sb.WriteString(strings.ToLower(item.TightDescription))
	for _, tag := range item.Tags {
		sb.WriteByte(' ')
		sb.WriteString(strings.ToLower(tag))
	}
	haystack := sb.String()
	linkedEntityTerms := make(map[string]bool, len(item.EntityNames)+len(item.EntityAliases))
	for _, name := range item.EntityNames {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed != "" {
			linkedEntityTerms[trimmed] = true
		}
	}
	for _, alias := range item.EntityAliases {
		trimmed := strings.ToLower(strings.TrimSpace(alias))
		if trimmed != "" {
			linkedEntityTerms[trimmed] = true
		}
	}

	weightedTerms := queryEntityWeights
	if len(weightedTerms) == 0 {
		weightedTerms = make(map[string]float64, len(queryEntities))
		for _, entity := range queryEntities {
			trimmed := strings.ToLower(strings.TrimSpace(entity))
			if trimmed == "" {
				continue
			}
			weightedTerms[trimmed] = 1.0
		}
	}

	var matchedWeight float64
	var totalWeight float64
	for term, weight := range weightedTerms {
		if weight <= 0 || term == "" {
			continue
		}
		totalWeight += weight
		if strings.Contains(haystack, term) || linkedEntityTerms[term] {
			matchedWeight += weight
		}
	}
	if totalWeight == 0 {
		return 0.0
	}
	return matchedWeight / totalWeight
}

// signalScopeFit returns how well the item scope matches the query context.
func signalScopeFit(item ScoringCandidate, sctx ScoringContext) float64 {
	if sctx.ProjectID == "" {
		// No project context — any item is acceptable.
		if item.Scope == core.ScopeGlobal {
			return 1.0
		}
		return 0.5
	}
	// Project context is set.
	if item.ProjectID == sctx.ProjectID {
		return 1.0
	}
	if item.Scope == core.ScopeGlobal {
		return 0.5
	}
	return 0.3
}

// signalRecency uses exponential decay from the most recent timestamp.
func signalRecency(item ScoringCandidate, now time.Time) float64 {
	ts := mostRecentTimestamp(item)
	days := now.Sub(ts).Hours() / 24.0
	if days < 0 {
		days = 0
	}
	return math.Exp(-0.693 * days / recencyHalfLifeDays)
}

// signalImportance returns the item importance directly (already 0-1).
func signalImportance(item ScoringCandidate) float64 {
	if item.Importance < 0 {
		return 0.0
	}
	if item.Importance > 1 {
		return 1.0
	}
	return item.Importance
}

// signalTemporalValidity checks whether the item is still valid and applies a
// staleness penalty when the content contains relative-time language but the
// item is old.
func signalTemporalValidity(item ScoringCandidate, now time.Time) float64 {
	if item.SupersededBy != "" {
		return 0.5
	}
	if item.ValidTo != nil && item.ValidTo.Before(now) {
		return 0.0
	}
	base := 1.0
	penalty := temporalStalenessPenalty(item, now)
	result := base * (1.0 - penalty)
	if result < 0 {
		return 0
	}
	return result
}

// temporalStalenessAgeDays is the age threshold (in days) before relative-time
// language triggers a staleness penalty.
const temporalStalenessAgeDays = 14

// temporalStalenessMaxPenalty is the maximum penalty applied (caps the ramp).
const temporalStalenessMaxPenalty = 0.3

// temporalStalenessRampDays controls how many days it takes to reach the max
// penalty after exceeding the age threshold.
const temporalStalenessRampDays = 180.0

// temporalStalenessPenalty returns a 0–0.3 penalty when the item body contains
// relative-time references ("today", "currently", etc.) and the item is older
// than temporalStalenessAgeDays. The penalty ramps linearly over 180 days.
func temporalStalenessPenalty(item ScoringCandidate, now time.Time) float64 {
	// Use CreatedAt rather than mostRecentTimestamp to avoid metadata-only
	// updates (e.g. status changes) resetting the staleness clock.
	ts := item.CreatedAt
	days := now.Sub(ts).Hours() / 24.0
	if days <= float64(temporalStalenessAgeDays) {
		return 0
	}
	if !containsRelativeTimeLanguage(item.Body) && !containsRelativeTimeLanguage(item.TightDescription) {
		return 0
	}
	excess := days - float64(temporalStalenessAgeDays)
	penalty := (excess / temporalStalenessRampDays) * temporalStalenessMaxPenalty
	if penalty > temporalStalenessMaxPenalty {
		return temporalStalenessMaxPenalty
	}
	return penalty
}

// relativeTimeWords are phrases that imply time-relative claims.
var relativeTimeWords = []string{
	"today",
	"currently",
	"right now",
	"at the moment",
	"this week",
	"this sprint",
	"this month",
	"as of now",
	"at present",
	"latest",
	"just now",
	"now using",
	"now use",
}

// containsRelativeTimeLanguage checks if text contains relative-time phrases.
func containsRelativeTimeLanguage(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range relativeTimeWords {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// signalStructuralProximity rewards well-linked items.
func signalStructuralProximity(item ScoringCandidate) float64 {
	if len(item.SourceEventIDs) > 0 {
		return 1.0
	}
	return 0.5
}

func signalSourceTrust(item ScoringCandidate) float64 {
	source := strings.ToLower(strings.TrimSpace(item.SourceSystem))
	switch {
	case source == "remember" || source == "explicit":
		return 1.0
	case source == "":
		return 0.6
	case source == "claude-code" || source == "opencode" || source == "codex":
		return 0.9
	case source == "reflect" || source == "reprocess":
		return 0.8
	case strings.Contains(source, "hook") || strings.Contains(source, "webhook"):
		return 0.7
	case source == "heuristic":
		return 0.5
	default:
		return 0.6
	}
}

// signalRepetitionPenalty returns 1.0 if the item was recently shown.
func signalRepetitionPenalty(item ScoringCandidate, sctx ScoringContext) float64 {
	if sctx.RecentRecalls != nil && sctx.RecentRecalls[item.ID] {
		return 1.0
	}
	return 0.0
}

// --- timestamp helpers ---

// mostRecentTimestamp picks the latest meaningful timestamp on an item.
func mostRecentTimestamp(item ScoringCandidate) time.Time {
	best := item.CreatedAt
	if item.UpdatedAt.After(best) {
		best = item.UpdatedAt
	}
	if item.ObservedAt != nil && item.ObservedAt.After(best) {
		best = *item.ObservedAt
	}
	if item.LastConfirmedAt != nil && item.LastConfirmedAt.After(best) {
		best = *item.LastConfirmedAt
	}
	return best
}

// --- conversion helpers ---

// MemoryToCandidate converts a memory into a scoring candidate using ftsPos as
// its lexical rank.
func MemoryToCandidate(m core.Memory, ftsPos int) ScoringCandidate {
	sourceSystem := strings.TrimSpace(m.Metadata["source_system"])
	if strings.EqualFold(strings.TrimSpace(m.Metadata["extraction_method"]), "heuristic") {
		sourceSystem = "heuristic"
	} else if sourceSystem == "" && len(m.SourceEventIDs) == 0 {
		sourceSystem = "remember"
	}

	return ScoringCandidate{
		ID:                m.ID,
		Kind:              "memory",
		Type:              string(m.Type),
		Scope:             m.Scope,
		Subject:           m.Subject,
		Body:              m.Body,
		TightDescription:  m.TightDescription,
		Importance:        m.Importance,
		Confidence:        m.Confidence,
		Tags:              m.Tags,
		ProjectID:         m.ProjectID,
		Status:            string(m.Status),
		ObservedAt:        m.ObservedAt,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
		ValidFrom:         m.ValidFrom,
		ValidTo:           m.ValidTo,
		LastConfirmedAt:   m.LastConfirmedAt,
		SupersededBy:      m.SupersededBy,
		SourceEventIDs:    m.SourceEventIDs,
		ExtractionQuality: m.Metadata["extraction_quality"],
		SourceSystem:      sourceSystem,
		FTSPosition:       ftsPos,
	}
}

// SummaryToCandidate converts a summary into a scoring candidate using ftsPos
// as its lexical rank.
func SummaryToCandidate(s core.Summary, ftsPos int) ScoringCandidate {
	return ScoringCandidate{
		ID:               s.ID,
		Kind:             "summary",
		Type:             s.Kind,
		Scope:            s.Scope,
		Subject:          s.Title,
		Body:             s.Body,
		TightDescription: s.TightDescription,
		ProjectID:        s.ProjectID,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		SourceEventIDs:   s.SourceSpan.EventIDs,
		SourceSystem:     "reflect",
		FTSPosition:      ftsPos,
	}
}

// EpisodeToCandidate converts an episode into a scoring candidate using ftsPos
// as its lexical rank.
func EpisodeToCandidate(e core.Episode, ftsPos int) ScoringCandidate {
	return ScoringCandidate{
		ID:               e.ID,
		Kind:             "episode",
		Scope:            e.Scope,
		Subject:          e.Title,
		Body:             e.Summary,
		TightDescription: e.TightDescription,
		Importance:       e.Importance,
		ProjectID:        e.ProjectID,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
		SourceEventIDs:   e.SourceSpan.EventIDs,
		SourceSystem:     "reflect",
		FTSPosition:      ftsPos,
	}
}

// EventToCandidate converts an event into a history-node scoring candidate
// using ftsPos as its lexical rank.
func EventToCandidate(e core.Event, ftsPos int) ScoringCandidate {
	return ScoringCandidate{
		ID:               e.ID,
		Kind:             "history-node",
		Type:             e.Kind,
		Body:             e.Content,
		TightDescription: e.Content,
		ProjectID:        e.ProjectID,
		CreatedAt:        e.OccurredAt,
		UpdatedAt:        e.IngestedAt,
		SourceSystem:     e.SourceSystem,
		FTSPosition:      ftsPos,
	}
}
