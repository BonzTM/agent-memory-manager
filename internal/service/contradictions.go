package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// DetectContradictions scans claims for conflicting subject+predicate pairs with different values.
// Creates contradiction-type memories linking the conflicting items.
// Returns the number of contradictions found.
func (s *AMMService) DetectContradictions(ctx context.Context) (int, error) {
	// List all active memories.
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for contradiction detection: %w", err)
	}

	// claimKey uniquely identifies a subject+predicate pair.
	type claimKey struct {
		subject   string
		predicate string
	}

	// claimEntry pairs a claim with its parent memory for context.
	type claimEntry struct {
		claim    core.Claim
		memoryID string
	}

	claimMap := make(map[claimKey][]claimEntry)

	// Build the map of (predicate, subject) -> claims.
	for _, mem := range memories {
		claims, err := s.repo.ListClaimsByMemory(ctx, mem.ID)
		if err != nil {
			continue
		}
		for _, c := range claims {
			subject := c.SubjectEntityID
			if subject == "" {
				subject = "subject:" + mem.Subject
			}
			key := claimKey{
				subject:   strings.ToLower(strings.TrimSpace(subject)),
				predicate: strings.ToLower(strings.TrimSpace(c.Predicate)),
			}
			claimMap[key] = append(claimMap[key], claimEntry{
				claim:    c,
				memoryID: mem.ID,
			})
		}
	}

	// Load existing contradiction memories so we don't duplicate.
	existingContradictions, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeContradiction,
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		existingContradictions = nil
	}
	existingBodies := make(map[string]bool, len(existingContradictions))
	for _, ec := range existingContradictions {
		existingBodies[ec.Body] = true
	}

	found := 0
	for key, entries := range claimMap {
		if len(entries) < 2 {
			continue
		}

		// Find the first pair with different object values.
		var a, b *claimEntry
		for i := 0; i < len(entries)-1; i++ {
			valA := strings.ToLower(strings.TrimSpace(entries[i].claim.ObjectValue))
			for j := i + 1; j < len(entries); j++ {
				valB := strings.ToLower(strings.TrimSpace(entries[j].claim.ObjectValue))
				if valA != valB {
					a = &entries[i]
					b = &entries[j]
					break
				}
			}
			if a != nil {
				break
			}
		}
		if a == nil {
			continue
		}

		// Determine a human-readable subject for the contradiction.
		subject := key.subject
		if strings.HasPrefix(subject, "subject:") {
			subject = strings.TrimPrefix(subject, "subject:")
		}

		body := fmt.Sprintf(
			"Conflicting claims about %q: claim %s says %q, claim %s says %q",
			subject, a.claim.ID, a.claim.ObjectValue, b.claim.ID, b.claim.ObjectValue,
		)

		// Skip if an identical contradiction already exists.
		if existingBodies[body] {
			continue
		}

		// Also check the reverse ordering to avoid near-duplicates.
		bodyReverse := fmt.Sprintf(
			"Conflicting claims about %q: claim %s says %q, claim %s says %q",
			subject, b.claim.ID, b.claim.ObjectValue, a.claim.ID, a.claim.ObjectValue,
		)
		if existingBodies[bodyReverse] {
			continue
		}

		// Combine source event IDs from both claims.
		var sourceEventIDs []string
		if a.claim.SourceEventID != "" {
			sourceEventIDs = append(sourceEventIDs, a.claim.SourceEventID)
		}
		if b.claim.SourceEventID != "" {
			sourceEventIDs = append(sourceEventIDs, b.claim.SourceEventID)
		}

		now := time.Now().UTC()
		mem := &core.Memory{
			ID:               generateID("mem_"),
			Type:             core.MemoryTypeContradiction,
			Scope:            core.ScopeGlobal,
			Subject:          key.predicate,
			Body:             body,
			TightDescription: fmt.Sprintf("Contradiction: %s has conflicting values for %s", subject, key.predicate),
			Confidence:       0.7,
			Importance:       0.8,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			SourceEventIDs:   sourceEventIDs,
			Tags:             []string{"contradiction", "auto-detected"},
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		if err := s.repo.InsertMemory(ctx, mem); err != nil {
			return found, fmt.Errorf("insert contradiction memory: %w", err)
		}
		existingBodies[body] = true
		found++
	}

	return found, nil
}
