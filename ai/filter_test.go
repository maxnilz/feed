package ai

import (
	"context"
	"testing"
)

func TestNewFilterUsesConfiguredGeminiModel(t *testing.T) {
	filter, err := NewFilter(context.Background(), FilterConfig{
		Type:         FilterTypeGemini,
		GeminiAPIKey: "test-key",
		Model:        "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	geminiFilter, ok := filter.(*GeminiFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *GeminiFilter", filter)
	}
	if got := geminiFilter.modelName; got != "gemini-2.5-pro" {
		t.Fatalf("Gemini model = %q, want %q", got, "gemini-2.5-pro")
	}
}

func TestNewFilterUsesDefaultOpenAIModelWhenUnset(t *testing.T) {
	filter, err := NewFilter(context.Background(), FilterConfig{
		Type:         FilterTypeOpenAI,
		OpenAIAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	openaiFilter, ok := filter.(*OpenAIFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *OpenAIFilter", filter)
	}
	if got := openaiFilter.model; got != "gpt-4.1" {
		t.Fatalf("OpenAI model = %q, want %q", got, "gpt-4.1")
	}
}

func TestNewFilterUsesConfiguredOpenAIModel(t *testing.T) {
	filter, err := NewFilter(context.Background(), FilterConfig{
		Type:         FilterTypeOpenAI,
		OpenAIAPIKey: "test-key",
		Model:        "gpt-5-mini",
	})
	if err != nil {
		t.Fatalf("NewFilter() error = %v", err)
	}
	defer filter.Close()

	openaiFilter, ok := filter.(*OpenAIFilter)
	if !ok {
		t.Fatalf("NewFilter() returned %T, want *OpenAIFilter", filter)
	}
	if got := openaiFilter.model; got != "gpt-5-mini" {
		t.Fatalf("OpenAI model = %q, want %q", got, "gpt-5-mini")
	}
}
