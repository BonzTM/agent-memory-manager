package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// phraseCue maps a memory type to case-insensitive trigger phrases.
type phraseCue struct {
	memType core.MemoryType
	phrases []string
}

var phraseCues = []phraseCue{
	{
		memType: core.MemoryTypePreference,
		phrases: []string{"prefer", "always use", "don't like", "i like", "i want", "default to", "rather than"},
	},
	{
		memType: core.MemoryTypeDecision,
		phrases: []string{"decided", "we agreed", "going with", "chosen", "settled on", "will use", "switching to"},
	},
	{
		memType: core.MemoryTypeFact,
		phrases: []string{"is a", "works by", "uses", "requires", "depends on", "supports", "runs on"},
	},
	{
		memType: core.MemoryTypeOpenLoop,
		phrases: []string{"todo", "need to", "should look into", "haven't figured out", "remains", "still need", "tbd", "unresolved"},
	},
	{
		memType: core.MemoryTypeConstraint,
		phrases: []string{"must not", "never", "always must", "required to", "cannot", "forbidden"},
	},
}

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

		contentLower := strings.ToLower(evt.Content)

		// Find the first matching cue type.
		var matchedType core.MemoryType
		for _, cue := range phraseCues {
			for _, phrase := range cue.phrases {
				if strings.Contains(contentLower, phrase) {
					matchedType = cue.memType
					break
				}
			}
			if matchedType != "" {
				break
			}
		}

		if matchedType == "" {
			continue
		}

		// Determine scope from event.
		scope := core.ScopeGlobal
		projectID := ""
		if evt.ProjectID != "" {
			scope = core.ScopeProject
			projectID = evt.ProjectID
		}

		// Build body: truncate to 500 chars.
		body := evt.Content
		if len(body) > 500 {
			body = body[:500]
		}

		// Build tight description: first sentence or first 100 chars.
		tight := extractTightDescription(evt.Content, 100)

		now := time.Now().UTC()
		mem := &core.Memory{
			ID:               generateID("mem_"),
			Type:             matchedType,
			Scope:            scope,
			ProjectID:        projectID,
			Body:             body,
			TightDescription: tight,
			Confidence:       0.6,
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
