package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

var ammToolNames = map[string]struct{}{
	"amm_recall":            {},
	"amm_remember":          {},
	"amm_expand":            {},
	"amm_describe":          {},
	"amm_ingest_event":      {},
	"amm_ingest_transcript": {},
	"amm_history":           {},
	"amm_status":            {},
	"amm_jobs_run":          {},
	"amm_share":             {},
	"amm_reset_derived":     {},
	"amm_repair":            {},
	"amm_policy_add":        {},
	"amm_policy_list":       {},
	"amm_policy_remove":     {},
	"amm_get_memory":        {},
	"amm_update_memory":     {},
	"amm_explain_recall":    {},
	"amm_init":              {},
}

// CheckIngestionPolicy returns the effective ingestion mode for event based on
// the highest-priority matching policy.
func (s *AMMService) CheckIngestionPolicy(ctx context.Context, event *core.Event) (string, error) {
	mode, _, err := s.checkIngestionPolicy(ctx, event)
	if err != nil {
		return "", err
	}
	return mode, nil
}

func (s *AMMService) checkIngestionPolicy(ctx context.Context, event *core.Event) (mode string, matched bool, err error) {
	if event == nil {
		return "full", false, nil
	}

	// Check policies in priority order: session, project, agent, source, surface, kind.
	// Kind is last so specific session/project/source overrides beat broad kind rules.
	checks := []struct {
		patternType string
		value       string
	}{
		{"session", event.SessionID},
		{"project", event.ProjectID},
		{"agent", event.AgentID},
		{"source", event.SourceSystem},
		{"surface", event.Surface},
		{"kind", strings.TrimSpace(event.Kind)},
	}

	for _, c := range checks {
		if c.value == "" {
			continue
		}
		policy, err := s.repo.MatchIngestionPolicy(ctx, c.patternType, c.value)
		if err != nil {
			continue // No match for this pattern type; try the next.
		}
		if policy != nil {
			return policy.Mode, true, nil
		}
	}

	return "full", false, nil
}

// ListPolicies returns all configured ingestion policies.
func (s *AMMService) ListPolicies(ctx context.Context) ([]core.IngestionPolicy, error) {
	return s.repo.ListIngestionPolicies(ctx)
}

// AddPolicy assigns IDs and timestamps, stores policy, and returns the saved
// policy.
func (s *AMMService) AddPolicy(ctx context.Context, policy *core.IngestionPolicy) (*core.IngestionPolicy, error) {
	if policy.ID == "" {
		policy.ID = generateID("pol_")
	}
	now := time.Now().UTC()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now

	if err := s.repo.InsertIngestionPolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("insert ingestion policy: %w", err)
	}
	return policy, nil
}

// RemovePolicy deletes the ingestion policy identified by id.
func (s *AMMService) RemovePolicy(ctx context.Context, id string) error {
	if err := s.repo.DeleteIngestionPolicy(ctx, id); err != nil {
		return fmt.Errorf("delete ingestion policy: %w", err)
	}
	return nil
}

// ShouldIngest reports whether event should be stored and whether it should
// trigger memory creation under the effective ingestion policy.
func (s *AMMService) ShouldIngest(ctx context.Context, event *core.Event) (ingest bool, createMemory bool, err error) {
	mode, matched, err := s.checkIngestionPolicy(ctx, event)
	if err != nil {
		return false, false, err
	}

	if !matched && mode == "full" {
		if noiseKind, ok := detectConservativeNoise(event); ok {
			mode = "read_only"
			if event.Metadata == nil {
				event.Metadata = make(map[string]string)
			}
			event.Metadata["ingestion_mode"] = "read_only"
			event.Metadata["ingestion_reason"] = "noise_filter"
			event.Metadata["noise_kind"] = noiseKind
		}
	}

	switch mode {
	case "ignore":
		return false, false, nil
	case "read_only":
		return true, false, nil
	default: // "full"
		return true, true, nil
	}
}

func detectConservativeNoise(event *core.Event) (string, bool) {
	if event == nil {
		return "", false
	}

	if strings.EqualFold(strings.TrimSpace(event.Kind), "tool_call") && containsAMMToolCallName(event.Content) {
		return "amm_self_reference", true
	}

	if strings.EqualFold(strings.TrimSpace(event.Kind), "tool_result") {
		return "tool_result", true
	}

	content := strings.TrimSpace(event.Content)
	if content == "" {
		return "", false
	}

	if isLargeJSONBlob(content) {
		return "json_blob", true
	}
	if isBuildOrTestLogDump(content) {
		return "build_or_test_log", true
	}
	if isListingOrDiffDump(content) {
		return "listing_or_diff_dump", true
	}

	return "", false
}

func containsAMMToolCallName(content string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(content), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	}) {
		if _, ok := ammToolNames[token]; ok {
			return true
		}
	}
	return false
}

func isLargeJSONBlob(content string) bool {
	if len(content) < 1200 {
		return false
	}
	if !(strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[")) {
		return false
	}
	return json.Valid([]byte(content))
}

func isBuildOrTestLogDump(content string) bool {
	lines := nonEmptyLines(content)
	if len(lines) < 6 {
		return false
	}

	signal := 0
	for _, raw := range lines {
		line := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case strings.HasPrefix(line, "=== run"):
			signal++
		case strings.HasPrefix(line, "--- pass:"):
			signal++
		case strings.HasPrefix(line, "--- fail:"):
			signal++
		case strings.HasPrefix(line, "ok\t"):
			signal++
		case strings.HasPrefix(line, "fail\t"):
			signal++
		case strings.HasPrefix(line, "panic:"):
			signal++
		case strings.Contains(line, "build failed"):
			signal++
		case strings.Contains(line, "compilation failed"):
			signal++
		case strings.Contains(line, "exit status"):
			signal++
		case strings.Contains(line, "error:"):
			signal++
		}
	}

	return signal >= 4 && signal*2 >= len(lines)
}

func isListingOrDiffDump(content string) bool {
	lines := nonEmptyLines(content)
	if len(lines) < 8 {
		return false
	}

	matches := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") ||
			strings.HasPrefix(line, "@@") ||
			strings.HasPrefix(line, "+++") ||
			strings.HasPrefix(line, "---") {
			matches++
			continue
		}
		if strings.HasPrefix(line, "total ") ||
			strings.HasPrefix(line, "drwx") ||
			strings.HasPrefix(line, "-rw") {
			matches++
			continue
		}
		if strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../") {
			matches++
			continue
		}
		if isLikelyGrepLine(line) {
			matches++
			continue
		}
	}

	return matches >= 6 && matches*2 >= len(lines)
}

func isLikelyGrepLine(line string) bool {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return false
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return false
	}
	path := parts[0]
	return strings.Contains(path, "/") || strings.Contains(path, "\\") || strings.Contains(path, ".")
}

func nonEmptyLines(content string) []string {
	raw := strings.Split(content, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
