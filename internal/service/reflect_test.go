package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func TestReflect_UsesAnalyzeEventsAndCreatesRelationships(t *testing.T) {
	ctx := context.Background()
	llm := service.NewLLMSummarizer("http://127.0.0.1:1", "test-key", "test-model")
	svc, repo := testServiceForReprocessWithSummarizer(t, llm)
	concreteSvc, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatal("expected concrete AMMService")
	}

	intel := &reprocessIntelligenceStub{
		isLLM: true,
		analysisResult: &core.AnalysisResult{
			Memories: []core.MemoryCandidate{{
				Type:             core.MemoryTypeDecision,
				Subject:          "database",
				Body:             "API Gateway uses Redis Cache",
				TightDescription: "API Gateway uses Redis Cache",
				Confidence:       0.94,
				SourceEventNums:  []int{1},
			}},
			Entities: []core.EntityCandidate{{
				CanonicalName: "API Gateway",
				Type:          "service",
				Aliases:       []string{"api gateway"},
			}, {
				CanonicalName: "Redis Cache",
				Type:          "technology",
				Aliases:       []string{"redis cache"},
			}},
			Relationships: []core.RelationshipCandidate{{
				FromEntity: "API Gateway",
				ToEntity:   "Redis Cache",
				Type:       "uses",
			}},
		},
	}
	concreteSvc.SetIntelligenceProvider(intel)

	now := time.Now().UTC()
	if err := repo.InsertEvent(ctx, &core.Event{
		ID:           "evt_reflect_llm_rel",
		Kind:         "message_user",
		SourceSystem: "test",
		Content:      "Architecture note: API Gateway uses Redis Cache for throttling",
		PrivacyLevel: core.PrivacyPrivate,
		OccurredAt:   now,
		IngestedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reflect")
	if err != nil {
		t.Fatalf("reflect failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}
	if len(intel.analyzeBatchLens) == 0 || intel.analyzeBatchLens[0] != 1 {
		t.Fatalf("expected AnalyzeEvents to run for one event, got %#v", intel.analyzeBatchLens)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one active reflected memory, got %d", len(mems))
	}

	apiEntities, err := repo.SearchEntities(ctx, "API Gateway", 10)
	if err != nil {
		t.Fatal(err)
	}
	redisEntities, err := repo.SearchEntities(ctx, "Redis Cache", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiEntities) == 0 || len(redisEntities) == 0 {
		t.Fatalf("expected API Gateway and Redis Cache entities, got api=%#v redis=%#v", apiEntities, redisEntities)
	}
	rels, err := repo.ListRelationshipsByEntityIDs(ctx, []string{apiEntities[0].ID, redisEntities[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	foundUses := false
	for _, rel := range rels {
		if rel.FromEntityID == apiEntities[0].ID && rel.ToEntityID == redisEntities[0].ID && rel.RelationshipType == "uses" {
			foundUses = true
			break
		}
	}
	if !foundUses {
		t.Fatalf("expected reflected relationship API Gateway -> Redis Cache uses, got %#v", rels)
	}
}

func TestReflect_FallsBackToSummarizerWhenAnalyzeEventsFails(t *testing.T) {
	ctx := context.Background()
	candidatePayload, err := json.Marshal(map[string]any{
		"memories": []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "deployments",
			Body:             "Josh uses Kubernetes for deployments",
			TightDescription: "Josh uses Kubernetes for deployments",
			Confidence:       0.9,
			SourceEventNums:  []int{1},
		}},
	})
	if err != nil {
		t.Fatalf("marshal candidate payload: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconv.Quote(string(candidatePayload)) + `}}]}`))
	}))
	t.Cleanup(server.Close)

	llm := service.NewLLMSummarizer(server.URL, "test-key", "test-model")
	svc, repo := testServiceForReprocessWithSummarizer(t, llm)
	concreteSvc, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatal("expected concrete AMMService")
	}
	intel := &reprocessIntelligenceStub{isLLM: true, analyzeErr: errors.New("analysis unavailable"), extractFallback: llm}
	concreteSvc.SetIntelligenceProvider(intel)

	now := time.Now().UTC()
	if err := repo.InsertEvent(ctx, &core.Event{
		ID:           "evt_reflect_fallback",
		Kind:         "message_user",
		SourceSystem: "test",
		Content:      "Josh uses Kubernetes for AMM deployments",
		PrivacyLevel: core.PrivacyPrivate,
		OccurredAt:   now,
		IngestedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reflect")
	if err != nil {
		t.Fatalf("reflect failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}
	if len(intel.analyzeBatchLens) == 0 || intel.analyzeBatchLens[0] != 1 {
		t.Fatalf("expected AnalyzeEvents attempt before fallback, got %#v", intel.analyzeBatchLens)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one active reflected memory, got %d", len(mems))
	}
	linked, err := repo.GetMemoryEntities(ctx, mems[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	foundKubernetes := false
	for _, entity := range linked {
		if strings.EqualFold(entity.CanonicalName, "kubernetes") {
			foundKubernetes = true
			break
		}
	}
	if !foundKubernetes {
		t.Fatalf("expected heuristic entity linking fallback to include Kubernetes, got %#v", linked)
	}
}
