package ai

import (
	"context"
	"math"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	ComputeSimilarity(v1, v2 []float32) float32
	Close() error
}

type EmbedderConfig struct {
	APIKey string
}

func NewEmbedder(ctx context.Context, cfg EmbedderConfig) (Embedder, error) {
	if cfg.APIKey == "" {
		return &NoopEmbedder{}, nil
	}
	return NewGeminiEmbedder(ctx, cfg.APIKey)
}

type GeminiEmbedder struct {
	client *genai.Client
	model  *genai.EmbeddingModel
}

func NewGeminiEmbedder(ctx context.Context, apiKey string) (*GeminiEmbedder, error) {
	if apiKey == "" {
		return nil, nil
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	model := client.EmbeddingModel("text-embedding-004")
	return &GeminiEmbedder{
		client: client,
		model:  model,
	}, nil
}

func (e *GeminiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	res, err := e.model.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, err
	}
	if res == nil || res.Embedding == nil {
		return nil, nil
	}
	return res.Embedding.Values, nil
}

func (e *GeminiEmbedder) ComputeSimilarity(v1, v2 []float32) float32 {
	if len(v1) != len(v2) || len(v1) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := 0; i < len(v1); i++ {
		dotProduct += v1[i] * v2[i]
		normA += v1[i] * v1[i]
		normB += v2[i] * v2[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

func (e *GeminiEmbedder) Close() error {
	return e.client.Close()
}

type NoopEmbedder struct{}

func (n *NoopEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, nil
}

func (n *NoopEmbedder) ComputeSimilarity(v1, v2 []float32) float32 {
	return 1.0 // Always return 1.0 to ensure items are never filtered by semantic checks if noop is used.
}

func (n *NoopEmbedder) Close() error {
	return nil
}

type EmbeddingFilter struct {
	embedder Embedder
}

func NewEmbeddingFilter(ctx context.Context, apiKey string) (*EmbeddingFilter, error) {
	embedder, err := NewEmbedder(ctx, EmbedderConfig{APIKey: apiKey})
	if err != nil {
		return nil, err
	}

	return &EmbeddingFilter{
		embedder: embedder,
	}, nil
}

func (s *EmbeddingFilter) Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error) {
	criteria, err := cfg.LoadSemanticFilters()
	if err != nil {
		return nil, err
	}

	if len(criteria) == 0 {
		// If no criteria, everything is relevant (score 1.0)
		scores := make([]float32, len(items))
		for i := range scores {
			scores[i] = 1.0
		}
		return scores, nil
	}

	// On-the-fly embedding of criteria
	var criteriaEmbeddings [][]float32
	for _, c := range criteria {
		emb, err := s.embedder.Embed(ctx, c)
		if err != nil {
			return nil, err
		}
		criteriaEmbeddings = append(criteriaEmbeddings, emb)
	}

	scores := make([]float32, 0, len(items))
	for _, item := range items {
		// Combine title and description for embedding
		text := item.Title + "\n" + item.Description
		itemEmb, err := s.embedder.Embed(ctx, text)
		if err != nil {
			return nil, err
		}

		maxSim := float32(-1.0)
		for _, filterEmb := range criteriaEmbeddings {
			sim := s.embedder.ComputeSimilarity(itemEmb, filterEmb)
			if sim > maxSim {
				maxSim = sim
			}
		}
		scores = append(scores, maxSim)
	}
	return scores, nil
}

func (s *EmbeddingFilter) Close() error {
	return s.embedder.Close()
}
