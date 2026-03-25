package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// claimPattern maps a trigger phrase to a predicate name for SPO extraction.
type claimPattern struct {
	phrase    string
	predicate string
}

var claimPatterns = []claimPattern{
	{phrase: "decision: ", predicate: "decided"},
	{phrase: " was decided to be ", predicate: "decided"},
	{phrase: "decided to use ", predicate: "decided"},
	{phrase: " depends on ", predicate: "depends_on"},
	{phrase: " runs on ", predicate: "runs_on"},
	{phrase: " requires ", predicate: "requires"},
	{phrase: " supports ", predicate: "supports"},
	{phrase: " prefers ", predicate: "prefers"},
	{phrase: " uses ", predicate: "uses"},
	{phrase: " is ", predicate: "is_a"},
}

// ExtractClaims scans active memories, derives structured claims, and returns
// the number created.
func (s *AMMService) ExtractClaims(ctx context.Context) (int, error) {
	// List recent memories to process.
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  200,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for claim extraction: %w", err)
	}

	if len(memories) == 0 {
		return 0, nil
	}

	created := 0

	for i := range memories {
		mem := &memories[i]

		// Check if claims already exist for this memory.
		existing, err := s.repo.ListClaimsByMemory(ctx, mem.ID)
		if err == nil && len(existing) > 0 {
			continue
		}

		// Try to extract claims from the body.
		claims := extractClaimsFromBody(mem)
		for _, claim := range claims {
			if err := s.repo.InsertClaim(ctx, &claim); err != nil {
				return created, fmt.Errorf("insert claim: %w", err)
			}
			created++
		}
	}

	return created, nil
}

// extractClaimsFromBody applies heuristic patterns to extract SPO triples from a memory's body.
func extractClaimsFromBody(mem *core.Memory) []core.Claim {
	bodyLower := strings.ToLower(mem.Body)
	var claims []core.Claim

	for _, pat := range claimPatterns {
		idx := strings.Index(bodyLower, pat.phrase)
		if idx < 0 {
			continue
		}

		// Subject: text before the pattern phrase, or the memory's Subject field.
		subject := mem.Subject
		if subject == "" {
			subject = strings.TrimSpace(mem.Body[:idx])
			// Take the last sentence fragment as the subject.
			if nlIdx := strings.LastIndexAny(subject, ".!?\n"); nlIdx >= 0 {
				subject = strings.TrimSpace(subject[nlIdx+1:])
			}
		}

		// Object: text after the pattern phrase.
		afterIdx := idx + len(pat.phrase)
		if afterIdx >= len(mem.Body) {
			continue
		}
		object := trimClaimObject(mem.Body[afterIdx:])

		// Truncate subject and object to reasonable length.
		subject = truncate(subject, 100)
		object = truncate(object, 100)

		if subject == "" || object == "" {
			continue
		}

		confidence := mem.Confidence * 0.8
		if confidence <= 0 {
			confidence = 0.4
		}

		claim := core.Claim{
			ID:          generateID("clm_"),
			MemoryID:    mem.ID,
			Predicate:   pat.predicate,
			ObjectValue: object,
			Confidence:  confidence,
			ObservedAt:  mem.ObservedAt,
			Metadata:    map[string]string{"subject": subject},
		}

		claims = append(claims, claim)
	}

	return claims
}

// truncate returns the first maxLen characters of s.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func trimClaimObject(s string) string {
	object := strings.TrimSpace(s)
	objectLower := strings.ToLower(object)

	end := len(object)
	for _, idx := range []int{
		strings.IndexAny(object, ".!?\n;"),
		strings.Index(objectLower, " why:"),
		strings.Index(objectLower, " tradeoff:"),
		strings.Index(objectLower, " because "),
	} {
		if idx >= 0 && idx < end {
			end = idx
		}
	}

	return strings.TrimSpace(object[:end])
}
