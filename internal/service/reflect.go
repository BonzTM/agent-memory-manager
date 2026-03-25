package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

const reflectBatchSize = 100

// Reflect scans unprocessed events and creates candidate durable memories.
// Returns the number of memories created.
func (s *AMMService) Reflect(ctx context.Context) (int, error) {
	runStartedAt := time.Now().UTC()

	// Determine watermark: find the last completed "reflect" job.
	var afterRowID int64
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   "reflect",
		Status: "completed",
		Limit:  1,
	})
	if err == nil && len(jobs) > 0 {
		if value := jobs[0].Result["last_event_rowid"]; value != "" {
			parsed, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr == nil && parsed > 0 {
				afterRowID = parsed
			}
		}
	}

	maxRowID, err := s.repo.MaxEventRowID(ctx)
	if err != nil {
		return 0, fmt.Errorf("get max event rowid for reflect: %w", err)
	}
	if maxRowID <= afterRowID {
		return 0, nil
	}

	// Build a set of event IDs that already have memories referencing them.
	// We check existing memories and collect their source event IDs.
	existingMemories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Limit: 50000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for dedup: %w", err)
	}
	reflectedEventIDs := make(map[string]bool)
	for _, mem := range existingMemories {
		for _, eid := range mem.SourceEventIDs {
			reflectedEventIDs[eid] = true
		}
	}

	created := 0
	lastScannedRowID := afterRowID
	for {
		events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
			AfterRowID:  lastScannedRowID,
			BeforeRowID: maxRowID,
			Limit:       reflectBatchSize,
		})
		if err != nil {
			return created, fmt.Errorf("list events for reflect: %w", err)
		}
		if len(events) == 0 {
			break
		}

		for i := range events {
			evt := &events[i]
			lastScannedRowID = evt.RowID

			// Skip events already reflected.
			if reflectedEventIDs[evt.ID] {
				continue
			}

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

			// Determine scope from event.
			scope := core.ScopeGlobal
			projectID := ""
			if evt.ProjectID != "" {
				scope = core.ScopeProject
				projectID = evt.ProjectID
			}

			eventCreated := false
			for _, rawCandidate := range candidates {
				candidate, ok := prepareMemoryCandidate(rawCandidate)
				if !ok {
					continue
				}

				now := time.Now().UTC()
				mem := &core.Memory{
					ID:               generateID("mem_"),
					Type:             candidate.Type,
					Scope:            scope,
					ProjectID:        projectID,
					Subject:          candidate.Subject,
					Body:             candidate.Body,
					TightDescription: candidate.TightDescription,
					Confidence:       candidate.Confidence,
					Importance:       importanceForCandidate(candidate),
					PrivacyLevel:     core.PrivacyPrivate,
					Status:           core.MemoryStatusActive,
					SourceEventIDs:   []string{evt.ID},
					Metadata:         map[string]string{"extraction_method": s.extractionMethod()},
					CreatedAt:        now,
					UpdatedAt:        now,
				}

				if err := s.repo.InsertMemory(ctx, mem); err != nil {
					return created, fmt.Errorf("insert reflected memory: %w", err)
				}

				if err := s.linkEntitiesToMemory(ctx, mem.ID, evt.Content); err != nil {
					return created, fmt.Errorf("link reflected entities: %w", err)
				}

				created++
				eventCreated = true
			}
			if eventCreated {
				reflectedEventIDs[evt.ID] = true
			}
		}
	}

	// Record a job for watermarking.
	finishedAt := time.Now().UTC()
	job := &core.Job{
		ID:         generateID("job_"),
		Kind:       "reflect",
		Status:     "completed",
		StartedAt:  &runStartedAt,
		FinishedAt: &finishedAt,
		Result:     map[string]string{"created": fmt.Sprintf("%d", created), "last_event_rowid": fmt.Sprintf("%d", lastScannedRowID)},
		CreatedAt:  finishedAt,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		// Non-fatal: the memories were already created.
		return created, fmt.Errorf("record reflect job: %w", err)
	}

	return created, nil
}

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
