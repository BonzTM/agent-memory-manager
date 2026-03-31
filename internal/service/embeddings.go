package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const defaultEmbeddingBatchSize = 64

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

func buildEpisodeEmbeddingText(episode *core.Episode) string {
	if episode == nil {
		return ""
	}
	return buildEmbeddingText(episode.Title, episode.Summary, episode.TightDescription)
}

func embeddingHasMagnitude(vec []float32) bool {
	for _, value := range vec {
		if value != 0 {
			return true
		}
	}
	return false
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
	if !embeddingHasMagnitude(vectors[0]) {
		if err := s.repo.DeleteEmbeddings(ctx, objectID, objectKind, s.embeddingProvider.Model()); err != nil {
			slog.Warn("embedding delete failed after empty vector", "objectKind", objectKind, "objectID", objectID, "model", s.embeddingProvider.Model(), "error", err)
		}
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
	batchSize := s.embeddingBatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbeddingBatchSize
	}

	if forceAll {
		return s.rebuildEmbeddingsFull(ctx, model, batchSize)
	}

	memoryEmbedded, err := s.embedMissing(ctx, "memory", model, batchSize)
	if err != nil {
		return fmt.Errorf("embed missing memories: %w", err)
	}

	summaryEmbedded, err := s.embedMissing(ctx, "summary", model, batchSize)
	if err != nil {
		return fmt.Errorf("embed missing summaries: %w", err)
	}

	episodeEmbedded, err := s.embedMissing(ctx, "episode", model, batchSize)
	if err != nil {
		return fmt.Errorf("embed missing episodes: %w", err)
	}

	slog.Debug("embeddings rebuild done", "memories_embedded", memoryEmbedded, "summaries_embedded", summaryEmbedded, "episodes_embedded", episodeEmbedded)
	return nil
}

func (s *AMMService) embedMissing(ctx context.Context, kind, model string, batchSize int) (int, error) {
	var items []embeddable
	switch kind {
	case "memory":
		memories, err := s.repo.ListUnembeddedMemories(ctx, model, 50000)
		if err != nil {
			return 0, err
		}
		items = make([]embeddable, len(memories))
		for i := range memories {
			items[i] = embeddable{id: memories[i].ID, text: buildMemoryEmbeddingText(&memories[i])}
		}
	case "summary":
		summaries, err := s.repo.ListUnembeddedSummaries(ctx, model, 50000)
		if err != nil {
			return 0, err
		}
		items = make([]embeddable, len(summaries))
		for i := range summaries {
			items[i] = embeddable{id: summaries[i].ID, text: buildSummaryEmbeddingText(&summaries[i])}
		}
	case "episode":
		episodes, err := s.repo.ListUnembeddedEpisodes(ctx, model, 50000)
		if err != nil {
			return 0, err
		}
		items = make([]embeddable, len(episodes))
		for i := range episodes {
			items[i] = embeddable{id: episodes[i].ID, text: buildEpisodeEmbeddingText(&episodes[i])}
		}
	}

	if len(items) == 0 {
		return 0, nil
	}

	return s.embedBatch(ctx, items, kind, model, batchSize)
}

type embeddable struct {
	id   string
	text string
}

func (s *AMMService) embedBatch(ctx context.Context, items []embeddable, kind, model string, batchSize int) (int, error) {
	embedded := 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		texts := make([]string, 0, len(batch))
		ids := make([]string, 0, len(batch))
		for _, item := range batch {
			if strings.TrimSpace(item.text) == "" {
				continue
			}
			ids = append(ids, item.id)
			texts = append(texts, item.text)
		}
		if len(texts) == 0 {
			continue
		}
		vectors, err := s.embeddingProvider.Embed(ctx, texts)
		if err != nil {
			slog.Warn("batch embedding generation failed", "kind", kind, "provider", s.embeddingProvider.Name(), "model", model, "count", len(texts), "error", err)
			continue
		}
		if len(vectors) != len(ids) {
			slog.Warn("batch embedding vector count mismatch", "kind", kind, "expected", len(ids), "actual", len(vectors))
			continue
		}
		now := time.Now().UTC()
		for j := range ids {
			if !embeddingHasMagnitude(vectors[j]) {
				if err := s.repo.DeleteEmbeddings(ctx, ids[j], kind, model); err != nil {
					slog.Warn("embedding delete failed after empty batch vector", "kind", kind, "id", ids[j], "model", model, "error", err)
				}
				continue
			}
			record := &core.EmbeddingRecord{ObjectID: ids[j], ObjectKind: kind, Model: model, Vector: vectors[j], CreatedAt: now}
			if err := s.repo.UpsertEmbedding(ctx, record); err != nil {
				slog.Warn("embedding persist failed", "kind", kind, "id", ids[j], "error", err)
			} else {
				embedded++
			}
		}
	}
	return embedded, nil
}

func (s *AMMService) rebuildEmbeddingsFull(ctx context.Context, model string, batchSize int) error {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 50000})
	if err != nil {
		return fmt.Errorf("list memories for full embedding rebuild: %w", err)
	}
	memItems := make([]embeddable, len(memories))
	for i := range memories {
		memItems[i] = embeddable{id: memories[i].ID, text: buildMemoryEmbeddingText(&memories[i])}
	}
	memEmbedded, err := s.embedBatch(ctx, memItems, "memory", model, batchSize)
	if err != nil {
		return err
	}

	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 50000})
	if err != nil {
		return fmt.Errorf("list summaries for full embedding rebuild: %w", err)
	}
	sumItems := make([]embeddable, len(summaries))
	for i := range summaries {
		sumItems[i] = embeddable{id: summaries[i].ID, text: buildSummaryEmbeddingText(&summaries[i])}
	}
	sumEmbedded, err := s.embedBatch(ctx, sumItems, "summary", model, batchSize)
	if err != nil {
		return err
	}

	episodes, err := s.repo.ListEpisodes(ctx, core.ListEpisodesOptions{Limit: 50000})
	if err != nil {
		return fmt.Errorf("list episodes for full embedding rebuild: %w", err)
	}
	episodeItems := make([]embeddable, len(episodes))
	for i := range episodes {
		episodeItems[i] = embeddable{id: episodes[i].ID, text: buildEpisodeEmbeddingText(&episodes[i])}
	}
	episodeEmbedded, err := s.embedBatch(ctx, episodeItems, "episode", model, batchSize)
	if err != nil {
		return err
	}

	slog.Debug("full embeddings rebuild done", "memories_embedded", memEmbedded, "summaries_embedded", sumEmbedded, "episodes_embedded", episodeEmbedded)
	return nil
}
