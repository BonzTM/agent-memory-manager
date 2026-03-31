package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// LLMSummarizer uses an OpenAI-compatible chat completion endpoint for
// summarization and memory extraction, with heuristic fallback on failure.
type LLMSummarizer struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
	fallback *HeuristicSummarizer
}

// NewLLMSummarizer constructs an LLM-backed summarizer for the supplied
// endpoint, API key, and model.
func NewLLMSummarizer(endpoint, apiKey, model string) *LLMSummarizer {
	return &LLMSummarizer{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: 30 * time.Second},
		fallback: &HeuristicSummarizer{},
	}
}

// Summarize asks the configured LLM for a concise summary and falls back to the
// heuristic summarizer when the request fails.
func (s *LLMSummarizer) Summarize(ctx context.Context, text string, maxLen int) (string, error) {
	prompt := fmt.Sprintf(
		"Summarize the following text in at most %d characters. "+
			"Return ONLY the summary text, no preamble.\n\n%s", maxLen, text)

	result, err := s.chatComplete(ctx, prompt)
	if err != nil {
		return s.fallback.Summarize(ctx, text, maxLen)
	}

	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result, nil
}

// ExtractMemoryCandidate asks the configured LLM to extract durable memory
// candidates from a single event, falling back heuristically on failure.
func (s *LLMSummarizer) ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]core.MemoryCandidate, error) {
	prompt := buildMemoryExtractionPrompt([]string{eventContent}, false)

	result, err := s.chatComplete(ctx, prompt)
	if err != nil {
		return s.fallback.ExtractMemoryCandidate(ctx, eventContent)
	}

	var candidates []core.MemoryCandidate
	if err := json.Unmarshal([]byte(result), &candidates); err != nil {
		return s.fallback.ExtractMemoryCandidate(ctx, eventContent)
	}
	return candidates, nil
}

const maxEventContentLen = 1200

// ExtractMemoryCandidateBatch asks the configured LLM to extract deduplicated
// memory candidates across a batch of events, falling back heuristically on
// failure.
func (s *LLMSummarizer) ExtractMemoryCandidateBatch(ctx context.Context, eventContents []string) ([]core.MemoryCandidate, error) {
	if len(eventContents) == 0 {
		return nil, nil
	}

	prompt := buildMemoryExtractionPrompt(eventContents, true)

	result, err := s.chatComplete(ctx, prompt)
	if err != nil {
		return s.fallback.ExtractMemoryCandidateBatch(ctx, eventContents)
	}

	var candidates []core.MemoryCandidate
	if err := json.Unmarshal([]byte(result), &candidates); err != nil {
		return s.fallback.ExtractMemoryCandidateBatch(ctx, eventContents)
	}
	return candidates, nil
}

func buildMemoryExtractionPrompt(eventContents []string, includeSourceEvents bool) string {
	var fieldLines strings.Builder
	fieldLines.WriteString("- type: one of \"preference\", \"fact\", \"decision\", \"procedure\", \"constraint\", \"open_loop\", \"identity\", \"relationship\", \"incident\", \"assumption\"\n")
	fieldLines.WriteString("- subject: short noun phrase for entity/topic\n")
	fieldLines.WriteString("- body: full memory text\n")
	fieldLines.WriteString("- tight_description: a natural-language retrieval phrase (max 100 chars). Must be searchable — write it as if someone would type it to find this memory later. NO file paths, timestamps, or technical IDs. Good: 'CGO and FTS5 flags required for all builds'. Bad: '/home/user/project/build.go line 42'\n")
	fieldLines.WriteString("- confidence: 0.0-1.0 certainty this is durable memory\n")
	fieldLines.WriteString("- importance: 0.0-1.0 future recall value\n")
	if includeSourceEvents {
		fieldLines.WriteString("- source_events: array of event numbers (1-indexed) this memory was derived from\n")
	}

	var rules strings.Builder
	rules.WriteString("- Only extract things worth remembering across sessions\n")
	rules.WriteString("- Be highly selective: at most 10-15% of events should yield a memory. A batch of 20 events should produce 2-3 memories at most, not 10. Fewer high-quality memories is always better than many thin ones.\n")
	rules.WriteString("- Durability check: before creating each memory, ask yourself — will this still matter in 30 days? If the answer is 'only if something goes wrong' or 'only during this task', skip it.\n")
	rules.WriteString("- Skip transient task state, status noise, and greetings\n")
	rules.WriteString("- Tool output (grep results, build logs, test output) should NOT be stored verbatim. Instead, if tool output reveals a durable lesson, constraint, or gotcha, extract the LESSON — not the output itself. For example: a build failure showing 'missing import' becomes a constraint memory about dependency requirements, not a copy of the build log.\n")
	rules.WriteString("- Skip file trees, package inventories, raw config/env var dumps, diffs, logs, and JSON blobs\n")
	rules.WriteString("- Before producing a memory, consider: is this information already obviously part of the project's README, AGENTS.md, or standard documentation? If so, skip it — agents can read those files directly. Only extract non-obvious truths, personal preferences, lessons learned, and contextual decisions.\n")
	rules.WriteString("- Memory bodies should be self-contained and useful without context. Include the 'why' and 'so what', not just the 'what'. A body like 'Uses SQLite' is thin; 'Uses SQLite for local-first deployment — avoids network dependency, supports single-binary distribution' is rich.\n")
	if includeSourceEvents {
		rules.WriteString("- Deduplicate across events: if multiple events express the same fact/preference/decision, produce ONE memory with higher confidence\n")
	}
	rules.WriteString("- A \"preference\" is something the user consistently wants\n")
	rules.WriteString("- A \"decision\" is an explicit architectural or design choice with rationale\n")
	rules.WriteString("- Only emit a \"decision\" when the text shows a settled choice, not brainstorming, proposals, or open questions\n")
	rules.WriteString("- For a \"decision\" body, start with \"Decision: <chosen option>\"\n")
	rules.WriteString("- If rationale exists, add a new line \"Why: <brief rationale>\"\n")
	rules.WriteString("- If a key tradeoff or constraint exists, optionally add a new line \"Tradeoff: <brief note>\"\n")
	rules.WriteString("- Do not emit a \"decision\" for raw tool output, diffs, logs, file paths, or grep/code dumps unless the surrounding text explicitly frames a durable choice\n")
	rules.WriteString("- A \"fact\" is a stable truth about the project or domain\n")
	rules.WriteString("- Return [] if nothing is worth remembering\n")
	rules.WriteString("- Return ONLY the JSON array, no markdown fences or commentary\n")

	var eventsBlock strings.Builder
	for i, content := range eventContents {
		if includeSourceEvents && len(content) > maxEventContentLen {
			content = content[:maxEventContentLen]
		}
		fmt.Fprintf(&eventsBlock, "[Event %d]\n%s\n\n", i+1, content)
	}

	label := "event"
	if len(eventContents) != 1 {
		label = fmt.Sprintf("%d conversation events", len(eventContents))
	}

	return fmt.Sprintf(`Extract durable memories from the following %s.

For each memory, return a JSON array of objects with these fields:
%s
Rules:
%s
Events:
%s`, label, fieldLines.String(), rules.String(), eventsBlock.String())
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (s *LLMSummarizer) chatComplete(ctx context.Context, prompt string) (string, error) {
	body := chatRequest{
		Model: s.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(s.endpoint, "/")
	endpoint = strings.TrimSuffix(endpoint, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("LLM API returned status %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content), nil
}
