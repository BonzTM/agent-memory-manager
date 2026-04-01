package service

import (
	"regexp"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// classifyRecallIntent inspects a query string and returns a more specific
// recall mode when the query clearly matches a specialized retrieval strategy.
// It returns ("", false) when the query is ambiguous or best served by the
// caller's original mode (typically hybrid).
//
// Intent routing only fires for hybrid mode — ambient is intentionally broad
// and must never be re-routed.
//
// Note: timeline and history modes are intentionally excluded from routing.
// recallTimeline ignores the query text (it lists events by timestamp only),
// and recallHistory passes the query verbatim to FTS, which produces poor
// results for intent-style prompts like "what just happened".
func classifyRecallIntent(query string, entities []string) (core.RecallMode, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "", false
	}

	// Contradiction patterns — strongest signal first.
	if contradictionPattern.MatchString(q) {
		return core.RecallModeContradictions, true
	}

	// Entity-focused queries — only when we actually detected entities.
	if len(entities) > 0 && entityQueryPattern.MatchString(q) {
		return core.RecallModeEntity, true
	}

	return "", false
}

// Patterns are compiled once. Each requires multiple signal words or
// unambiguous phrasing to avoid false-positive routing.

var contradictionPattern = regexp.MustCompile(
	`\b(contradict|contradiction|contradictions|contradicting|conflicts?\s+with|conflicting|inconsisten|disagree)`,
)

var entityQueryPattern = regexp.MustCompile(
	`\b(who\s+is|what\s+is|tell\s+me\s+about|describe|everything\s+(?:about|on)|what\s+do\s+(?:we|you|i)\s+know\s+about)`,
)
