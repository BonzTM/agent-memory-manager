package core

import "context"

type Summarizer interface {
	Summarize(ctx context.Context, text string, maxLen int) (string, error)
	ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]MemoryCandidate, error)
	ExtractMemoryCandidateBatch(ctx context.Context, eventContents []string) ([]MemoryCandidate, error)
}

type MemoryCandidate struct {
	Type             MemoryType `json:"type"`
	Subject          string     `json:"subject,omitempty"`
	Body             string     `json:"body"`
	TightDescription string     `json:"tight_description"`
	Confidence       float64    `json:"confidence"`
	Importance       *float64   `json:"importance,omitempty"`
	SourceEventNums  []int      `json:"source_events,omitempty"`
}
