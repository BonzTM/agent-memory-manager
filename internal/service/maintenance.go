package service

import (
	"context"
	"fmt"
	"log/slog"
)

func (s *AMMService) PurgeOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	slog.Debug("PurgeOldEvents called", "olderThanDays", olderThanDays)
	deleted, err := s.repo.PurgeOldEvents(ctx, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("purge old events: %w", err)
	}
	slog.Debug("PurgeOldEvents completed successfully", "olderThanDays", olderThanDays, "deleted", deleted)
	return deleted, nil
}

func (s *AMMService) PurgeOldJobs(ctx context.Context, olderThanDays int) (int64, error) {
	slog.Debug("PurgeOldJobs called", "olderThanDays", olderThanDays)
	deleted, err := s.repo.PurgeOldJobs(ctx, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("purge old jobs: %w", err)
	}
	slog.Debug("PurgeOldJobs completed successfully", "olderThanDays", olderThanDays, "deleted", deleted)
	return deleted, nil
}

func (s *AMMService) ExpireRetrievalCache(ctx context.Context) (int64, error) {
	slog.Debug("ExpireRetrievalCache called")
	deleted, err := s.repo.ExpireRetrievalCache(ctx)
	if err != nil {
		return 0, fmt.Errorf("expire retrieval cache: %w", err)
	}
	slog.Debug("ExpireRetrievalCache completed successfully", "deleted", deleted)
	return deleted, nil
}

func (s *AMMService) PurgeOldRelevanceFeedback(ctx context.Context, olderThanDays int) (int64, error) {
	slog.Debug("PurgeOldRelevanceFeedback called", "olderThanDays", olderThanDays)
	deleted, err := s.repo.PurgeOldRelevanceFeedback(ctx, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("purge old relevance feedback: %w", err)
	}
	slog.Debug("PurgeOldRelevanceFeedback completed successfully", "olderThanDays", olderThanDays, "deleted", deleted)
	return deleted, nil
}

func (s *AMMService) VacuumAnalyze(ctx context.Context) error {
	slog.Debug("VacuumAnalyze called")
	if err := s.repo.VacuumAnalyze(ctx); err != nil {
		return fmt.Errorf("vacuum analyze: %w", err)
	}
	slog.Debug("VacuumAnalyze completed successfully")
	return nil
}
