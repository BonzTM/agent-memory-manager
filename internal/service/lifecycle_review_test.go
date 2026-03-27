package service

import (
	"context"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type lifecycleReviewIntelligenceStub struct {
	reviewFn func([]core.MemoryReview) *core.ReviewResult
	calls    int
}

func (s *lifecycleReviewIntelligenceStub) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (s *lifecycleReviewIntelligenceStub) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return []core.MemoryCandidate{}, nil
}

func (s *lifecycleReviewIntelligenceStub) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return []core.MemoryCandidate{}, nil
}

func (s *lifecycleReviewIntelligenceStub) AnalyzeEvents(context.Context, []core.EventContent) (*core.AnalysisResult, error) {
	return &core.AnalysisResult{}, nil
}

func (s *lifecycleReviewIntelligenceStub) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	decisions := make(map[int]core.TriageDecision, len(events))
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		decisions[index] = core.TriageReflect
	}
	return decisions, nil
}

func (s *lifecycleReviewIntelligenceStub) ReviewMemories(_ context.Context, memories []core.MemoryReview) (*core.ReviewResult, error) {
	s.calls++
	if s.reviewFn == nil {
		return &core.ReviewResult{}, nil
	}
	result := s.reviewFn(memories)
	if result == nil {
		return &core.ReviewResult{}, nil
	}
	return result, nil
}

func (s *lifecycleReviewIntelligenceStub) CompressEventBatches(_ context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, core.CompressionResult{Index: chunk.Index})
	}
	return results, nil
}

func (s *lifecycleReviewIntelligenceStub) SummarizeTopicBatches(_ context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(topics))
	for _, topic := range topics {
		results = append(results, core.CompressionResult{Index: topic.Index})
	}
	return results, nil
}

func (s *lifecycleReviewIntelligenceStub) ConsolidateNarrative(context.Context, []core.EventContent, []core.MemorySummary) (*core.NarrativeResult, error) {
	return &core.NarrativeResult{}, nil
}

func TestLifecycleReview_PromotesMemory(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "lifecycle promote target",
		TightDescription: "lifecycle promote target",
		Importance:       0.5,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub := &lifecycleReviewIntelligenceStub{
		reviewFn: func([]core.MemoryReview) *core.ReviewResult {
			return &core.ReviewResult{Promote: []string{mem.ID}}
		},
	}
	svc.SetIntelligenceProvider(stub)

	affected, err := svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected memory, got %d", affected)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Importance <= 0.5 {
		t.Fatalf("expected promoted importance > 0.5, got %f", updated.Importance)
	}
}

func TestLifecycleReview_ArchivesMemory(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeTodo,
		Body:             "archive this memory",
		TightDescription: "archive this memory",
		Importance:       0.4,
		Confidence:       0.7,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub := &lifecycleReviewIntelligenceStub{
		reviewFn: func([]core.MemoryReview) *core.ReviewResult {
			return &core.ReviewResult{Archive: []string{mem.ID}}
		},
	}
	svc.SetIntelligenceProvider(stub)

	_, err = svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != core.MemoryStatusArchived {
		t.Fatalf("expected archived status, got %s", updated.Status)
	}
}

func TestLifecycleReview_TagsReviewedMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "review metadata tag",
		TightDescription: "review metadata tag",
		Importance:       0.4,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.SetIntelligenceProvider(&lifecycleReviewIntelligenceStub{})

	_, err = svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Metadata[MetaLifecycleReviewedAt]; got == "" {
		t.Fatal("expected lifecycle_reviewed_at to be set")
	}
}

func TestLifecycleReview_NoDoubleReview(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "single review per interval",
		TightDescription: "single review per interval",
		Importance:       0.6,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub := &lifecycleReviewIntelligenceStub{}
	svc.SetIntelligenceProvider(stub)

	_, err = svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("expected one review call after first run, got %d", stub.calls)
	}

	_, err = svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("expected no additional review call on second run, got %d", stub.calls)
	}
}

func TestLifecycleReview_RunJob(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "job-dispatch lifecycle review",
		TightDescription: "job-dispatch lifecycle review",
		Importance:       0.6,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub := &lifecycleReviewIntelligenceStub{}
	svc.SetIntelligenceProvider(stub)

	job, err := svc.RunJob(ctx, "lifecycle_review")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed lifecycle_review job, got %+v", job)
	}
	if job.Result["action"] != "lifecycle_review" {
		t.Fatalf("unexpected lifecycle_review job action: %+v", job.Result)
	}
}

func TestLifecycleReview_ConflictPrecedence(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "conflict precedence A",
		TightDescription: "conflict precedence A",
		Importance:       0.5,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "conflict precedence B",
		TightDescription: "conflict precedence B",
		Importance:       0.5,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	memC, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "conflict precedence C",
		TightDescription: "conflict precedence C",
		Importance:       0.5,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub := &lifecycleReviewIntelligenceStub{
		reviewFn: func([]core.MemoryReview) *core.ReviewResult {
			return &core.ReviewResult{
				Promote: []string{memA.ID},
				Decay:   []string{memA.ID, memB.ID},
				Archive: []string{memA.ID},
				Merge: []core.MergeSuggestion{{
					KeepID:  memC.ID,
					MergeID: memB.ID,
				}},
			}
		},
	}
	svc.SetIntelligenceProvider(stub)

	affected, err := svc.LifecycleReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 2 {
		t.Fatalf("expected exactly two affected memories (archive A + merge B), got %d", affected)
	}

	updatedA, err := repo.GetMemory(ctx, memA.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedB, err := repo.GetMemory(ctx, memB.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedC, err := repo.GetMemory(ctx, memC.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedA.Status != core.MemoryStatusArchived {
		t.Fatalf("expected memory A to be archived, got %s", updatedA.Status)
	}
	if updatedA.Importance != 0.5 {
		t.Fatalf("expected archive precedence to block promote/decay for A; importance=%f", updatedA.Importance)
	}
	if updatedB.Status != core.MemoryStatusSuperseded || updatedB.SupersededBy != memC.ID {
		t.Fatalf("expected memory B to be merged into C, got status=%s superseded_by=%s", updatedB.Status, updatedB.SupersededBy)
	}
	if updatedB.Importance != 0.5 {
		t.Fatalf("expected merge precedence to block decay for B; importance=%f", updatedB.Importance)
	}
	if updatedC.Status != core.MemoryStatusActive {
		t.Fatalf("expected merge keep memory C to stay active, got %s", updatedC.Status)
	}
}
