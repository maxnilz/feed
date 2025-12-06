package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"github.com/google/generative-ai-go/genai"
	"github.com/maxnilz/feed/errors"
	"google.golang.org/api/option"
)

// PromptData is the data structure passed to the prompt template.
type PromptData struct {
	Items    []FilterItem
	Criteria []string
}

// FilterResponseItem represents a single item in the filter response.
type FilterResponseItem struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Score float32 `json:"score"`
}

type GeminiFilter struct {
	client *genai.Client
	model  *genai.GenerativeModel
}

func NewGeminiFilter(ctx context.Context, apiKey string) (*GeminiFilter, error) {
	if apiKey == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "API key is required for Gemini filter")
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "create genai client failed")
	}
	model := client.GenerativeModel("gemini-2.5-flash")
	model.SetTemperature(0)
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeArray,
		Items: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"id":    {Type: genai.TypeString},
				"title": {Type: genai.TypeString},
				"score": {Type: genai.TypeNumber},
			},
			Required: []string{"id", "title", "score"},
		},
	}

	return &GeminiFilter{
		client: client,
		model:  model,
	}, nil
}

func (f *GeminiFilter) Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error) {
	criteria, err := cfg.LoadSemanticFilters()
	if err != nil {
		return nil, err
	}

	if len(criteria) == 0 || len(items) == 0 {
		scores := make([]float32, len(items))
		for i := range scores {
			scores[i] = 1.0
		}
		return scores, nil
	}

	prompt, err := f.buildPrompt(items, criteria, cfg)
	if err != nil {
		return nil, err
	}

	resp, err := f.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "Gemini generate content failed")
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, errors.Newf(errors.Internal, nil, "no content returned from Gemini")
	}

	var responseItems []FilterResponseItem
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			if err := json.Unmarshal([]byte(txt), &responseItems); err != nil {
				return nil, errors.Newf(errors.Internal, err, "failed to parse Gemini response: %s", txt)
			}
			break
		}
	}

	if len(responseItems) != len(items) {
		return nil, errors.Newf(errors.Internal, nil, "Gemini returned %d items, expected %d", len(responseItems), len(items))
	}

	// Extract scores from response items
	result := make([]float32, len(responseItems))
	for i, item := range responseItems {
		result[i] = item.Score
	}

	return result, nil
}

func (f *GeminiFilter) buildPrompt(items []FilterItem, criteria []string, cfg SourceFilterConfig) (string, error) {
	tmplStr, err := cfg.LoadLLMPromptTemplate()
	if err != nil {
		return "", err
	}
	if tmplStr == "" {
		return "", errors.Newf(errors.InvalidArgument, nil, "LLM prompt template is required (set llmPromptFile in config)")
	}

	tmpl, err := template.New("prompt").Parse(tmplStr)
	if err != nil {
		return "", errors.Newf(errors.InvalidArgument, err, "parse prompt template failed")
	}

	data := PromptData{
		Items:    items,
		Criteria: criteria,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.Newf(errors.Internal, err, "execute prompt template failed")
	}

	return buf.String(), nil
}

func (f *GeminiFilter) Close() error {
	return f.client.Close()
}
