package service

import (
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestPassesIntakeQualityGates(t *testing.T) {
	tests := []struct {
		name          string
		candidate     core.MemoryCandidate
		minConfidence float64
		minImportance float64
		want          bool
	}{
		{
			name: "passes when above both thresholds",
			candidate: core.MemoryCandidate{
				Confidence: 0.8,
				Importance: ptrFloat(0.7),
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0.5,
			minImportance: 0.3,
			want:          true,
		},
		{
			name: "rejected when confidence below threshold",
			candidate: core.MemoryCandidate{
				Confidence: 0.3,
				Importance: ptrFloat(0.7),
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0.5,
			minImportance: 0.3,
			want:          false,
		},
		{
			name: "rejected when importance below threshold",
			candidate: core.MemoryCandidate{
				Confidence: 0.8,
				Importance: ptrFloat(0.1),
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0.5,
			minImportance: 0.3,
			want:          false,
		},
		{
			name: "nil importance uses type default (fact=0.65, above 0.3)",
			candidate: core.MemoryCandidate{
				Confidence: 0.8,
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0.5,
			minImportance: 0.3,
			want:          true,
		},
		{
			name: "zero thresholds pass everything",
			candidate: core.MemoryCandidate{
				Confidence: 0.01,
				Importance: ptrFloat(0.01),
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0,
			minImportance: 0,
			want:          true,
		},
		{
			name: "exact threshold boundary passes",
			candidate: core.MemoryCandidate{
				Confidence: 0.5,
				Importance: ptrFloat(0.3),
				Type:       core.MemoryTypeFact,
			},
			minConfidence: 0.5,
			minImportance: 0.3,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := passesIntakeQualityGates(tt.candidate, tt.minConfidence, tt.minImportance)
			if got != tt.want {
				t.Errorf("passesIntakeQualityGates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCandidateSourcedFromLowQualityEvents(t *testing.T) {
	tests := []struct {
		name         string
		candidate    core.MemoryCandidate
		eventQuality map[int]string
		want         bool
	}{
		{
			name:         "nil event quality skips check",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1, 2}},
			eventQuality: nil,
			want:         false,
		},
		{
			name:         "empty source events skips check",
			candidate:    core.MemoryCandidate{},
			eventQuality: map[int]string{1: "noise"},
			want:         false,
		},
		{
			name:         "all ephemeral source events rejected",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1, 2}},
			eventQuality: map[int]string{1: "ephemeral", 2: "ephemeral"},
			want:         true,
		},
		{
			name:         "all noise source events rejected",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1}},
			eventQuality: map[int]string{1: "noise"},
			want:         true,
		},
		{
			name:         "mixed noise and ephemeral rejected",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1, 2}},
			eventQuality: map[int]string{1: "noise", 2: "ephemeral"},
			want:         true,
		},
		{
			name:         "one durable event keeps candidate",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1, 2}},
			eventQuality: map[int]string{1: "noise", 2: "durable"},
			want:         false,
		},
		{
			name:         "context-dependent keeps candidate",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1}},
			eventQuality: map[int]string{1: "context-dependent"},
			want:         false,
		},
		{
			name:         "missing quality for an event keeps candidate",
			candidate:    core.MemoryCandidate{SourceEventNums: []int{1, 3}},
			eventQuality: map[int]string{1: "noise"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidateSourcedFromLowQualityEvents(tt.candidate, tt.eventQuality)
			if got != tt.want {
				t.Errorf("candidateSourcedFromLowQualityEvents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptrFloat(f float64) *float64 {
	return &f
}
