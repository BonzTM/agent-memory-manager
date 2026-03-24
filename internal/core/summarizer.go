package core

import "context"

type Summarizer interface {
	Summarize(ctx context.Context, text string, maxLen int) (string, error)
	ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]MemoryCandidate, error)
}

type MemoryCandidate struct {
	Type             MemoryType
	Body             string
	TightDescription string
	Confidence       float64
}
