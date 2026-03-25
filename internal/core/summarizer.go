package core

import "context"

// Summarizer extracts summaries and memory candidates from text.
type Summarizer interface {
	// Summarize compresses text to at most maxLen characters.
	Summarize(ctx context.Context, text string, maxLen int) (string, error)
	// ExtractMemoryCandidate extracts one memory candidate from a single event.
	ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]MemoryCandidate, error)
	// ExtractMemoryCandidateBatch extracts memory candidates from multiple events.
	ExtractMemoryCandidateBatch(ctx context.Context, eventContents []string) ([]MemoryCandidate, error)
}

// MemoryCandidate represents a potential durable memory extracted from text.
type MemoryCandidate struct {
	Type             MemoryType `json:"type"`
	Subject          string     `json:"subject,omitempty"`
	Body             string     `json:"body"`
	TightDescription string     `json:"tight_description"`
	Confidence       float64    `json:"confidence"`
	Importance       *float64   `json:"importance,omitempty"`
	SourceEventNums  []int      `json:"source_events,omitempty"`
}
