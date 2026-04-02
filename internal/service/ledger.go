package service

import (
	"strconv"
	"strings"
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
	MetaFallbackCount           = "fallback_count"

	MethodLLM       = "llm"
	MethodHeuristic = "heuristic"

	QualityVerified    = "verified"
	QualityProvisional = "provisional"
	QualityUpgraded    = "upgraded"
)

const maxHeuristicFallbackRetries = 3

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

func getFallbackCount(mem *core.Memory) int {
	if mem == nil {
		return 0
	}
	return fallbackCountFromMetadata(mem.Metadata)
}

func fallbackCountFromMetadata(metadata map[string]string) int {
	raw := strings.TrimSpace(metadata[MetaFallbackCount])
	if raw == "" {
		return 0
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func setFallbackCount(mem *core.Memory, count int) {
	if mem == nil || mem.Metadata == nil && count <= 0 {
		return
	}
	if count <= 0 {
		delete(mem.Metadata, MetaFallbackCount)
		return
	}
	setProcessingMeta(mem, MetaFallbackCount, strconv.Itoa(count))
}

func shouldRetryHeuristicMemory(mem *core.Memory) bool {
	if mem == nil || mem.Status != core.MemoryStatusActive {
		return false
	}
	return getProcessingMeta(mem, MetaExtractionMethod) == MethodHeuristic &&
		getFallbackCount(mem) > 0 &&
		getFallbackCount(mem) < maxHeuristicFallbackRetries
}

func markExtracted(mem *core.Memory, method, model string, retryable bool) {
	if method == "" {
		return
	}
	setProcessingMeta(mem, MetaExtractionMethod, method)
	if method == MethodLLM {
		setProcessingMeta(mem, MetaExtractionQuality, QualityVerified)
		setFallbackCount(mem, 0)
	} else {
		setProcessingMeta(mem, MetaExtractionQuality, QualityProvisional)
		if retryable {
			setFallbackCount(mem, getFallbackCount(mem)+1)
		} else {
			setFallbackCount(mem, 0)
		}
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
