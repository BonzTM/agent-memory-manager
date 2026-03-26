package service

import (
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	MetaExtractionMethod        = "extraction_method"
	MetaExtractionQuality       = "extraction_quality"
	MetaExtractedAt             = "extracted_at"
	MetaExtractedModel          = "extracted_model"
	MetaEmbeddedAt              = "embedded_at"
	MetaEmbeddedModel           = "embedded_model"
	MetaEntitiesExtracted       = "entities_extracted"
	MetaEntitiesExtractedMethod = "entities_extracted_method"
	MetaClaimsExtracted         = "claims_extracted"
	MetaLifecycleReviewedAt     = "lifecycle_reviewed_at"
	MetaLifecycleReviewedModel  = "lifecycle_reviewed_model"
	MetaNarrativeIncluded       = "narrative_included"

	MethodLLM       = "llm"
	MethodHeuristic = "heuristic"

	QualityVerified    = "verified"
	QualityProvisional = "provisional"
	QualityUpgraded    = "upgraded"
)

func setProcessingMeta(mem *core.Memory, key, value string) {
	if mem == nil {
		return
	}
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]string)
	}
	mem.Metadata[key] = value
}

func getProcessingMeta(mem *core.Memory, key string) string {
	if mem == nil || mem.Metadata == nil {
		return ""
	}
	return mem.Metadata[key]
}

func hasProcessingStep(mem *core.Memory, key string) bool {
	return getProcessingMeta(mem, key) != ""
}

func hasLLMProcessingStep(mem *core.Memory, key string) bool {
	if key == MetaEntitiesExtracted || key == MetaClaimsExtracted || key == MetaNarrativeIncluded {
		return hasProcessingStep(mem, key)
	}
	return getProcessingMeta(mem, key) == MethodLLM
}

func needsLLMUpgrade(mem *core.Memory, methodKey string) bool {
	return getProcessingMeta(mem, methodKey) == MethodHeuristic
}

func markExtracted(mem *core.Memory, method, model string) {
	if method == "" {
		return
	}
	setProcessingMeta(mem, MetaExtractionMethod, method)
	if method == MethodLLM {
		setProcessingMeta(mem, MetaExtractionQuality, QualityVerified)
	} else {
		setProcessingMeta(mem, MetaExtractionQuality, QualityProvisional)
	}
	setProcessingMeta(mem, MetaExtractedAt, time.Now().UTC().Format(time.RFC3339))
	if model != "" {
		setProcessingMeta(mem, MetaExtractedModel, model)
	}
}

func markEmbedded(mem *core.Memory, model string) {
	setProcessingMeta(mem, MetaEmbeddedAt, time.Now().UTC().Format(time.RFC3339))
	if model != "" {
		setProcessingMeta(mem, MetaEmbeddedModel, model)
	}
}

func markEntitiesExtracted(mem *core.Memory, method string) {
	setProcessingMeta(mem, MetaEntitiesExtracted, "true")
	if method != "" {
		setProcessingMeta(mem, MetaEntitiesExtractedMethod, method)
	}
}

func markClaimsExtracted(mem *core.Memory) {
	setProcessingMeta(mem, MetaClaimsExtracted, "true")
}

func markLifecycleReviewed(mem *core.Memory, model string) {
	setProcessingMeta(mem, MetaLifecycleReviewedAt, time.Now().UTC().Format(time.RFC3339))
	if model != "" {
		setProcessingMeta(mem, MetaLifecycleReviewedModel, model)
	}
}
