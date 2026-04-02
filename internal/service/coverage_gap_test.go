package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestCoverageGapRunJobs(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-60 * 24 * time.Hour)
	for _, mem := range []*core.Memory{
		{ID: "mem_contra_a", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Subject: "amm", Body: "amm uses sqlite.", TightDescription: "amm uses sqlite", Confidence: 0.9, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_contra_b", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Subject: "amm", Body: "amm uses postgres.", TightDescription: "amm uses postgres", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_decay", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "stale memory", TightDescription: "stale memory", Confidence: 0.9, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: old, UpdatedAt: old},
		{ID: "mem_dup_a", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "duplicate alpha beta gamma", TightDescription: "duplicate alpha beta gamma", Confidence: 0.9, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_dup_b", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "duplicate alpha beta gamma extra", TightDescription: "duplicate alpha beta gamma extra", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.InsertMemory(ctx, mem); err != nil { t.Fatalf("insert %s: %v", mem.ID, err) }
	}
	check := func(kind, key, want string) {
		job, err := svc.RunJob(ctx, kind)
		if err != nil { t.Fatalf("%s: %v", kind, err) }
		if job.Status != "completed" || job.Result[key] != want { t.Fatalf("%s result: %+v", kind, job) }
	}
	check("extract_claims", "claims_created", "2")
	check("detect_contradictions", "contradictions_found", "1")
	check("decay_stale_memory", "memories_decayed", "1")
	check("merge_duplicates", "merges_performed", "1")
}

func TestCoverageGapTextHelpers(t *testing.T) {
	hs := &HeuristicSummarizer{}
	ctx := context.Background()
	if got, _ := hs.Summarize(ctx, "hello", 2); got != "he" { t.Fatalf("summarize short: %q", got) }
	if got, _ := hs.Summarize(ctx, "hello", 0); got != "" { t.Fatalf("summarize zero: %q", got) }
	if c, _ := hs.ExtractMemoryCandidate(ctx, "i prefer go because it requires minimal deps"); len(c) != 1 || c[0].Type != core.MemoryTypePreference { t.Fatalf("extract candidate: %+v", c) }
	if c, _ := hs.ExtractMemoryCandidate(ctx, "no cue here"); c != nil { t.Fatalf("expected no candidate, got %+v", c) }
	if b, _ := hs.ExtractMemoryCandidateBatch(ctx, []string{"i prefer go because it requires less code", "i want tests that requires a real database"}); len(b) != 2 || b[1].SourceEventNums[0] != 2 { t.Fatalf("batch candidates: %+v", b) }
	if got, ok := sanitizeTightDescription(" tool_input: x "); ok || got == "" { t.Fatalf("sanitize tight desc blocked: %q %v", got, ok) }
	if got, ok := sanitizeTightDescription(strings.Repeat("a", maxTightDescLen+1)); ok || got == "" { t.Fatalf("sanitize tight desc long: %q %v", got, ok) }
	if got, ok := sanitizeTightDescription("keep this"); !ok || got != "keep this" { t.Fatalf("sanitize tight desc keep: %q %v", got, ok) }
	if got := sanitizeSnippet(" {\"tool_name\":\"x\"} "); got != "" { t.Fatalf("sanitize snippet blocked: %q", got) }
}

func TestCoverageGapIntelligenceProviders(t *testing.T) {
	ctx := context.Background()
	for _, p := range []core.IntelligenceProvider{NewSummarizerIntelligenceAdapter(nil), NewSummarizerIntelligenceAdapter(dummySummarizer{})} {
		if a, err := p.AnalyzeEvents(ctx, []core.EventContent{{Content: "I prefer Go and need tests"}}); err != nil || a == nil { t.Fatalf("analyze events: %+v %v", a, err) }
		if triage, err := p.TriageEvents(ctx, []core.EventContent{{Content: "todo: fix this"}}); err != nil || len(triage) == 0 { t.Fatalf("triage events: %+v %v", triage, err) }
		if _, err := p.ReviewMemories(ctx, nil); err != nil { t.Fatalf("review memories: %v", err) }
		if comp, err := p.CompressEventBatches(ctx, []core.EventChunk{{Index: 1, Contents: []string{"alpha", "beta"}}}); err != nil || len(comp) != 1 { t.Fatalf("compress batches: %+v %v", comp, err) }
		if topics, err := p.SummarizeTopicBatches(ctx, []core.TopicChunk{{Index: 2, Contents: []string{"topic one", "topic two"}}}); err != nil || len(topics) != 1 { t.Fatalf("topic batches: %+v %v", topics, err) }
		if nar, err := p.ConsolidateNarrative(ctx, []core.EventContent{{Content: "first"}, {Content: "second"}}, nil); err != nil || nar == nil { t.Fatalf("consolidate narrative: %+v %v", nar, err) }
	}
}

func TestCoverageGapLLMIntelligenceFallbacks(t *testing.T) {
	ctx := context.Background()
	if p := NewLLMIntelligenceProviderWithReviewConfig(nil, ReviewConfig{}); p == nil { t.Fatal("expected provider from nil review config") }
	if p := NewLLMIntelligenceProviderWithReviewConfig(NewLLMSummarizer("https://example.com", "key", "model", 0), ReviewConfig{Endpoint: "https://review.example.com", APIKey: "review-key", Model: "review-model"}); p == nil { t.Fatal("expected provider from review config") }
	p := NewLLMIntelligenceProvider(nil, nil)
	if a, err := p.AnalyzeEvents(ctx, nil); err != nil || a == nil || len(a.Memories) != 0 { t.Fatalf("analyze empty: %+v %v", a, err) }
	if a, err := p.AnalyzeEvents(ctx, []core.EventContent{{Content: "i prefer go"}}); err != nil || a == nil { t.Fatalf("analyze fallback: %+v %v", a, err) }
	if triage, err := p.TriageEvents(ctx, []core.EventContent{{Content: "todo: fix this"}}); err != nil || len(triage) == 0 { t.Fatalf("triage fallback: %+v %v", triage, err) }
	if _, err := p.ReviewMemories(ctx, nil); err != nil { t.Fatalf("review empty: %v", err) }
	if topics, err := p.SummarizeTopicBatches(ctx, nil); err != nil || len(topics) != 0 { t.Fatalf("topic empty: %+v %v", topics, err) }
	if comp, err := p.CompressEventBatches(ctx, []core.EventChunk{{Index: 1, Contents: []string{"alpha", "beta"}}}); err != nil || len(comp) != 1 { t.Fatalf("compress fallback: %+v %v", comp, err) }
	if topics, err := p.SummarizeTopicBatches(ctx, []core.TopicChunk{{Index: 2, Contents: []string{"topic one", "topic two"}}}); err != nil || len(topics) != 1 { t.Fatalf("topic fallback: %+v %v", topics, err) }
	if nar, err := p.ConsolidateNarrative(ctx, nil, nil); err != nil || nar == nil { t.Fatalf("narrative empty: %+v %v", nar, err) }
	if nar, err := p.ConsolidateNarrative(ctx, []core.EventContent{{Content: "first"}}, nil); err != nil || nar == nil { t.Fatalf("narrative fallback: %+v %v", nar, err) }
}

func TestCoverageGapLLMPromptsAndParsers(t *testing.T) {
	events := []core.EventContent{{ProjectID: "p", SessionID: "s", Content: "first"}, {Index: 3, Content: "second"}}
	if got := trimLLMJSON("```json\n{\"a\":1}\n```"); got != "{\"a\":1}" { t.Fatalf("trim json: %q", got) }
	if s := buildAnalyzeEventsPrompt(events); !strings.Contains(s, "project_id: p") || !strings.Contains(s, "session_id: s") { t.Fatalf("analyze prompt: %s", s) }
	if s := buildTriageEventsPrompt(events); !strings.Contains(s, "[1] first") || !strings.Contains(s, "[3] second") { t.Fatalf("triage prompt: %s", s) }
	if s := buildCompressEventBatchesPrompt([]core.EventChunk{{Contents: []string{" a ", ""}}}); !strings.Contains(s, "[Chunk 1]") { t.Fatalf("compress prompt: %s", s) }
	if s := buildSummarizeTopicBatchesPrompt([]core.TopicChunk{{Title: "", Contents: []string{"one"}}, {Index: 2, Title: "Custom", Contents: []string{"two"}}}); !strings.Contains(s, "[Topic 1] Topic 1") || !strings.Contains(s, "[Topic 2] Custom") { t.Fatalf("topic prompt: %s", s) }
	if s := buildReviewMemoriesPrompt([]core.MemoryReview{{ID: "m1", Type: "fact", Subject: "sub", Body: "body", TightDescription: "tight", Confidence: 0.5, Importance: 0.1, CreatedAt: time.Now().UTC().Format(time.RFC3339), LastAccessedAt: "", AccessCount: 1}}); !strings.Contains(s, "id=m1") { t.Fatalf("review prompt: %s", s) }
	if s := buildConsolidateNarrativePrompt([]core.EventContent{{Content: "one"}}, nil); !strings.Contains(s, "Existing memories for context:") { t.Fatalf("narrative prompt: %s", s) }
	if _, err := parseAnalysisResult(`{"memories":[],"entities":[],"relationships":[],"event_quality":{"1":"durable","x":"skip"}}`); err != nil { t.Fatalf("parse analysis: %v", err) }
	if tri, err := parseTriageDecisions(`{"1":"skip","2":"high_priority"}`, events); err != nil || tri[1] != core.TriageSkip || tri[3] != core.TriageReflect { t.Fatalf("parse triage: %+v %v", tri, err) }
	if comp, err := parseCompressionResults(`[{"index":0,"body":" ","tight_description":"desc"},{"index":1,"body":" body ","tight_description":"tool_input: bad"},{"index":2,"body":" body ","tight_description":"desc"}]`, []int{2}); err != nil || len(comp) != 1 || comp[0].Body != "body" { t.Fatalf("parse compression: %+v %v", comp, err) }
}

func TestCoverageGapRepairIndexes(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()
	report, err := svc.Repair(ctx, true, "indexes")
	if err != nil { t.Fatalf("repair indexes: %v", err) }
	if report.Fixed != 1 || len(report.Details) == 0 { t.Fatalf("repair report: %+v", report) }
}
