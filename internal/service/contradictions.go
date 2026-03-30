package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// DetectContradictions scans extracted claims for conflicting subject-predicate
// pairs and records contradiction memories for conflicts it finds.
func (s *AMMService) DetectContradictions(ctx context.Context) (int, error) {
	slog.Debug("DetectContradictions called")
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

		explanation := fmt.Sprintf(
			"claim %s says %q, claim %s says %q (predicate: %s)",
			a.claim.ID, a.claim.ObjectValue, b.claim.ID, b.claim.ObjectValue, key.predicate,
		)
		created, err := s.persistContradiction(ctx, a.memoryID, b.memoryID, fmt.Sprintf("%s: %s", subject, explanation), []string{"contradiction", "auto-detected"})
		if err != nil {
			return found, err
		}
		if created {
			found++
		}
	}

	return found, nil
}

func (s *AMMService) persistContradiction(ctx context.Context, memAID, memBID, explanation string, tags []string) (bool, error) {
	memA, err := s.repo.GetMemory(ctx, memAID)
	if err != nil {
		return false, fmt.Errorf("get contradiction memory A %s: %w", memAID, err)
	}
	memB, err := s.repo.GetMemory(ctx, memBID)
	if err != nil {
		return false, fmt.Errorf("get contradiction memory B %s: %w", memBID, err)
	}

	subject := strings.TrimSpace(memA.Subject)
	if subject == "" {
		subject = strings.TrimSpace(memB.Subject)
	}
	if subject == "" {
		subject = "memory"
	}

	body := contradictionBody(subject, *memA, *memB, explanation)
	bodyReverse := contradictionBody(subject, *memB, *memA, explanation)
	sourceEventIDs := s.contradictionSourceEventIDs(ctx, explanation)

	existingContradictions, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeContradiction,
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		existingContradictions = nil
	}
	existingBodies := make(map[string]struct{}, len(existingContradictions))
	for _, existing := range existingContradictions {
		existingBodies[existing.Body] = struct{}{}
	}
	if _, ok := existingBodies[body]; ok {
		return false, nil
	}
	if _, ok := existingBodies[bodyReverse]; ok {
		return false, nil
	}

	now := time.Now().UTC()
	inserted := &core.Memory{
			ID:               core.GenerateID("mem_"),
		Type:             core.MemoryTypeContradiction,
		Scope:            core.ScopeGlobal,
		Subject:          subject,
		Body:             body,
		TightDescription: fmt.Sprintf("Contradiction: %s", subject),
		Confidence:       0.7,
		Importance:       0.8,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		SourceEventIDs:   sourceEventIDs,
		Tags:             tags,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.InsertMemory(ctx, inserted); err != nil {
		return false, fmt.Errorf("insert contradiction memory: %w", err)
	}

	if !memA.CreatedAt.Equal(memB.CreatedAt) {
		older := memA
		newer := memB
		if memB.CreatedAt.Before(memA.CreatedAt) {
			older = memB
			newer = memA
		}

		switch older.Status {
		case core.MemoryStatusActive:
			older.Status = core.MemoryStatusSuperseded
			older.SupersededBy = newer.ID
			older.SupersededAt = &now
			older.UpdatedAt = now
			if err := s.repo.UpdateMemory(ctx, older); err != nil {
				return false, fmt.Errorf("supersede older conflicting memory: %w", err)
			}
		case core.MemoryStatusSuperseded:
			slog.Warn("skipping supersession for already superseded memory",
				"memory_id", older.ID,
				"current_superseded_by", older.SupersededBy,
				"newer_memory_id", newer.ID,
			)
		default:
			slog.Warn("skipping supersession for non-active memory",
				"memory_id", older.ID,
				"status", older.Status,
				"newer_memory_id", newer.ID,
			)
		}
	}

	slog.Info("persisted contradiction memory",
		"memory_a", memA.ID,
		"memory_b", memB.ID,
		"subject", subject,
		"explanation", explanation,
		"tags", tags,
	)
	return true, nil
}

func contradictionBody(subject string, memoryA core.Memory, memoryB core.Memory, explanation string) string {
	base := fmt.Sprintf(
		"Conflicting claims about %q: memory %s says %q, memory %s says %q",
		subject, memoryA.ID, memoryA.Body, memoryB.ID, memoryB.Body,
	)
	explanation = strings.TrimSpace(explanation)
	if explanation == "" {
		return base
	}
	return fmt.Sprintf("%s (%s)", base, explanation)
}

var contradictionExplanationClaimIDPattern = regexp.MustCompile(`claim\s+([^\s]+)\s+says`)

func (s *AMMService) contradictionSourceEventIDs(ctx context.Context, explanation string) []string {
	claimMatches := contradictionExplanationClaimIDPattern.FindAllStringSubmatch(explanation, -1)
	if len(claimMatches) == 0 {
		return nil
	}

	sourceEventIDs := make([]string, 0, len(claimMatches))
	for _, match := range claimMatches {
		if len(match) < 2 {
			continue
		}
		claimID := strings.TrimSpace(match[1])
		if claimID == "" {
			continue
		}
		claim, err := s.repo.GetClaim(ctx, claimID)
		if err != nil || claim == nil {
			continue
		}
		sourceEventID := strings.TrimSpace(claim.SourceEventID)
		if sourceEventID == "" {
			continue
		}
		sourceEventIDs = mergeUniqueStrings(sourceEventIDs, []string{sourceEventID})
	}

	return sourceEventIDs
}
