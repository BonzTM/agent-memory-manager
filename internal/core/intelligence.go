package core

import "context"

type IntelligenceProvider interface {
	Summarizer

	AnalyzeEvents(ctx context.Context, events []EventContent) (*AnalysisResult, error)
	TriageEvents(ctx context.Context, events []EventContent) (map[int]TriageDecision, error)
	ReviewMemories(ctx context.Context, memories []MemoryReview) (*ReviewResult, error)
	CompressEventBatches(ctx context.Context, chunks []EventChunk) ([]CompressionResult, error)
	SummarizeTopicBatches(ctx context.Context, topics []TopicChunk) ([]CompressionResult, error)
	ConsolidateNarrative(ctx context.Context, events []EventContent, existingMemories []MemorySummary) (*NarrativeResult, error)
}

type TriageDecision string

const (
	TriageSkip         TriageDecision = "skip"
	TriageReflect      TriageDecision = "reflect"
	TriageHighPriority TriageDecision = "high_priority"
)

type EventContent struct {
	Index     int
	Content   string
	ProjectID string
	SessionID string
}

type EventChunk struct {
	Index    int
	Contents []string
}

type TopicChunk struct {
	Index    int
	Contents []string
	Title    string
}

type CompressionResult struct {
	Index            int    `json:"index"`
	Body             string `json:"body"`
	TightDescription string `json:"tight_description"`
}

type AnalysisResult struct {
	Memories      []MemoryCandidate       `json:"memories"`
	Entities      []EntityCandidate       `json:"entities"`
	Relationships []RelationshipCandidate `json:"relationships"`
	EventQuality  map[int]string          `json:"event_quality"`
}

type EntityCandidate struct {
	CanonicalName string   `json:"canonical_name"`
	Type          string   `json:"type"`
	Aliases       []string `json:"aliases,omitempty"`
	Description   string   `json:"description,omitempty"`
}

type RelationshipCandidate struct {
	FromEntity  string `json:"from_entity"`
	ToEntity    string `json:"to_entity"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type MemoryReview struct {
	ID               string
	Type             string
	Subject          string
	Body             string
	TightDescription string
	Confidence       float64
	Importance       float64
	CreatedAt        string
	LastAccessedAt   string
	AccessCount      int
}

type ReviewResult struct {
	Promote        []string            `json:"promote"`
	Decay          []string            `json:"decay"`
	Archive        []string            `json:"archive"`
	Merge          []MergeSuggestion   `json:"merge"`
	Contradictions []ContradictionPair `json:"contradictions"`
}

type MergeSuggestion struct {
	KeepID  string `json:"keep_id"`
	MergeID string `json:"merge_id"`
	Reason  string `json:"reason"`
}

type ContradictionPair struct {
	MemoryA     string `json:"memory_a"`
	MemoryB     string `json:"memory_b"`
	Explanation string `json:"explanation"`
}

type MemorySummary struct {
	Type             string
	Subject          string
	TightDescription string
}

type NarrativeResult struct {
	Summary      string            `json:"summary"`
	TightDesc    string            `json:"tight_description"`
	Episode      *EpisodeCandidate `json:"episode,omitempty"`
	KeyDecisions []string          `json:"key_decisions,omitempty"`
	Unresolved   []string          `json:"unresolved,omitempty"`
}

type EpisodeCandidate struct {
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Participants []string `json:"participants,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Outcomes     []string `json:"outcomes,omitempty"`
	Unresolved   []string `json:"unresolved,omitempty"`
}
