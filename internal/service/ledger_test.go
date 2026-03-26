package service

import (
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestSetProcessingMeta_NilMap(t *testing.T) {
	mem := &core.Memory{}
	setProcessingMeta(mem, MetaExtractionMethod, MethodLLM)

	if mem.Metadata == nil {
		t.Fatal("expected metadata map to be initialized")
	}
	if got := mem.Metadata[MetaExtractionMethod]; got != MethodLLM {
		t.Fatalf("expected %q, got %q", MethodLLM, got)
	}
}

func TestSetProcessingMeta_ExistingMap(t *testing.T) {
	mem := &core.Memory{Metadata: map[string]string{"existing": "value"}}
	setProcessingMeta(mem, MetaExtractionMethod, MethodHeuristic)

	if got := mem.Metadata["existing"]; got != "value" {
		t.Fatalf("expected existing key to remain, got %q", got)
	}
	if got := mem.Metadata[MetaExtractionMethod]; got != MethodHeuristic {
		t.Fatalf("expected %q, got %q", MethodHeuristic, got)
	}
}

func TestHasProcessingStep(t *testing.T) {
	mem := &core.Memory{Metadata: map[string]string{MetaEntitiesExtracted: "true", "empty": ""}}

	if !hasProcessingStep(mem, MetaEntitiesExtracted) {
		t.Fatal("expected entities_extracted to be considered present")
	}
	if hasProcessingStep(mem, "missing") {
		t.Fatal("expected missing key to be absent")
	}
	if hasProcessingStep(mem, "empty") {
		t.Fatal("expected empty value to be treated as absent")
	}
}

func TestHasLLMProcessingStep(t *testing.T) {
	llm := &core.Memory{Metadata: map[string]string{MetaExtractionMethod: MethodLLM}}
	heuristic := &core.Memory{Metadata: map[string]string{MetaExtractionMethod: MethodHeuristic}}
	entities := &core.Memory{Metadata: map[string]string{MetaEntitiesExtracted: "true"}}

	if !hasLLMProcessingStep(llm, MetaExtractionMethod) {
		t.Fatal("expected extraction_method=llm to be true")
	}
	if hasLLMProcessingStep(heuristic, MetaExtractionMethod) {
		t.Fatal("expected extraction_method=heuristic to be false")
	}
	if !hasLLMProcessingStep(entities, MetaEntitiesExtracted) {
		t.Fatal("expected boolean processing key to be treated as processed")
	}
}

func TestNeedsLLMUpgrade(t *testing.T) {
	heuristic := &core.Memory{Metadata: map[string]string{MetaExtractionMethod: MethodHeuristic}}
	llm := &core.Memory{Metadata: map[string]string{MetaExtractionMethod: MethodLLM}}
	absent := &core.Memory{}

	if !needsLLMUpgrade(heuristic, MetaExtractionMethod) {
		t.Fatal("expected heuristic step to require upgrade")
	}
	if needsLLMUpgrade(llm, MetaExtractionMethod) {
		t.Fatal("expected llm step to not require upgrade")
	}
	if needsLLMUpgrade(absent, MetaExtractionMethod) {
		t.Fatal("expected absent step to not require upgrade")
	}
}

func TestMarkExtracted_LLM(t *testing.T) {
	mem := &core.Memory{}
	markExtracted(mem, MethodLLM, "gpt-4o-mini")

	if got := getProcessingMeta(mem, MetaExtractionMethod); got != MethodLLM {
		t.Fatalf("expected %q, got %q", MethodLLM, got)
	}
	if got := getProcessingMeta(mem, MetaExtractionQuality); got != QualityVerified {
		t.Fatalf("expected %q, got %q", QualityVerified, got)
	}
	if got := getProcessingMeta(mem, MetaExtractedModel); got != "gpt-4o-mini" {
		t.Fatalf("expected extracted model to be set, got %q", got)
	}
	assertRFC3339(t, getProcessingMeta(mem, MetaExtractedAt))
}

func TestMarkExtracted_Heuristic(t *testing.T) {
	mem := &core.Memory{}
	markExtracted(mem, MethodHeuristic, "")

	if got := getProcessingMeta(mem, MetaExtractionMethod); got != MethodHeuristic {
		t.Fatalf("expected %q, got %q", MethodHeuristic, got)
	}
	if got := getProcessingMeta(mem, MetaExtractionQuality); got != QualityProvisional {
		t.Fatalf("expected %q, got %q", QualityProvisional, got)
	}
	if got := getProcessingMeta(mem, MetaExtractedModel); got != "" {
		t.Fatalf("expected empty extracted model, got %q", got)
	}
	assertRFC3339(t, getProcessingMeta(mem, MetaExtractedAt))
}

func TestMarkEmbedded(t *testing.T) {
	mem := &core.Memory{}
	markEmbedded(mem, "text-embedding-3-small")

	assertRFC3339(t, getProcessingMeta(mem, MetaEmbeddedAt))
	if got := getProcessingMeta(mem, MetaEmbeddedModel); got != "text-embedding-3-small" {
		t.Fatalf("expected embedded model to be set, got %q", got)
	}
}

func TestMarkEntitiesExtracted(t *testing.T) {
	mem := &core.Memory{}
	markEntitiesExtracted(mem, MethodHeuristic)

	if got := getProcessingMeta(mem, MetaEntitiesExtracted); got != "true" {
		t.Fatalf("expected entities extracted marker, got %q", got)
	}
	if got := getProcessingMeta(mem, MetaEntitiesExtractedMethod); got != MethodHeuristic {
		t.Fatalf("expected entities extraction method, got %q", got)
	}
}

func TestMarkClaimsExtracted(t *testing.T) {
	mem := &core.Memory{}
	markClaimsExtracted(mem)

	if got := getProcessingMeta(mem, MetaClaimsExtracted); got != "true" {
		t.Fatalf("expected claims extracted marker, got %q", got)
	}
}

func TestMarkLifecycleReviewed(t *testing.T) {
	mem := &core.Memory{}
	markLifecycleReviewed(mem, "gpt-4o-mini")

	assertRFC3339(t, getProcessingMeta(mem, MetaLifecycleReviewedAt))
	if got := getProcessingMeta(mem, MetaLifecycleReviewedModel); got != "gpt-4o-mini" {
		t.Fatalf("expected lifecycle model to be set, got %q", got)
	}
}

func assertRFC3339(t *testing.T, value string) {
	t.Helper()
	if value == "" {
		t.Fatal("expected non-empty RFC3339 timestamp")
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		t.Fatalf("expected valid RFC3339 timestamp, got %q: %v", value, err)
	}
}
