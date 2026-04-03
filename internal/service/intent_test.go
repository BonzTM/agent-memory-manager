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

		// Open loop / pending work patterns
		{
			name:     "what's pending",
			query:    "what's pending right now?",
			wantMode: core.RecallModeFacts,
			wantOK:   true,
		},
		{
			name:     "what's open",
			query:    "what's open that I should know about?",
			wantMode: core.RecallModeFacts,
			wantOK:   true,
		},
		{
			name:     "open loop",
			query:    "are there any open loop items?",
			wantMode: core.RecallModeFacts,
			wantOK:   true,
		},
		{
			name:     "unresolved",
			query:    "what's still unresolved from last session?",
			wantMode: core.RecallModeFacts,
			wantOK:   true,
		},
		{
			name:     "outstanding issues",
			query:    "any outstanding issues I should know about?",
			wantMode: core.RecallModeFacts,
			wantOK:   true,
		},

		// Decision-focused patterns
		{
			name:     "why did we decide",
			query:    "why did we choose SQLite over Postgres?",
			wantMode: core.RecallModeEpisodes,
			wantOK:   true,
		},
		{
			name:     "what was decided",
			query:    "what was decided about the API versioning?",
			wantMode: core.RecallModeEpisodes,
			wantOK:   true,
		},
		{
			name:     "what did we decide",
			query:    "what did we decide on the auth approach?",
			wantMode: core.RecallModeEpisodes,
			wantOK:   true,
		},
		{
			name:     "decision about",
			query:    "what's the decision about caching?",
			wantMode: core.RecallModeEpisodes,
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
