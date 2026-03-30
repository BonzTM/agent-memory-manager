package service

import "strings"

// sanitizeTightDescription returns (s, true) when s is a usable retrieval
// phrase, and (s, false) when it should be replaced with a fallback.
// Rejected: JSON object/array literals, strings containing tool-call marker
// keys ("tool_name", "tool_input"), and strings exceeding maxTightDescLen.
const maxTightDescLen = 200

var tightDescBlockedSubstrings = []string{
	`"tool_name"`,
	`"tool_input"`,
	`tool_name:`,
	`tool_input:`,
}

func sanitizeTightDescription(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return t, false
	}
	// Reject JSON literals.
	if t[0] == '{' || t[0] == '[' {
		return t, false
	}
	// Reject strings containing tool-call markers.
	lower := strings.ToLower(t)
	for _, blocked := range tightDescBlockedSubstrings {
		if strings.Contains(lower, strings.ToLower(blocked)) {
			return t, false
		}
	}
	// Reject excessively long strings — these are almost certainly raw content
	// that leaked through rather than a deliberately crafted retrieval phrase.
	if len(t) > maxTightDescLen {
		return t, false
	}
	return t, true
}

// sanitizeSnippet returns a cleaned version of a raw event-content string
// suitable for embedding in a tight_description snippet list. JSON-looking
// content is replaced with an empty string so callers can skip it.
func sanitizeSnippet(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	if t[0] == '{' || t[0] == '[' {
		return ""
	}
	lower := strings.ToLower(t)
	for _, blocked := range tightDescBlockedSubstrings {
		if strings.Contains(lower, strings.ToLower(blocked)) {
			return ""
		}
	}
	return t
}

func extractTightDescription(content string, maxLen int) string {
	for i, ch := range content {
		if i >= maxLen {
			break
		}
		if ch == '.' || ch == '!' || ch == '?' {
			return content[:i+1]
		}
	}
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen]
}
