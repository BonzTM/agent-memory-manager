package service

import (
	"context"
	"fmt"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// CheckIntegrity validates cross-record links and summarizes any integrity
// issues it finds.
func (s *AMMService) CheckIntegrity(ctx context.Context) (*core.RepairReport, error) {
	report := &core.RepairReport{}

	// 1. Summary-source links: verify that source_span event_ids exist.
	summarySourceIssues, summarySourceChecked, err := s.checkSummarySourceLinks(ctx)
	if err != nil {
		return report, fmt.Errorf("check summary-source links: %w", err)
	}
	report.Checked += summarySourceChecked
	report.Issues += summarySourceIssues
	if summarySourceIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("summary-source: %d broken links found", summarySourceIssues))
	}

	// 2. Memory-source links: verify source_event_ids, source_summary_ids, source_artifact_ids exist.
	memorySourceIssues, memorySourceChecked, err := s.checkMemorySourceLinks(ctx)
	if err != nil {
		return report, fmt.Errorf("check memory-source links: %w", err)
	}
	report.Checked += memorySourceChecked
	report.Issues += memorySourceIssues
	if memorySourceIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("memory-source: %d broken links found", memorySourceIssues))
	}

	// 3. Supersession chains: verify supersedes/superseded_by targets exist and detect cycles.
	supersessionIssues, supersessionChecked, err := s.checkSupersessionChains(ctx)
	if err != nil {
		return report, fmt.Errorf("check supersession chains: %w", err)
	}
	report.Checked += supersessionChecked
	report.Issues += supersessionIssues
	if supersessionIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("supersession: %d issues found", supersessionIssues))
	}

	// 4. Entity links: verify memory_entities join records point to existing items.
	entityLinkIssues, entityLinkChecked, err := s.checkEntityLinks(ctx)
	if err != nil {
		return report, fmt.Errorf("check entity links: %w", err)
	}
	report.Checked += entityLinkChecked
	report.Issues += entityLinkIssues
	if entityLinkIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("entity-links: %d broken links found", entityLinkIssues))
	}

	// 5. Orphaned summaries: summaries with empty source_span and no summary_edge children.
	orphanedSummaryIssues, orphanedSummaryChecked, err := s.checkOrphanedSummaries(ctx)
	if err != nil {
		return report, fmt.Errorf("check orphaned summaries: %w", err)
	}
	report.Checked += orphanedSummaryChecked
	report.Issues += orphanedSummaryIssues
	if orphanedSummaryIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("orphaned-summaries: %d found", orphanedSummaryIssues))
	}

	// 6. Summary edge integrity: verify parent and child exist.
	edgeIssues, edgeChecked, err := s.checkSummaryEdgeIntegrity(ctx)
	if err != nil {
		return report, fmt.Errorf("check summary edge integrity: %w", err)
	}
	report.Checked += edgeChecked
	report.Issues += edgeIssues
	if edgeIssues > 0 {
		report.Details = append(report.Details, fmt.Sprintf("summary-edges: %d broken edges found", edgeIssues))
	}

	// Include record counts for context.
	evtCount, _ := s.repo.CountEvents(ctx)
	memCount, _ := s.repo.CountMemories(ctx)
	sumCount, _ := s.repo.CountSummaries(ctx)
	epCount, _ := s.repo.CountEpisodes(ctx)
	entCount, _ := s.repo.CountEntities(ctx)
	totalRecords := evtCount + memCount + sumCount + epCount + entCount
	report.Checked += int(totalRecords)
	report.Details = append(report.Details, fmt.Sprintf(
		"records: events=%d memories=%d summaries=%d episodes=%d entities=%d",
		evtCount, memCount, sumCount, epCount, entCount,
	))

	if report.Issues == 0 {
		report.Details = append(report.Details, "no integrity issues found")
	}

	return report, nil
}

// FixLinks repairs link issues that can be corrected through the repository
// interface and reports what changed.
func (s *AMMService) FixLinks(ctx context.Context) (*core.RepairReport, error) {
	report := &core.RepairReport{}

	// Fix broken supersession pointers: clear supersedes/superseded_by that point to non-existent memories.
	supersessionFixed, supersessionChecked, err := s.fixBrokenSupersessionPointers(ctx)
	if err != nil {
		return report, fmt.Errorf("fix supersession pointers: %w", err)
	}
	report.Checked += supersessionChecked
	report.Fixed += supersessionFixed
	if supersessionFixed > 0 {
		report.Details = append(report.Details, fmt.Sprintf("supersession: cleared %d broken pointers", supersessionFixed))
	}

	// Clean up orphaned recall history older than 30 days.
	cleaned, err := s.repo.CleanupRecallHistory(ctx, 30)
	if err != nil {
		return report, fmt.Errorf("cleanup recall history: %w", err)
	}
	if cleaned > 0 {
		report.Fixed += int(cleaned)
		report.Details = append(report.Details, fmt.Sprintf("recall-history: cleaned %d old entries", cleaned))
	}

	// Note: Broken summary_edges and memory_entities cannot be deleted through
	// the current Repository interface. They are reported by CheckIntegrity but
	// require direct DB access or new repository methods to fix.

	if report.Fixed == 0 {
		report.Details = append(report.Details, "no repairs needed")
	}

	return report, nil
}

// checkSummarySourceLinks verifies that source_span event_ids in summaries actually exist.
func (s *AMMService) checkSummarySourceLinks(ctx context.Context) (issues, checked int, err error) {
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, sum := range summaries {
		for _, eid := range sum.SourceSpan.EventIDs {
			checked++
			if _, err := s.repo.GetEvent(ctx, eid); err != nil {
				issues++
			}
		}
	}
	return issues, checked, nil
}

// checkMemorySourceLinks verifies that source IDs in memories actually exist.
func (s *AMMService) checkMemorySourceLinks(ctx context.Context) (issues, checked int, err error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, mem := range memories {
		for _, eid := range mem.SourceEventIDs {
			checked++
			if _, err := s.repo.GetEvent(ctx, eid); err != nil {
				issues++
			}
		}
		for _, sid := range mem.SourceSummaryIDs {
			checked++
			if _, err := s.repo.GetSummary(ctx, sid); err != nil {
				issues++
			}
		}
		for _, aid := range mem.SourceArtifactIDs {
			checked++
			if _, err := s.repo.GetArtifact(ctx, aid); err != nil {
				issues++
			}
		}
	}
	return issues, checked, nil
}

// checkSupersessionChains verifies supersedes/superseded_by targets exist and detects cycles.
func (s *AMMService) checkSupersessionChains(ctx context.Context) (issues, checked int, err error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, mem := range memories {
		// Check supersedes target exists.
		if mem.Supersedes != "" {
			checked++
			if _, err := s.repo.GetMemory(ctx, mem.Supersedes); err != nil {
				issues++
			}
		}

		// Check superseded_by target exists and points back.
		if mem.SupersededBy != "" {
			checked++
			target, err := s.repo.GetMemory(ctx, mem.SupersededBy)
			if err != nil {
				issues++
			} else if target.Supersedes != mem.ID {
				issues++
			}
		}

		// Detect cycles by following supersedes chain.
		if mem.Supersedes != "" {
			visited := map[string]bool{mem.ID: true}
			current := mem.Supersedes
			depth := 0
			for current != "" && depth < 50 {
				if visited[current] {
					issues++
					break
				}
				visited[current] = true
				target, err := s.repo.GetMemory(ctx, current)
				if err != nil {
					break // Already counted as a missing target above.
				}
				current = target.Supersedes
				depth++
			}
			checked++ // One cycle check per memory with supersedes.
		}
	}
	return issues, checked, nil
}

// checkEntityLinks verifies memory_entities join records point to existing memories and entities.
func (s *AMMService) checkEntityLinks(ctx context.Context) (issues, checked int, err error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, mem := range memories {
		entities, err := s.repo.GetMemoryEntities(ctx, mem.ID)
		if err != nil {
			continue
		}
		for _, ent := range entities {
			checked++
			// The entity was returned by GetMemoryEntities, so the memory side is valid.
			// Verify the entity itself still exists.
			if _, err := s.repo.GetEntity(ctx, ent.ID); err != nil {
				issues++
			}
		}
	}
	return issues, checked, nil
}

// checkOrphanedSummaries finds summaries with no source events and no summary edges.
func (s *AMMService) checkOrphanedSummaries(ctx context.Context) (issues, checked int, err error) {
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, sum := range summaries {
		checked++
		hasEventSources := len(sum.SourceSpan.EventIDs) > 0
		hasSummarySources := len(sum.SourceSpan.SummaryIDs) > 0

		if !hasEventSources && !hasSummarySources {
			// Also check if this summary has children via summary_edges.
			children, err := s.repo.GetSummaryChildren(ctx, sum.ID)
			if err != nil || len(children) == 0 {
				issues++
			}
		}
	}
	return issues, checked, nil
}

// checkSummaryEdgeIntegrity verifies that summary edge parent and child references exist.
func (s *AMMService) checkSummaryEdgeIntegrity(ctx context.Context) (issues, checked int, err error) {
	// We check edges by iterating all summaries and inspecting their children.
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, sum := range summaries {
		edges, err := s.repo.GetSummaryChildren(ctx, sum.ID)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			checked++
			switch edge.ChildKind {
			case "summary":
				if _, err := s.repo.GetSummary(ctx, edge.ChildID); err != nil {
					issues++
				}
			case "event":
				if _, err := s.repo.GetEvent(ctx, edge.ChildID); err != nil {
					issues++
				}
			default:
				issues++ // Unknown child kind is an issue.
			}
		}
	}
	return issues, checked, nil
}

// fixBrokenSupersessionPointers clears supersedes/superseded_by fields pointing to non-existent memories.
func (s *AMMService) fixBrokenSupersessionPointers(ctx context.Context) (fixed, checked int, err error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10000})
	if err != nil {
		return 0, 0, err
	}

	for _, mem := range memories {
		needsUpdate := false

		if mem.Supersedes != "" {
			checked++
			if _, err := s.repo.GetMemory(ctx, mem.Supersedes); err != nil {
				mem.Supersedes = ""
				needsUpdate = true
				fixed++
			}
		}

		if mem.SupersededBy != "" {
			checked++
			if _, err := s.repo.GetMemory(ctx, mem.SupersededBy); err != nil {
				mem.SupersededBy = ""
				mem.SupersededAt = nil
				needsUpdate = true
				fixed++
			}
		}

		if needsUpdate {
			if err := s.repo.UpdateMemory(ctx, &mem); err != nil {
				return fixed, checked, fmt.Errorf("update memory %s: %w", mem.ID, err)
			}
		}
	}
	return fixed, checked, nil
}
