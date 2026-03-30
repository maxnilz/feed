package ai

import (
	"context"
	"testing"

	"github.com/maxnilz/feed/logging"
)

type stubFilter struct {
	scores    []float32
	evaluateN int
}

func (s *stubFilter) Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error) {
	s.evaluateN++
	out := make([]float32, len(s.scores))
	copy(out, s.scores)
	return out, nil
}

func (s *stubFilter) Close() error {
	return nil
}

func TestNewFilterUsesConfiguredGeminiModel(t *testing.T) {
	filter, err := NewFilter(context.Background(), logging.DefaultLogger, FilterConfig{
		Type:         FilterTypeGemini,
		GeminiAPIKey: "test-key",
		Model:        "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	cached, ok := filter.(*cachedFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *cachedFilter", filter)
	}

	geminiFilter, ok := cached.inner.(*GeminiFilter)
	if !ok {
		t.Fatalf("cached inner filter = %T, want *GeminiFilter", cached.inner)
	}
	if got := geminiFilter.modelName; got != "gemini-2.5-pro" {
		t.Fatalf("Gemini model = %q, want %q", got, "gemini-2.5-pro")
	}
}

func TestNewFilterUsesDefaultOpenAIModelWhenUnset(t *testing.T) {
	filter, err := NewFilter(context.Background(), logging.DefaultLogger, FilterConfig{
		Type:         FilterTypeOpenAI,
		OpenAIAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	cached, ok := filter.(*cachedFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *cachedFilter", filter)
	}

	openaiFilter, ok := cached.inner.(*OpenAIFilter)
	if !ok {
		t.Fatalf("cached inner filter = %T, want *OpenAIFilter", cached.inner)
	}
	if got := openaiFilter.model; got != "gpt-4.1" {
		t.Fatalf("OpenAI model = %q, want %q", got, "gpt-4.1")
	}
}

func TestNewFilterUsesConfiguredOpenAIModel(t *testing.T) {
	filter, err := NewFilter(context.Background(), logging.DefaultLogger, FilterConfig{
		Type:         FilterTypeOpenAI,
		OpenAIAPIKey: "test-key",
		Model:        "gpt-5-mini",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	cached, ok := filter.(*cachedFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *cachedFilter", filter)
	}

	openaiFilter, ok := cached.inner.(*OpenAIFilter)
	if !ok {
		t.Fatalf("cached inner filter = %T, want *OpenAIFilter", cached.inner)
	}
	if got := openaiFilter.model; got != "gpt-5-mini" {
		t.Fatalf("OpenAI model = %q, want %q", got, "gpt-5-mini")
	}
}

func TestCachedFilterReusesScoresForSameItems(t *testing.T) {
	inner := &stubFilter{scores: []float32{0.8, 0.5}}
	filter := newCachedFilter(inner, logging.DefaultLogger, 8)

	items := []FilterItem{
		{Title: "Item 1", Description: "foo", Content: "bar", Link: "https://example.com/1"},
		{Title: "Item 2", Description: "baz", Content: "qux", Link: "https://example.com/2"},
	}
	cfg := SourceFilterConfig{}

	got1, err := filter.Evaluate(context.Background(), items, cfg)
	if err != nil {
		t.Fatalf("Evaluate() first call error = %v", err)
	}
	got2, err := filter.Evaluate(context.Background(), items, cfg)
	if err != nil {
		t.Fatalf("Evaluate() second call error = %v", err)
	}

	if inner.evaluateN != 1 {
		t.Fatalf("inner Evaluate() calls = %d, want 1", inner.evaluateN)
	}
	if len(got1) != len(got2) {
		t.Fatalf("score lengths differ: %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Fatalf("score[%d] = %v on second call, want %v", i, got2[i], got1[i])
		}
	}
}

func TestCachedFilterMissesWhenItemsChange(t *testing.T) {
	inner := &stubFilter{scores: []float32{0.9}}
	filter := newCachedFilter(inner, logging.DefaultLogger, 8)
	cfg := SourceFilterConfig{}

	_, err := filter.Evaluate(context.Background(), []FilterItem{
		{Title: "Item 1", Description: "foo", Content: "bar", Link: "https://example.com/1"},
	}, cfg)
	if err != nil {
		t.Fatalf("Evaluate() first call error = %v", err)
	}
	_, err = filter.Evaluate(context.Background(), []FilterItem{
		{Title: "Item 1", Description: "foo", Content: "changed", Link: "https://example.com/1"},
	}, cfg)
	if err != nil {
		t.Fatalf("Evaluate() second call error = %v", err)
	}

	if inner.evaluateN != 2 {
		t.Fatalf("inner Evaluate() calls = %d, want 2", inner.evaluateN)
	}
}
