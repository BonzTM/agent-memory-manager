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
	endpoint        string
	apiKey          string
	model           string
	reasoningEffort string // low, medium, high — empty means reasoning disabled
	client          *http.Client
	fallback        *HeuristicSummarizer
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

// SetReasoningEffort configures the reasoning_effort parameter for LLM calls.
// Valid values: "low", "medium", "high". Empty disables reasoning parameters.
func (s *LLMSummarizer) SetReasoningEffort(effort string) {
	s.reasoningEffort = strings.ToLower(strings.TrimSpace(effort))
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
	fieldLines.WriteString("- body: the full memory content. MUST go beyond tight_description — include the surrounding context, reasoning, or motivation that makes this memory useful in the future. A body that merely restates tight_description is a defect. Think: what would a future agent need to know to act on this memory without any other context?\n")
	fieldLines.WriteString("- tight_description: a natural-language retrieval phrase (max 100 chars). Must be searchable — write it as if someone would type it to find this memory later. NO file paths, timestamps, or technical IDs. Good: 'CGO and FTS5 flags required for all builds'. Bad: '/home/user/project/build.go line 42'\n")
	fieldLines.WriteString("- confidence: 0.0-1.0 certainty this is durable memory. Calibrate: 0.95 = explicitly stated by user; 0.85-0.94 = strongly implied from context; 0.7-0.84 = reasonable inference; 0.5-0.69 = speculative. Vary your scores.\n")
	fieldLines.WriteString("- importance: 0.0-1.0 future recall value\n")
	if includeSourceEvents {
		fieldLines.WriteString("- source_events: array of event numbers (1-indexed) this memory was derived from\n")
	}

	var rules strings.Builder
	rules.WriteString("FILTERING — apply these rules first, before extracting anything:\n")
	rules.WriteString("- Most events contain nothing worth remembering. Return [] unless you find something genuinely durable.\n")
	rules.WriteString("- Durability check: will this still matter in 30 days? If not, skip it.\n")
	rules.WriteString("- Skip: transient task state, status noise, greetings, file trees, package inventories, raw config/env var dumps, diffs, logs, and JSON blobs.\n")
	rules.WriteString("- Skip: information already obvious from the project's README, AGENTS.md, or standard documentation.\n")
	rules.WriteString("- Tool output (grep results, build logs, test output) should NOT be stored verbatim. Only extract the LESSON if one exists.\n")
	rules.WriteString("- User questions and requests are not memories. Extract from the answers and conclusions, not the questions that prompted them.\n")
	if includeSourceEvents {
		rules.WriteString("- Deduplicate across events: if multiple events express the same thing, produce ONE memory with higher confidence.\n")
	}
	rules.WriteString("\n")
	rules.WriteString("BODY QUALITY — for any memory you do extract:\n")
	rules.WriteString("- Body must be self-contained and useful without context. Include the 'why' and 'so what', not just the 'what'.\n")
	rules.WriteString("- Body MUST go beyond tight_description. A body like 'Uses SQLite' is thin; 'Uses SQLite for local-first deployment — avoids network dependency, supports single-binary distribution' is rich.\n")
	rules.WriteString("\n")
	rules.WriteString("TYPE REFERENCE — use these to pick the right type and shape the body:\n")
	rules.WriteString("- preference: something the user wants or a way they like to work. Body: what they prefer and why.\n")
	rules.WriteString("- decision: a settled architectural or design choice (not brainstorming or proposals). Body: what was chosen and the reasoning behind it. Do not extract decisions from raw tool output, diffs, or logs.\n")
	rules.WriteString("- open_loop: an unresolved question or blocked work that spans sessions (not routine task completion). Body: what is unresolved, why it matters, and what would close the loop.\n")
	rules.WriteString("- constraint: a hard requirement or boundary that limits future choices (not a preference). Body: what is constrained, why, and what it rules out.\n")
	rules.WriteString("- procedure: a non-obvious multi-step workflow with gotchas (not already documented). Body: the steps and tricky parts.\n")
	rules.WriteString("- incident: a notable failure or surprise with a durable lesson (not routine errors). Body: what happened, what was learned, and how to avoid it.\n")
	rules.WriteString("- assumption: something believed but not verified. Body: the assumption, why it is being made, and what would confirm or refute it.\n")
	rules.WriteString("- fact: a stable, verified truth not obvious from code or docs. If unverified, use assumption. Body: the fact and why it matters.\n")
	rules.WriteString("- identity: who someone or something is. Body: the entity and its role or significance.\n")
	rules.WriteString("- relationship: a connection between entities. Body: the entities and the nature of their relationship.\n")
	rules.WriteString("\n")
	rules.WriteString("IMPORTANT: Return [] for most inputs. An empty array is the correct and expected answer most of the time. Only return memories when the content is clearly worth remembering 30 days from now.\n")
	rules.WriteString("Return ONLY the JSON array, no markdown fences or commentary.\n")

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

	return fmt.Sprintf(`Evaluate the following %s for durable memories worth keeping across sessions. Most events contain nothing worth remembering — return an empty JSON array [] unless you find something genuinely durable.

When you do find something worth keeping, return a JSON array of objects with these fields:
%s
%s
Events:
%s`, label, fieldLines.String(), rules.String(), eventsBlock.String())
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Reasoning       *bool         `json:"reasoning,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
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
	if s.reasoningEffort != "" {
		t := true
		body.Reasoning = &t
		body.ReasoningEffort = s.reasoningEffort
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
