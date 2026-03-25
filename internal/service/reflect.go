package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const reflectBatchSize = 100 // reflectBatchSize controls how many events are claimed per batch.

// Reflect scans unreflected events, extracts durable memories.
// Uses atomic row-level claim to prevent duplicate processing across concurrent jobs.
func (s *AMMService) Reflect(ctx context.Context, jobID string) (int, error) {
	created := 0
	processedCount := 0

	for {
		// Atomically claim unreflected events - this prevents concurrent jobs
		// from processing the same events, even if they start simultaneously.
		events, err := s.repo.ClaimUnreflectedEvents(ctx, reflectBatchSize)
		if err != nil {
			return created, fmt.Errorf("claim events for reflect: %w", err)
		}
		if len(events) == 0 {
			break
		}

		for i := range events {
			evt := &events[i]
			processedCount++

			// Skip events tagged as read_only or ignore by ingestion policy.
			if mode, ok := evt.Metadata["ingestion_mode"]; ok && (mode == "read_only" || mode == "ignore") {
				continue
			}

			candidates, err := s.summarizer.ExtractMemoryCandidate(ctx, evt.Content)
			if err != nil {
				return created, fmt.Errorf("extract memory candidate: %w", err)
			}
			if len(candidates) == 0 {
				continue
			}

			scope := core.ScopeGlobal
			projectID := ""
			if evt.ProjectID != "" {
				scope = core.ScopeProject
				projectID = evt.ProjectID
			}

			for _, rawCandidate := range candidates {
				candidate, ok := prepareMemoryCandidate(rawCandidate)
				if !ok {
					continue
				}

				now := time.Now().UTC()
				importance := importanceForCandidate(candidate)
				candidateMemory := core.Memory{
					Type:             candidate.Type,
					Scope:            scope,
					ProjectID:        projectID,
					Subject:          candidate.Subject,
					Body:             candidate.Body,
					TightDescription: candidate.TightDescription,
					Confidence:       candidate.Confidence,
					Importance:       importance,
					Status:           core.MemoryStatusActive,
					SourceEventIDs:   []string{evt.ID},
				}

				existing, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
					Type:      candidate.Type,
					Scope:     scope,
					ProjectID: projectID,
					Status:    core.MemoryStatusActive,
					Limit:     50000,
				})
				if err != nil {
					return created, fmt.Errorf("list memories for reflect duplicate detection: %w", err)
				}
				activeMemories := make([]*core.Memory, 0, len(existing))
				for i := range existing {
					activeMemories = append(activeMemories, &existing[i])
				}

				if duplicates := findDuplicateActiveMemories(activeMemories, candidateMemory); len(duplicates) > 0 {
					duplicate := selectDuplicateKeeper(duplicates)
					duplicate.SourceEventIDs = mergeUniqueStrings(duplicate.SourceEventIDs, candidateMemory.SourceEventIDs)
					for _, sibling := range duplicates {
						if sibling == nil || sibling.ID == duplicate.ID {
							continue
						}
						duplicate.SourceEventIDs = mergeUniqueStrings(duplicate.SourceEventIDs, sibling.SourceEventIDs)
					}
					if candidateMemory.Confidence > duplicate.Confidence {
						duplicate.Confidence = candidateMemory.Confidence
					}
					if candidateMemory.Importance > duplicate.Importance {
						duplicate.Importance = candidateMemory.Importance
					}
					if shouldUpgradeDuplicateContent(duplicate, candidateMemory, s.extractionMethod()) {
						duplicate.Subject = candidateMemory.Subject
						duplicate.Body = candidateMemory.Body
						duplicate.TightDescription = candidateMemory.TightDescription
					}
					if duplicate.Metadata == nil {
						duplicate.Metadata = make(map[string]string)
					}
					method := s.extractionMethod()
					if duplicate.Metadata["extraction_method"] == "" || method == "llm" {
						duplicate.Metadata["extraction_method"] = method
					}
					duplicate.UpdatedAt = now
					if err := s.repo.UpdateMemory(ctx, duplicate); err != nil {
						return created, fmt.Errorf("update duplicate reflected memory %s: %w", duplicate.ID, err)
					}
					s.upsertMemoryEmbeddingBestEffort(ctx, duplicate)

					for _, sibling := range duplicates {
						if sibling == nil || sibling.ID == duplicate.ID || sibling.Status == core.MemoryStatusSuperseded {
							continue
						}
						supNow := time.Now().UTC()
						sibling.Status = core.MemoryStatusSuperseded
						sibling.SupersededBy = duplicate.ID
						sibling.SupersededAt = &supNow
						sibling.UpdatedAt = supNow
						if err := s.repo.UpdateMemory(ctx, sibling); err != nil {
							return created, fmt.Errorf("supersede duplicate reflected sibling %s: %w", sibling.ID, err)
						}
						s.upsertMemoryEmbeddingBestEffort(ctx, sibling)
					}

					if err := s.linkEntitiesToMemory(ctx, duplicate.ID, evt.Content); err != nil {
						return created, fmt.Errorf("link reflected entities: %w", err)
					}
					continue
				}

				mem := &core.Memory{
					ID:               generateID("mem_"),
					Type:             candidateMemory.Type,
					Scope:            candidateMemory.Scope,
					ProjectID:        candidateMemory.ProjectID,
					Subject:          candidateMemory.Subject,
					Body:             candidateMemory.Body,
					TightDescription: candidateMemory.TightDescription,
					Confidence:       candidateMemory.Confidence,
					Importance:       importance,
					PrivacyLevel:     core.PrivacyPrivate,
					Status:           core.MemoryStatusActive,
					SourceEventIDs:   candidateMemory.SourceEventIDs,
					Metadata:         map[string]string{"extraction_method": s.extractionMethod()},
					CreatedAt:        now,
					UpdatedAt:        now,
				}

				if err := s.repo.InsertMemory(ctx, mem); err != nil {
					return created, fmt.Errorf("insert reflected memory: %w", err)
				}
				s.upsertMemoryEmbeddingBestEffort(ctx, mem)

				if err := s.linkEntitiesToMemory(ctx, mem.ID, evt.Content); err != nil {
					return created, fmt.Errorf("link reflected entities: %w", err)
				}

				created++
			}
		}
	}

	finishedAt := time.Now().UTC()
	result := map[string]string{
		"created":   fmt.Sprintf("%d", created),
		"processed": fmt.Sprintf("%d", processedCount),
	}

	if jobID != "" {
		if job, err := s.repo.GetJob(ctx, jobID); err == nil {
			job.Status = "completed"
			job.FinishedAt = &finishedAt
			job.Result = result
			if err := s.repo.UpdateJob(ctx, job); err != nil {
				return created, fmt.Errorf("update reflect job: %w", err)
			}
		}
	}

	return created, nil
}

// linkEntitiesToMemory extracts entity names from content and links them to the memory.
func (s *AMMService) linkEntitiesToMemory(ctx context.Context, memoryID, content string) error {
	names := ExtractEntities(content)
	for _, name := range names {
		entity, err := s.findOrCreateEntity(ctx, name)
		if err != nil {
			return err
		}
		if entity == nil {
			continue
		}
		if err := s.repo.LinkMemoryEntity(ctx, memoryID, entity.ID, "mentioned"); err != nil {
			return err
		}
	}
	return nil
}

// findOrCreateEntity searches for an existing entity by name (case-insensitive),
// or creates a new entity if no match is found.
func (s *AMMService) findOrCreateEntity(ctx context.Context, canonicalName string) (*core.Entity, error) {
	existing, err := s.repo.SearchEntities(ctx, canonicalName, 50)
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if strings.EqualFold(existing[i].CanonicalName, canonicalName) {
			entity := existing[i]
			return &entity, nil
		}
	}

	now := time.Now().UTC()
	entity := &core.Entity{
		ID:            generateID("ent_"),
		Type:          "topic",
		CanonicalName: canonicalName,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.InsertEntity(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

// extractTightDescription returns the first sentence or first maxLen characters
// of content, whichever is shorter.
func extractTightDescription(content string, maxLen int) string {
	// Try to find the end of the first sentence.
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
