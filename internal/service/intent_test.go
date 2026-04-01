package service

import (
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestClassifyRecallIntent(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		entities []string
		wantMode core.RecallMode
		wantOK   bool
	}{
		// Contradiction patterns
		{
			name:     "contradiction keyword",
			query:    "are there any contradictions about the auth system?",
			wantMode: core.RecallModeContradictions,
			wantOK:   true,
		},
		{
			name:     "conflicting keyword",
			query:    "find conflicting claims about the database",
			wantMode: core.RecallModeContradictions,
			wantOK:   true,
		},
		{
			name:     "conflicts with",
			query:    "what conflicts with our deployment strategy?",
			wantMode: core.RecallModeContradictions,
			wantOK:   true,
		},
		{
			name:     "inconsistent",
			query:    "are there inconsistent facts about the API?",
			wantMode: core.RecallModeContradictions,
			wantOK:   true,
		},

		// Timeline and history queries should NOT be routed (those modes
		// don't use the query text properly).
		{
			name:   "timeline of — not routed",
			query:  "timeline of the auth migration",
			wantOK: false,
		},
		{
			name:   "what happened during — not routed",
			query:  "what happened during the outage last week?",
			wantOK: false,
		},
		{
			name:   "recent activity — not routed",
			query:  "show recent activity",
			wantOK: false,
		},
		{
			name:   "what just happened — not routed",
			query:  "what just happened?",
			wantOK: false,
		},

		// Entity patterns (requires entities)
		{
			name:     "who is with entity",
			query:    "who is Alice?",
			entities: []string{"Alice"},
			wantMode: core.RecallModeEntity,
			wantOK:   true,
		},
		{
			name:     "tell me about with entity",
			query:    "tell me about Redis",
			entities: []string{"Redis"},
			wantMode: core.RecallModeEntity,
			wantOK:   true,
		},
		{
			name:   "who is without entity — no route",
			query:  "who is responsible for this?",
			wantOK: false,
		},
		{
			name:     "what do we know about",
			query:    "what do we know about the Kafka cluster?",
			entities: []string{"Kafka"},
			wantMode: core.RecallModeEntity,
			wantOK:   true,
		},

		// No match — should stay hybrid
		{
			name:   "generic query",
			query:  "how should we handle authentication?",
			wantOK: false,
		},
		{
			name:   "empty query",
			query:  "",
			wantOK: false,
		},
		{
			name:   "simple factual query",
			query:  "what database do we use?",
			wantOK: false,
		},

		// Contradiction takes precedence over entity
		{
			name:     "contradiction beats entity",
			query:    "are there contradictions about Alice?",
			entities: []string{"Alice"},
			wantMode: core.RecallModeContradictions,
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, ok := classifyRecallIntent(tt.query, tt.entities)
			if ok != tt.wantOK {
				t.Errorf("classifyRecallIntent(%q) ok = %v, want %v", tt.query, ok, tt.wantOK)
			}
			if ok && mode != tt.wantMode {
				t.Errorf("classifyRecallIntent(%q) mode = %q, want %q", tt.query, mode, tt.wantMode)
			}
		})
	}
}
