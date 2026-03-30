package ai

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/maxnilz/feed/ai/prompts"
	"github.com/maxnilz/feed/errors"
)

// FilterItem represents an item to be evaluated by the filter.
type FilterItem struct {
	Title       string
	Description string
	Content     string
	Link        string
}

// Filter evaluates items for relevance based on criteria.
type Filter interface {
	// Evaluate accepts a batch of items and source config, returns relevance scores (0.0 to 1.0).
	Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error)
	Close() error
}

type FilterType string

const (
	FilterTypeEmbedding FilterType = "embedding"
	FilterTypeGemini    FilterType = "gemini"
	FilterTypeOpenAI    FilterType = "openai"
)

// FilterConfig holds global configuration for the AI filter.
type FilterConfig struct {
	Type         FilterType `yaml:"type"`         // "embedding", "gemini", or "openai", defaults to "embedding"
	Model        string     `yaml:"model"`        // Model name for the selected LLM provider (used when type is "gemini" or "openai")
	GeminiAPIKey string     `yaml:"geminiApiKey"` // API key for Gemini (used when type is "gemini" or "embedding")
	OpenAIAPIKey string     `yaml:"openaiApiKey"` // API key for OpenAI (used when type is "openai")
}

// GetFilterType returns the filter type, defaulting to embedding if not set.
func (c FilterConfig) GetFilterType() FilterType {
	if c.Type == "" {
		return FilterTypeEmbedding
	}
	return c.Type
}

// SourceFilterConfig holds per-source filter configuration.
type SourceFilterConfig struct {
	// Inline semantic filters (short lists)
	SemanticFilters []string `yaml:"semanticFilters"`
	// File path containing semantic filters (one per line)
	SemanticFiltersFile string `yaml:"semanticFiltersFile"`
	// Embedded semantic filters file name (from ai/prompts directory)
	EmbeddedFiltersFile string `yaml:"embeddedFiltersFile"`
	// Custom LLM prompt template file
	LLMPromptFile string `yaml:"llmPromptFile"`
	// Embedded LLM prompt template name (from ai/prompts directory)
	EmbeddedPromptFile string `yaml:"embeddedPromptFile"`
	// Minimum similarity threshold (0.0-1.0)
	SimilarityThreshold float32 `yaml:"similarityThreshold"`
}

// LoadSemanticFilters returns all semantic filters, combining inline, file-based, and embedded.
func (c SourceFilterConfig) LoadSemanticFilters() ([]string, error) {
	filters := make([]string, 0, len(c.SemanticFilters))
	filters = append(filters, c.SemanticFilters...)

	if c.SemanticFiltersFile != "" {
		fileFilters, err := loadLinesFromFile(c.SemanticFiltersFile)
		if err != nil {
			return nil, errors.Newf(errors.InvalidArgument, err, "load semantic filters file %s failed", c.SemanticFiltersFile)
		}
		filters = append(filters, fileFilters...)
	}

	if c.EmbeddedFiltersFile != "" {
		embeddedFilters, err := loadLinesFromEmbedded(c.EmbeddedFiltersFile)
		if err != nil {
			return nil, errors.Newf(errors.InvalidArgument, err, "load embedded filters file %s failed", c.EmbeddedFiltersFile)
		}
		filters = append(filters, embeddedFilters...)
	}
	return filters, nil
}

// LoadLLMPromptTemplate loads the LLM prompt template from file or embedded assets.
func (c SourceFilterConfig) LoadLLMPromptTemplate() (string, error) {
	// First check embedded prompt (takes precedence)
	if c.EmbeddedPromptFile != "" {
		data, err := prompts.FS.ReadFile(c.EmbeddedPromptFile)
		if err != nil {
			return "", errors.Newf(errors.InvalidArgument, err, "load embedded prompt file %s failed", c.EmbeddedPromptFile)
		}
		return string(data), nil
	}

	// Fall back to file-based prompt
	if c.LLMPromptFile == "" {
		return "", nil
	}
	data, err := os.ReadFile(c.LLMPromptFile)
	if err != nil {
		return "", errors.Newf(errors.InvalidArgument, err, "load LLM prompt file %s failed", c.LLMPromptFile)
	}
	return string(data), nil
}

// loadLinesFromFile reads a file and returns non-empty, trimmed lines.
func loadLinesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return scanLines(bufio.NewScanner(f))
}

// loadLinesFromEmbedded reads an embedded file and returns non-empty, trimmed lines.
func loadLinesFromEmbedded(name string) ([]string, error) {
	data, err := prompts.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return scanLines(bufio.NewScanner(strings.NewReader(string(data))))
}

// scanLines scans lines from a scanner and returns non-empty, trimmed lines.
func scanLines(scanner *bufio.Scanner) ([]string, error) {
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "//") { // Skip empty lines and comments
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func NewFilter(ctx context.Context, cfg FilterConfig) (Filter, error) {
	switch cfg.GetFilterType() {
	case FilterTypeGemini:
		return NewGeminiFilter(ctx, cfg.GeminiAPIKey, cfg.Model)
	case FilterTypeOpenAI:
		return NewOpenAIFilter(cfg.OpenAIAPIKey, cfg.Model)
	case FilterTypeEmbedding:
		return NewEmbeddingFilter(ctx, cfg.GeminiAPIKey)
	default:
		return NewEmbeddingFilter(ctx, cfg.GeminiAPIKey)
	}
}
