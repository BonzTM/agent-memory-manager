package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// Reflect scans unprocessed events and creates candidate durable memories.
// Returns the number of memories created.
func (s *AMMService) Reflect(ctx context.Context) (int, error) {
	// Determine watermark: find the last completed "reflect" job.
	var afterTime string
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   "reflect",
		Status: "completed",
		Limit:  1,
	})
	if err == nil && len(jobs) > 0 && jobs[0].FinishedAt != nil {
		afterTime = jobs[0].FinishedAt.Format(time.RFC3339Nano)
	}

	// List events to process.
	var events []core.Event
	if afterTime != "" {
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			After: afterTime,
			Limit: 100,
		})
	} else {
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			Limit: 100,
		})
	}
	if err != nil {
		return 0, fmt.Errorf("list events for reflect: %w", err)
	}

	if len(events) == 0 {
		return 0, nil
	}

	// Build a set of event IDs that already have memories referencing them.
	// We check existing memories and collect their source event IDs.
	existingMemories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Limit: 500,
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
	for i := range events {
		evt := &events[i]

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
		candidate := candidates[0]

		// Determine scope from event.
		scope := core.ScopeGlobal
		projectID := ""
		if evt.ProjectID != "" {
			scope = core.ScopeProject
			projectID = evt.ProjectID
		}

		now := time.Now().UTC()
		mem := &core.Memory{
			ID:               generateID("mem_"),
			Type:             candidate.Type,
			Scope:            scope,
			ProjectID:        projectID,
			Body:             candidate.Body,
			TightDescription: candidate.TightDescription,
			Confidence:       candidate.Confidence,
			Importance:       0.5,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			SourceEventIDs:   []string{evt.ID},
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
	}

	// Record a job for watermarking.
	now := time.Now().UTC()
	job := &core.Job{
		ID:         generateID("job_"),
		Kind:       "reflect",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result:     map[string]string{"created": fmt.Sprintf("%d", created)},
		CreatedAt:  now,
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
