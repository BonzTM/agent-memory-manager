package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultNoopEmbeddingProviderName  = "noop-local"
	defaultNoopEmbeddingProviderModel = "noop"
)

type NoopEmbeddingProvider struct {
	name  string
	model string
}

type APIEmbeddingProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

var _ core.EmbeddingProvider = (*NoopEmbeddingProvider)(nil)
var _ core.EmbeddingProvider = (*APIEmbeddingProvider)(nil)

// NewAPIEmbeddingProvider creates an embedding provider backed by an
// OpenAI-compatible API. Pass 0 for timeout to use the default (30s).
func NewAPIEmbeddingProvider(endpoint, apiKey, model string, timeout time.Duration) *APIEmbeddingProvider {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &APIEmbeddingProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: timeout},
	}
}

func NewNoopEmbeddingProvider(name, model string) *NoopEmbeddingProvider {
	if name == "" {
		name = defaultNoopEmbeddingProviderName
	}
	if model == "" {
		model = defaultNoopEmbeddingProviderModel
	}
	return &NoopEmbeddingProvider{name: name, model: model}
}

func (p *NoopEmbeddingProvider) Name() string {
	return p.name
}

func (p *NoopEmbeddingProvider) Model() string {
	return p.model
}

func (p *NoopEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{}
	}
	return vectors, nil
}

func (p *APIEmbeddingProvider) Name() string {
	return "api"
}

func (p *APIEmbeddingProvider) Model() string {
	return p.model
}

type apiEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type apiEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (p *APIEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	payload, err := json.Marshal(apiEmbeddingRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.endpoint, "/")
	endpoint = strings.TrimSuffix(endpoint, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("embedding API returned status %d", resp.StatusCode)
	}

	var embedResp apiEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	sort.Slice(embedResp.Data, func(i, j int) bool {
		return embedResp.Data[i].Index < embedResp.Data[j].Index
	})

	vectors := make([][]float32, len(texts))
	seen := make([]bool, len(texts))
	for _, item := range embedResp.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("invalid embedding index %d", item.Index)
		}
		vector := make([]float32, len(item.Embedding))
		for i, value := range item.Embedding {
			vector[i] = float32(value)
		}
		vectors[item.Index] = vector
		seen[item.Index] = true
	}

	for i := range seen {
		if !seen[i] {
			return nil, fmt.Errorf("missing embedding for input index %d", i)
		}
	}

	return vectors, nil
}
