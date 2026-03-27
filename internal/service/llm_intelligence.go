package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type ChatCompleteFunc func(ctx context.Context, prompt string) (string, error)

type LLMIntelligenceProvider struct {
	*LLMSummarizer
	fallback            *HeuristicIntelligenceProvider
	extractChatComplete ChatCompleteFunc
	reviewChatComplete  ChatCompleteFunc
}

func NewLLMIntelligenceProvider(summarizer *LLMSummarizer, reviewChatComplete ChatCompleteFunc) *LLMIntelligenceProvider {
	provider := &LLMIntelligenceProvider{
		LLMSummarizer: summarizer,
		fallback:      NewHeuristicIntelligenceProvider(),
	}
	if summarizer != nil {
		provider.extractChatComplete = summarizer.chatComplete
	}
	if reviewChatComplete != nil {
		provider.reviewChatComplete = reviewChatComplete
	} else {
		provider.reviewChatComplete = provider.extractChatComplete
	}
	return provider
}

func NewLLMIntelligenceProviderWithReviewConfig(summarizer *LLMSummarizer, reviewEndpoint, reviewAPIKey, reviewModel string) *LLMIntelligenceProvider {
	if summarizer == nil {
		return NewLLMIntelligenceProvider(nil, nil)
	}
	if strings.TrimSpace(reviewEndpoint) == "" {
		return NewLLMIntelligenceProvider(summarizer, nil)
	}
	model := strings.TrimSpace(reviewModel)
	if model == "" {
		model = strings.TrimSpace(summarizer.model)
		if model == "" {
			model = "gpt-4o-mini"
		}
	}
	apiKey := strings.TrimSpace(reviewAPIKey)
	if apiKey == "" {
		apiKey = summarizer.apiKey
	}
	reviewSummarizer := NewLLMSummarizer(reviewEndpoint, apiKey, model)
	return NewLLMIntelligenceProvider(summarizer, reviewSummarizer.chatComplete)
}

func (p *LLMIntelligenceProvider) AnalyzeEvents(ctx context.Context, events []core.EventContent) (*core.AnalysisResult, error) {
	if len(events) == 0 {
		return &core.AnalysisResult{
			Memories:      []core.MemoryCandidate{},
			Entities:      []core.EntityCandidate{},
			Relationships: []core.RelationshipCandidate{},
			EventQuality:  map[int]string{},
		}, nil
	}
	if p.extractChatComplete == nil {
		return p.fallback.AnalyzeEvents(ctx, events)
	}

	prompt := buildAnalyzeEventsPrompt(events)
	raw, err := p.extractChatComplete(ctx, prompt)
	if err != nil {
		return p.fallback.AnalyzeEvents(ctx, events)
	}

	parsed, err := parseAnalysisResult(raw)
	if err != nil {
		return p.fallback.AnalyzeEvents(ctx, events)
	}

	return parsed, nil
}

func (p *LLMIntelligenceProvider) TriageEvents(ctx context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	if len(events) == 0 {
		return map[int]core.TriageDecision{}, nil
	}
	if p.extractChatComplete == nil {
		return p.fallback.TriageEvents(ctx, events)
	}

	prompt := buildTriageEventsPrompt(events)
	raw, err := p.extractChatComplete(ctx, prompt)
	if err != nil {
		return p.fallback.TriageEvents(ctx, events)
	}

	parsed, err := parseTriageDecisions(raw, events)
	if err != nil {
		return p.fallback.TriageEvents(ctx, events)
	}

	return parsed, nil
}

func (p *LLMIntelligenceProvider) CompressEventBatches(ctx context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	if len(chunks) == 0 {
		return []core.CompressionResult{}, nil
	}
	if p.extractChatComplete == nil {
		return p.fallback.CompressEventBatches(ctx, chunks)
	}

	prompt := buildCompressEventBatchesPrompt(chunks)
	raw, err := p.extractChatComplete(ctx, prompt)
	if err != nil {
		return p.fallback.CompressEventBatches(ctx, chunks)
	}

	requiredIndexes := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		requiredIndexes = append(requiredIndexes, chunk.Index)
	}
	parsed, err := parseCompressionResults(raw, requiredIndexes)
	if err != nil {
		return p.fallback.CompressEventBatches(ctx, chunks)
	}

	return parsed, nil
}

func (p *LLMIntelligenceProvider) SummarizeTopicBatches(ctx context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	if len(topics) == 0 {
		return []core.CompressionResult{}, nil
	}

	chatComplete := p.reviewChatComplete
	if chatComplete == nil {
		chatComplete = p.extractChatComplete
	}
	if chatComplete == nil {
		return p.fallback.SummarizeTopicBatches(ctx, topics)
	}

	prompt := buildSummarizeTopicBatchesPrompt(topics)
	raw, err := chatComplete(ctx, prompt)
	if err != nil {
		return p.fallback.SummarizeTopicBatches(ctx, topics)
	}

	requiredIndexes := make([]int, 0, len(topics))
	for _, topic := range topics {
		requiredIndexes = append(requiredIndexes, topic.Index)
	}
	parsed, err := parseCompressionResults(raw, requiredIndexes)
	if err != nil {
		return p.fallback.SummarizeTopicBatches(ctx, topics)
	}

	return parsed, nil
}

func (p *LLMIntelligenceProvider) ReviewMemories(ctx context.Context, memories []core.MemoryReview) (*core.ReviewResult, error) {
	if len(memories) == 0 {
		return &core.ReviewResult{}, nil
	}
	if p.reviewChatComplete == nil {
		return &core.ReviewResult{}, nil
	}

	prompt := buildReviewMemoriesPrompt(memories)
	raw, err := p.reviewChatComplete(ctx, prompt)
	if err != nil {
		return &core.ReviewResult{}, nil
	}

	var result core.ReviewResult
	if err := json.Unmarshal([]byte(trimLLMJSON(raw)), &result); err != nil {
		return &core.ReviewResult{}, nil
	}
	if result.Promote == nil {
		result.Promote = []string{}
	}
	if result.Decay == nil {
		result.Decay = []string{}
	}
	if result.Archive == nil {
		result.Archive = []string{}
	}
	if result.Merge == nil {
		result.Merge = []core.MergeSuggestion{}
	}
	if result.Contradictions == nil {
		result.Contradictions = []core.ContradictionPair{}
	}

	return &result, nil
}

func (p *LLMIntelligenceProvider) ConsolidateNarrative(ctx context.Context, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, error) {
	if len(events) == 0 {
		return &core.NarrativeResult{}, nil
	}
	if p.reviewChatComplete == nil {
		return p.fallback.ConsolidateNarrative(ctx, events, existingMemories)
	}

	prompt := buildConsolidateNarrativePrompt(events, existingMemories)
	raw, err := p.reviewChatComplete(ctx, prompt)
	if err != nil {
		return p.fallback.ConsolidateNarrative(ctx, events, existingMemories)
	}

	var result core.NarrativeResult
	if err := json.Unmarshal([]byte(trimLLMJSON(raw)), &result); err != nil {
		return p.fallback.ConsolidateNarrative(ctx, events, existingMemories)
	}
	if result.KeyDecisions == nil {
		result.KeyDecisions = []string{}
	}
	if result.Unresolved == nil {
		result.Unresolved = []string{}
	}

	return &result, nil
}

func parseAnalysisResult(raw string) (*core.AnalysisResult, error) {
	type analysisEnvelope struct {
		Memories      []core.MemoryCandidate       `json:"memories"`
		Entities      []core.EntityCandidate       `json:"entities"`
		Relationships []core.RelationshipCandidate `json:"relationships"`
		EventQuality  map[string]string            `json:"event_quality"`
	}

	var payload analysisEnvelope
	if err := json.Unmarshal([]byte(trimLLMJSON(raw)), &payload); err != nil {
		return nil, err
	}

	eventQuality := make(map[int]string)
	for key, value := range payload.EventQuality {
		idx, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		eventQuality[idx] = value
	}

	if payload.Memories == nil {
		payload.Memories = []core.MemoryCandidate{}
	}
	if payload.Entities == nil {
		payload.Entities = []core.EntityCandidate{}
	}
	if payload.Relationships == nil {
		payload.Relationships = []core.RelationshipCandidate{}
	}

	return &core.AnalysisResult{
		Memories:      payload.Memories,
		Entities:      payload.Entities,
		Relationships: payload.Relationships,
		EventQuality:  eventQuality,
	}, nil
}

func buildAnalyzeEventsPrompt(events []core.EventContent) string {
	eventTexts := make([]string, 0, len(events))
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		eventTexts = append(eventTexts, fmt.Sprintf("project_id: %s\nsession_id: %s\ncontent:\n%s", evt.ProjectID, evt.SessionID, evt.Content))
	}

	base := buildMemoryExtractionPrompt(eventTexts, true)
	return base + `

In addition to memories, also extract entities, relationships, and event quality in the SAME response.

Return a single JSON object with this exact shape:
{
  "memories": [
    {
      "type": "decision",
      "subject": "...",
      "body": "...",
      "tight_description": "...",
      "confidence": 0.9,
      "importance": 0.85,
      "source_events": [1, 3]
    }
  ],
  "entities": [
    {
      "canonical_name": "AMM",
      "type": "project",
      "aliases": ["agent-memory-manager", "amm"],
      "description": "Persistent memory substrate for agents"
    }
  ],
  "relationships": [
    {
      "from_entity": "AMM",
      "to_entity": "SQLite",
      "type": "uses",
      "description": "AMM uses SQLite as its canonical store"
    }
  ],
  "event_quality": {
    "1": "durable",
    "2": "noise",
    "3": "ephemeral"
  }
}

Entity extraction rules:
- Extract entities only when they appear meaningfully in context, not passing mentions
- For each entity provide: canonical_name, type, aliases, description
- Merge entities that are clearly the same thing (e.g., "AMM" and "agent-memory-manager") into one canonical entity with aliases
- Type guidance: people are "person"; languages/frameworks/tools are "technology"; repos/apps are "project"; abstract ideas are "concept"; companies are "org"
- Allowed entity types: person, project, technology, concept, org, service, artifact

Relationship extraction rules:
- Only extract relationships that are explicitly stated or strongly implied
- Relationship format: from_entity -> to_entity with type and brief description
- Use canonical entity names in from_entity and to_entity
- Allowed relationship types: uses, depends-on, contradicts, authored-by, part-of, replaces, extends

Event quality assessment rules:
- For every event in the batch, classify it as exactly one of: durable, ephemeral, noise, context-dependent
- Use the event's 1-based index as the key in event_quality
- durable: lasting knowledge likely useful later
- ephemeral: only relevant to immediate task execution
- noise: no useful signal
- context-dependent: useful only within this specific project/session

Output requirements:
- Return ONLY the JSON object (no markdown fences, prose, or commentary)
- Do not emit keys outside: memories, entities, relationships, event_quality
- If a section has no items, return an empty array/object for that section`
}

func buildTriageEventsPrompt(events []core.EventContent) string {
	var block strings.Builder
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		content := strings.TrimSpace(evt.Content)
		if len(content) > maxEventContentLen {
			content = content[:maxEventContentLen]
		}
		fmt.Fprintf(&block, "[%d] %s\n", index, content)
	}

	return `Classify each event for memory reflection triage.

Allowed labels:
- skip: clear noise or non-durable
- reflect: potentially durable, normal priority
- high_priority: explicit durable preference/decision/constraint

Return ONLY a JSON object where keys are event indexes and values are one label.
Example: {"1":"skip","2":"reflect","3":"high_priority"}

Events:
` + block.String()
}

func parseTriageDecisions(raw string, events []core.EventContent) (map[int]core.TriageDecision, error) {
	var payload map[string]string
	if err := json.Unmarshal([]byte(trimLLMJSON(raw)), &payload); err != nil {
		return nil, err
	}

	decisions := make(map[int]core.TriageDecision, len(events))
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		decisions[index] = core.TriageReflect
	}

	for key, value := range payload {
		idx, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil {
			continue
		}
		switch core.TriageDecision(strings.TrimSpace(value)) {
		case core.TriageSkip, core.TriageReflect, core.TriageHighPriority:
			decisions[idx] = core.TriageDecision(strings.TrimSpace(value))
		}
	}

	return decisions, nil
}

func parseCompressionResults(raw string, requiredIndexes []int) ([]core.CompressionResult, error) {
	var payload []core.CompressionResult
	if err := json.Unmarshal([]byte(trimLLMJSON(raw)), &payload); err != nil {
		return nil, err
	}

	byIndex := make(map[int]core.CompressionResult, len(payload))
	for _, item := range payload {
		if item.Index <= 0 {
			continue
		}
		item.Body = strings.TrimSpace(item.Body)
		if item.Body == "" {
			continue
		}
		if cleaned, ok := sanitizeTightDescription(item.TightDescription); ok {
			item.TightDescription = cleaned
		} else {
			item.TightDescription = ""
		}
		byIndex[item.Index] = item
	}

	results := make([]core.CompressionResult, 0, len(requiredIndexes))
	for _, idx := range requiredIndexes {
		item, ok := byIndex[idx]
		if !ok {
			return nil, fmt.Errorf("missing compression result for index %d", idx)
		}
		results = append(results, item)
	}

	return results, nil
}

func buildReviewMemoriesPrompt(memories []core.MemoryReview) string {
	var memoriesBlock strings.Builder
	for i, memory := range memories {
		age := memoryAgeHint(memory.CreatedAt)
		fmt.Fprintf(&memoriesBlock,
			"[%d] id=%s\n"+
				"type=%s\n"+
				"subject=%s\n"+
				"body=%s\n"+
				"tight_description=%s\n"+
				"confidence=%.3f\n"+
				"importance=%.3f\n"+
				"age=%s\n"+
				"created_at=%s\n"+
				"last_accessed_at=%s\n"+
				"access_count=%d\n\n",
			i+1,
			memory.ID,
			memory.Type,
			memory.Subject,
			memory.Body,
			memory.TightDescription,
			memory.Confidence,
			memory.Importance,
			age,
			memory.CreatedAt,
			memory.LastAccessedAt,
			memory.AccessCount,
		)
	}

	return `You are reviewing durable memories for lifecycle management.

For each memory, decide whether to promote, decay, archive, merge, or mark contradictions.

Promotion guidance:
- Promote memories that encode durable, reusable knowledge and are frequently recalled (high access_count)
- Promote memories with high confidence that are still relevant and broadly useful

Decay/archive guidance:
- Decay memories that seem less relevant, less certain, or lower utility over time
- Archive memories that are stale, superseded by newer information, or purely ephemeral context

Merge/contradiction guidance:
- Merge near-duplicates when one memory can absorb another without losing important nuance
- Report contradictions only when two memories materially conflict

Return a JSON object with exactly these keys:
- promote: array of memory IDs to increase importance
- decay: array of memory IDs to reduce importance
- archive: array of memory IDs to archive
- merge: array of {keep_id, merge_id, reason}
- contradictions: array of {memory_a, memory_b, explanation}

Rules:
- Only reference IDs present in the input
- Do not invent IDs
- If nothing applies for a key, return an empty array for that key
- Return ONLY the JSON object (no markdown fences or commentary)

Memories:
` + memoriesBlock.String()
}

func buildCompressEventBatchesPrompt(chunks []core.EventChunk) string {
	var block strings.Builder
	for i, chunk := range chunks {
		index := chunk.Index
		if index <= 0 {
			index = i + 1
		}
		fmt.Fprintf(&block, "[Chunk %d]\n", index)
		for _, content := range chunk.Contents {
			trimmed := strings.TrimSpace(content)
			if trimmed == "" {
				continue
			}
			fmt.Fprintf(&block, "- %s\n", trimmed)
		}
		block.WriteByte('\n')
	}

	return `Summarize each event chunk into one concise memory summary.

Return ONLY a JSON array of objects with this exact shape:
[
  {"index": 1, "body": "...", "tight_description": "..."}
]

Rules:
- Include exactly one object per input chunk index
- Preserve each chunk index in the output object
- body must be <= 1000 characters and capture key actions/decisions
- tight_description must be <= 100 characters and be retrieval-friendly
- Do not include markdown fences or prose

Chunks:
` + block.String()
}

func buildSummarizeTopicBatchesPrompt(topics []core.TopicChunk) string {
	var block strings.Builder
	for i, topic := range topics {
		index := topic.Index
		if index <= 0 {
			index = i + 1
		}
		title := strings.TrimSpace(topic.Title)
		if title == "" {
			title = fmt.Sprintf("Topic %d", index)
		}
		fmt.Fprintf(&block, "[Topic %d] %s\n", index, title)
		for _, content := range topic.Contents {
			trimmed := strings.TrimSpace(content)
			if trimmed == "" {
				continue
			}
			fmt.Fprintf(&block, "- %s\n", trimmed)
		}
		block.WriteByte('\n')
	}

	return `Merge each topic group into one coherent topic summary.

Return ONLY a JSON array of objects with this exact shape:
[
  {"index": 1, "body": "...", "tight_description": "..."}
]

Rules:
- Include exactly one object per input topic index
- Preserve each topic index in the output object
- body must be <= 2000 characters and synthesize recurring themes
- tight_description must be <= 100 characters and optimized for retrieval
- Do not include markdown fences or prose

Topics:
` + block.String()
}

func buildConsolidateNarrativePrompt(events []core.EventContent, existingMemories []core.MemorySummary) string {
	timeline := buildChronologicalEventsBlock(events)
	mb, _ := json.Marshal(existingMemories)

	return `Consolidate these events into a coherent narrative of what happened.

The episode should tell a clear story (who did what, why decisions were made, what changed, what remains unresolved), not a bullet-list dump.

Return a JSON object with exactly these keys:
- summary: concise chronological summary of the event sequence
- tight_description: retrieval-optimized phrase under 100 chars, written as something an agent would search for later
- episode: object {
    title: short episode title,
    body: coherent narrative body,
    participants: array of people/projects/services/tools involved,
    decisions: array of concrete decisions made,
    outcomes: array of outcomes/results,
    unresolved: array of open questions/open loops
  }
- key_decisions: array of high-impact decisions from the events
- unresolved: array of unresolved questions or next-step uncertainties

Rules:
- Ground outputs in the provided events; do not invent facts
- Keep summary and episode consistent with each other
- If no clear decisions or unresolved items exist, return empty arrays
- Return ONLY the JSON object (no markdown fences or commentary)

Events (chronological):
` + timeline + `

Existing memories for context:
` + string(mb)
}

func memoryAgeHint(createdAt string) string {
	ts := strings.TrimSpace(createdAt)
	if ts == "" {
		return "unknown"
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "unknown"
	}
	if parsed.After(time.Now()) {
		return "0d"
	}
	days := int(time.Since(parsed).Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

func buildChronologicalEventsBlock(events []core.EventContent) string {
	type indexedEvent struct {
		index int
		event core.EventContent
	}

	indexed := make([]indexedEvent, 0, len(events))
	for i, evt := range events {
		idx := evt.Index
		if idx <= 0 {
			idx = i + 1
		}
		indexed = append(indexed, indexedEvent{index: idx, event: evt})
	}

	sort.SliceStable(indexed, func(i, j int) bool {
		return indexed[i].index < indexed[j].index
	})

	var b strings.Builder
	for _, item := range indexed {
		fmt.Fprintf(&b, "[Event %d]\nproject_id: %s\nsession_id: %s\ncontent:\n%s\n\n", item.index, item.event.ProjectID, item.event.SessionID, item.event.Content)
	}

	return b.String()
}

func trimLLMJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}
