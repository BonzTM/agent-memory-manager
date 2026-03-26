package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const embeddingBatchSize = 64

func buildEmbeddingText(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, "\n\n")
}

func buildMemoryEmbeddingText(memory *core.Memory) string {
	if memory == nil {
		return ""
	}
	return buildEmbeddingText(memory.Subject, memory.Body, memory.TightDescription)
}

func buildSummaryEmbeddingText(summary *core.Summary) string {
	if summary == nil {
		return ""
	}
	return buildEmbeddingText(summary.Title, summary.Body, summary.TightDescription)
}

func (s *AMMService) upsertEmbeddingBestEffort(ctx context.Context, objectID, objectKind, text string) {
	if s.embeddingProvider == nil {
		return
	}
	if strings.TrimSpace(objectID) == "" || strings.TrimSpace(text) == "" {
		return
	}

	vectors, err := s.embeddingProvider.Embed(ctx, []string{text})
	if err != nil {
		slog.Warn("embedding generation failed", "objectKind", objectKind, "objectID", objectID, "provider", s.embeddingProvider.Name(), "model", s.embeddingProvider.Model(), "error", err)
		return
	}
	if len(vectors) != 1 {
		slog.Warn("embedding provider returned unexpected vector count", "objectKind", objectKind, "objectID", objectID, "provider", s.embeddingProvider.Name(), "model", s.embeddingProvider.Model(), "expected", 1, "actual", len(vectors))
		return
	}

	record := &core.EmbeddingRecord{
		ObjectID:   objectID,
		ObjectKind: objectKind,
		Model:      s.embeddingProvider.Model(),
		Vector:     vectors[0],
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.repo.UpsertEmbedding(ctx, record); err != nil {
		slog.Warn("embedding persist failed", "objectKind", objectKind, "objectID", objectID, "model", record.Model, "error", err)
	}
}

func (s *AMMService) upsertMemoryEmbeddingBestEffort(ctx context.Context, memory *core.Memory) {
	if memory == nil {
		return
	}
	s.upsertEmbeddingBestEffort(ctx, memory.ID, "memory", buildMemoryEmbeddingText(memory))
}

func (s *AMMService) upsertSummaryEmbeddingBestEffort(ctx context.Context, summary *core.Summary) {
	if summary == nil {
		return
	}
	s.upsertEmbeddingBestEffort(ctx, summary.ID, "summary", buildSummaryEmbeddingText(summary))
}

func (s *AMMService) rebuildEmbeddings(ctx context.Context, forceAll bool) error {
	if s.embeddingProvider == nil {
		return nil
	}

	model := s.embeddingProvider.Model()

	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 50000})
	if err != nil {
		return fmt.Errorf("list memories for embedding rebuild: %w", err)
	}

	memorySkipped := 0
	memoryEmbedded := 0
	for i := 0; i < len(memories); i += embeddingBatchSize {
		end := i + embeddingBatchSize
		if end > len(memories) {
			end = len(memories)
		}
		batch := memories[i:end]
		texts := make([]string, 0, len(batch))
		ids := make([]string, 0, len(batch))
		for j := range batch {
			text := buildMemoryEmbeddingText(&batch[j])
			if strings.TrimSpace(text) == "" {
				continue
			}
			if !forceAll {
				if _, err := s.repo.GetEmbedding(ctx, batch[j].ID, "memory", model); err == nil {
					memorySkipped++
					continue
				}
			}
			ids = append(ids, batch[j].ID)
			texts = append(texts, text)
		}
		if len(texts) == 0 {
			continue
		}
		vectors, err := s.embeddingProvider.Embed(ctx, texts)
		if err != nil {
			slog.Warn("memory batch embedding generation failed", "provider", s.embeddingProvider.Name(), "model", model, "count", len(texts), "error", err)
			continue
		}
		if len(vectors) != len(ids) {
			slog.Warn("memory batch embedding vector count mismatch", "provider", s.embeddingProvider.Name(), "model", model, "expected", len(ids), "actual", len(vectors))
			continue
		}
		now := time.Now().UTC()
		for j := range ids {
			_ = s.repo.DeleteEmbeddings(ctx, ids[j], "memory", model)
			record := &core.EmbeddingRecord{ObjectID: ids[j], ObjectKind: "memory", Model: model, Vector: vectors[j], CreatedAt: now}
			if err := s.repo.UpsertEmbedding(ctx, record); err != nil {
				slog.Warn("memory embedding rebuild persist failed", "memoryID", ids[j], "model", model, "error", err)
			} else {
				memoryEmbedded++
			}
		}
	}
	slog.Debug("memory embeddings rebuild done", "embedded", memoryEmbedded, "skipped", memorySkipped, "total", len(memories))

	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 50000})
	if err != nil {
		return fmt.Errorf("list summaries for embedding rebuild: %w", err)
	}

	summarySkipped := 0
	summaryEmbedded := 0
	for i := 0; i < len(summaries); i += embeddingBatchSize {
		end := i + embeddingBatchSize
		if end > len(summaries) {
			end = len(summaries)
		}
		batch := summaries[i:end]
		texts := make([]string, 0, len(batch))
		ids := make([]string, 0, len(batch))
		for j := range batch {
			text := buildSummaryEmbeddingText(&batch[j])
			if strings.TrimSpace(text) == "" {
				continue
			}
			if !forceAll {
				if _, err := s.repo.GetEmbedding(ctx, batch[j].ID, "summary", model); err == nil {
					summarySkipped++
					continue
				}
			}
			ids = append(ids, batch[j].ID)
			texts = append(texts, text)
		}
		if len(texts) == 0 {
			continue
		}
		vectors, err := s.embeddingProvider.Embed(ctx, texts)
		if err != nil {
			slog.Warn("summary batch embedding generation failed", "provider", s.embeddingProvider.Name(), "model", model, "count", len(texts), "error", err)
			continue
		}
		if len(vectors) != len(ids) {
			slog.Warn("summary batch embedding vector count mismatch", "provider", s.embeddingProvider.Name(), "model", model, "expected", len(ids), "actual", len(vectors))
			continue
		}
		now := time.Now().UTC()
		for j := range ids {
			_ = s.repo.DeleteEmbeddings(ctx, ids[j], "summary", model)
			record := &core.EmbeddingRecord{ObjectID: ids[j], ObjectKind: "summary", Model: model, Vector: vectors[j], CreatedAt: now}
			if err := s.repo.UpsertEmbedding(ctx, record); err != nil {
				slog.Warn("summary embedding rebuild persist failed", "summaryID", ids[j], "model", model, "error", err)
			} else {
				summaryEmbedded++
			}
		}
	}
	slog.Debug("summary embeddings rebuild done", "embedded", summaryEmbedded, "skipped", summarySkipped, "total", len(summaries))

	return nil
}
