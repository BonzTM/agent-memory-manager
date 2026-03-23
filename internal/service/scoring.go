package service

import (
	"math"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// v0 weights: semantic is disabled, so we renormalize the remaining 9 signals.
// Original weights (sum = 1.0):
//
//	lexical=0.25, semantic=0.18, entity_overlap=0.18, scope_fit=0.10,
//	recency=0.08, importance=0.07, temporal_validity=0.05,
//	structural_proximity=0.05, freshness=0.04, repetition_penalty=-0.10
//
// Without semantic the positive weights sum to 0.57 (from 0.75).
// We scale each positive weight by 0.75/0.57 ≈ 1.3158 so they sum to ~0.75.
// The penalty stays at -0.10 (not renormalized — it is a deduction, not a share).
const (
	wLexical             = 0.25 * (0.75 / 0.57)  // ~0.3289
	wEntityOverlap       = 0.18 * (0.75 / 0.57)  // ~0.2368
	wScopeFit            = 0.10 * (0.75 / 0.57)  // ~0.1316
	wRecency             = 0.08 * (0.75 / 0.57)  // ~0.1053
	wImportance          = 0.07 * (0.75 / 0.57)  // ~0.0921
	wTemporalValidity    = 0.05 * (0.75 / 0.57)  // ~0.0658
	wStructuralProximity = 0.05 * (0.75 / 0.57)  // ~0.0658
	wFreshness           = 0.04 * (0.75 / 0.57)  // ~0.0526
	wRepetitionPenalty   = 0.10                    // deducted
)

// recencyHalfLifeDays controls exponential decay for the recency signal.
const recencyHalfLifeDays = 14.0

// ScoringContext holds the query context for scoring.
type ScoringContext struct {
	Query         string
	QueryEntities []string          // extracted from query
	ProjectID     string
	SessionID     string
	RecentRecalls map[string]bool   // item IDs shown recently
	Now           time.Time
}

// SignalBreakdown shows the per-signal scores for explainability.
type SignalBreakdown struct {
	Lexical             float64 `json:"lexical"`
	EntityOverlap       float64 `json:"entity_overlap"`
	ScopeFit            float64 `json:"scope_fit"`
	Recency             float64 `json:"recency"`
	Importance          float64 `json:"importance"`
	TemporalValidity    float64 `json:"temporal_validity"`
	StructuralProximity float64 `json:"structural_proximity"`
	Freshness           float64 `json:"freshness"`
	RepetitionPenalty   float64 `json:"repetition_penalty"`
	FinalScore          float64 `json:"final_score"`
}

// ScoringCandidate is a type-erased representation of any scoreable item.
type ScoringCandidate struct {
	ID               string
	Kind             string // memory, summary, episode, history-node
	Type             string
	Scope            core.Scope
	Subject          string
	Body             string
	TightDescription string
	Importance       float64
	Confidence       float64
	Tags             []string
	ProjectID        string
	Status           string
	ObservedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ValidFrom        *time.Time
	ValidTo          *time.Time
	LastConfirmedAt  *time.Time
	SupersededBy     string
	SourceEventIDs   []string
	FTSPosition      int // position in FTS results (0-based)
}

// ScoreItem computes the weighted multi-signal score for a candidate.
func ScoreItem(item ScoringCandidate, sctx ScoringContext) SignalBreakdown {
	var b SignalBreakdown

	b.Lexical = signalLexical(item.FTSPosition)
	b.EntityOverlap = signalEntityOverlap(item, sctx.QueryEntities)
	b.ScopeFit = signalScopeFit(item, sctx)
	b.Recency = signalRecency(item, sctx.Now)
	b.Importance = signalImportance(item)
	b.TemporalValidity = signalTemporalValidity(item, sctx.Now)
	b.StructuralProximity = signalStructuralProximity(item)
	b.Freshness = signalFreshness(item, sctx.Now)
	b.RepetitionPenalty = signalRepetitionPenalty(item, sctx)

	b.FinalScore = wLexical*b.Lexical +
		wEntityOverlap*b.EntityOverlap +
		wScopeFit*b.ScopeFit +
		wRecency*b.Recency +
		wImportance*b.Importance +
		wTemporalValidity*b.TemporalValidity +
		wStructuralProximity*b.StructuralProximity +
		wFreshness*b.Freshness -
		wRepetitionPenalty*b.RepetitionPenalty

	// Clamp to [0, 1].
	if b.FinalScore < 0 {
		b.FinalScore = 0
	}
	if b.FinalScore > 1 {
		b.FinalScore = 1
	}

	return b
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

// signalEntityOverlap counts how many query entities appear in the item text.
func signalEntityOverlap(item ScoringCandidate, queryEntities []string) float64 {
	if len(queryEntities) == 0 {
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

	matches := 0
	for _, ent := range queryEntities {
		if strings.Contains(haystack, strings.ToLower(ent)) {
			matches++
		}
	}
	return float64(matches) / float64(len(queryEntities))
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

// signalTemporalValidity checks whether the item is still valid.
func signalTemporalValidity(item ScoringCandidate, now time.Time) float64 {
	if item.SupersededBy != "" {
		return 0.5
	}
	if item.ValidTo != nil && item.ValidTo.Before(now) {
		return 0.0
	}
	return 1.0
}

// signalStructuralProximity rewards well-linked items.
func signalStructuralProximity(item ScoringCandidate) float64 {
	if len(item.SourceEventIDs) > 0 {
		return 1.0
	}
	return 0.5
}

// signalFreshness uses the same half-life decay based on last touch.
func signalFreshness(item ScoringCandidate, now time.Time) float64 {
	ts := lastTouchTimestamp(item)
	days := now.Sub(ts).Hours() / 24.0
	if days < 0 {
		days = 0
	}
	return math.Exp(-0.693 * days / recencyHalfLifeDays)
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

// lastTouchTimestamp returns the last update/confirmation time for freshness.
func lastTouchTimestamp(item ScoringCandidate) time.Time {
	best := item.UpdatedAt
	if item.LastConfirmedAt != nil && item.LastConfirmedAt.After(best) {
		best = *item.LastConfirmedAt
	}
	return best
}

// --- conversion helpers ---

// MemoryToCandidate converts a core.Memory to a ScoringCandidate.
func MemoryToCandidate(m core.Memory, ftsPos int) ScoringCandidate {
	return ScoringCandidate{
		ID:               m.ID,
		Kind:             "memory",
		Type:             string(m.Type),
		Scope:            m.Scope,
		Subject:          m.Subject,
		Body:             m.Body,
		TightDescription: m.TightDescription,
		Importance:       m.Importance,
		Confidence:       m.Confidence,
		Tags:             m.Tags,
		ProjectID:        m.ProjectID,
		Status:           string(m.Status),
		ObservedAt:       m.ObservedAt,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		ValidFrom:        m.ValidFrom,
		ValidTo:          m.ValidTo,
		LastConfirmedAt:  m.LastConfirmedAt,
		SupersededBy:     m.SupersededBy,
		SourceEventIDs:   m.SourceEventIDs,
		FTSPosition:      ftsPos,
	}
}

// SummaryToCandidate converts a core.Summary to a ScoringCandidate.
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
		FTSPosition:      ftsPos,
	}
}

// EpisodeToCandidate converts a core.Episode to a ScoringCandidate.
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
		FTSPosition:      ftsPos,
	}
}

// EventToCandidate converts a core.Event to a ScoringCandidate.
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
		FTSPosition:      ftsPos,
	}
}
