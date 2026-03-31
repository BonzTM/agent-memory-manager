//go:build builtin_embeddings

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"unicode"

	_ "embed"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	builtinEmbeddingProviderName  = "builtin-glove"
	builtinEmbeddingProviderModel = "glove-6B-50d-100k"
	builtinEmbeddingDimension     = 50
)

//go:embed models/glove50d100k.bin.gz
var gloveCompressed []byte

// BuiltinEmbeddingAvailable reports whether the binary was compiled with
// built-in embedding support.
func BuiltinEmbeddingAvailable() bool { return true }

// BuiltinEmbeddingProvider uses pre-trained GloVe word vectors (50d, 100K vocab)
// to generate sentence embeddings via word-vector averaging. Pure Go, no CGo,
// no external API needed.
//
// Quality: ~60-70% of a transformer model (MiniLM). Captures genuine semantic
// similarity ("dog" ≈ "cat", "deploy" ≈ "deployment") but doesn't understand
// word order or sentence structure. Good enough for memory recall ranking where
// FTS and entity overlap handle the precision work.
type BuiltinEmbeddingProvider struct {
	vectors  map[string][]float32 // word -> 50d vector
	dim      int
	loadOnce sync.Once
	loadErr  error
}

var _ core.EmbeddingProvider = (*BuiltinEmbeddingProvider)(nil)

// NewBuiltinEmbeddingProvider returns a GloVe-based embedding provider.
// The model is loaded lazily on first Embed() call.
func NewBuiltinEmbeddingProvider() core.EmbeddingProvider {
	return &BuiltinEmbeddingProvider{
		dim: builtinEmbeddingDimension,
	}
}

func (p *BuiltinEmbeddingProvider) Name() string  { return builtinEmbeddingProviderName }
func (p *BuiltinEmbeddingProvider) Model() string { return builtinEmbeddingProviderModel }

// Embed produces sentence embeddings by averaging word vectors and L2-normalizing.
func (p *BuiltinEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	p.loadOnce.Do(func() {
		p.vectors, p.loadErr = loadGloveVectors(gloveCompressed, p.dim)
	})
	if p.loadErr != nil {
		return nil, fmt.Errorf("load glove vectors: %w", p.loadErr)
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		results[i] = p.embedText(text)
	}
	return results, nil
}

// embedText tokenizes text, averages word vectors, and L2-normalizes.
func (p *BuiltinEmbeddingProvider) embedText(text string) []float32 {
	words := tokenize(text)
	if len(words) == 0 {
		return make([]float32, p.dim)
	}

	// Average word vectors (skip unknown words)
	embedding := make([]float32, p.dim)
	found := 0
	for _, word := range words {
		vec, ok := p.vectors[word]
		if !ok {
			continue
		}
		for j := range embedding {
			embedding[j] += vec[j]
		}
		found++
	}

	if found == 0 {
		return embedding // zero vector
	}

	// Mean pool
	scale := 1.0 / float32(found)
	for j := range embedding {
		embedding[j] *= scale
	}

	// L2 normalize
	l2Normalize(embedding)

	return embedding
}

// tokenize splits text into lowercase words, stripping punctuation.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// l2Normalize normalizes a vector to unit length in-place.
func l2Normalize(vec []float32) {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		s := float32(1.0 / math.Sqrt(norm))
		for i := range vec {
			vec[i] *= s
		}
	}
}

// loadGloveVectors decompresses and parses the embedded uint8-quantized GloVe blob.
//
// Binary format:
//
//	uint32 word_count, uint32 dim
//	float32[dim] dim_min    (quantization min per dimension)
//	float32[dim] dim_range  (quantization range per dimension)
//	For each word:
//	  uint16 word_len, word_bytes[word_len], uint8[dim] quantized_vector
//
// Dequantization: float_val = uint8_val/255.0 * dim_range[d] + dim_min[d]
func loadGloveVectors(compressed []byte, expectedDim int) (map[string][]float32, error) {
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}

	if len(data) < 8 {
		return nil, fmt.Errorf("blob too short: %d bytes", len(data))
	}

	wordCount := int(binary.LittleEndian.Uint32(data[0:4]))
	dim := int(binary.LittleEndian.Uint32(data[4:8]))
	if dim != expectedDim {
		return nil, fmt.Errorf("dimension mismatch: got %d, expected %d", dim, expectedDim)
	}

	offset := 8

	// Read quantization params: dim_min[dim] + dim_range[dim]
	quantParamsSize := dim * 4 * 2 // float32 * dim * 2 arrays
	if len(data) < offset+quantParamsSize {
		return nil, fmt.Errorf("blob too short for quantization params")
	}

	dimMin := make([]float32, dim)
	dimRange := make([]float32, dim)
	for d := 0; d < dim; d++ {
		dimMin[d] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}
	for d := 0; d < dim; d++ {
		dimRange[d] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	vectors := make(map[string][]float32, wordCount)

	for i := 0; i < wordCount; i++ {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("unexpected end of data at word %d", i)
		}
		wordLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if offset+wordLen+dim > len(data) {
			return nil, fmt.Errorf("unexpected end of data at word %d (need %d bytes)", i, wordLen+dim)
		}

		word := string(data[offset : offset+wordLen])
		offset += wordLen

		// Dequantize uint8 -> float32
		vec := make([]float32, dim)
		for d := 0; d < dim; d++ {
			vec[d] = float32(data[offset+d])/255.0*dimRange[d] + dimMin[d]
		}
		offset += dim

		vectors[word] = vec
	}

	return vectors, nil
}
