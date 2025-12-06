package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"github.com/maxnilz/feed/errors"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type OpenAIFilter struct {
	client *openai.Client
	model  string
}

func NewOpenAIFilter(apiKey string) (*OpenAIFilter, error) {
	if apiKey == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "API key is required for OpenAI filter")
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))

	return &OpenAIFilter{
		client: &client,
		model:  "gpt-4.1",
	}, nil
}

func (f *OpenAIFilter) Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error) {
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

	resp, err := f.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: f.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		Temperature: openai.Float(0),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "filter_response",
					Strict: openai.Bool(true),
					Schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"items": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"id":    map[string]any{"type": "string"},
										"title": map[string]any{"type": "string"},
										"score": map[string]any{"type": "number"},
									},
									"required":             []string{"id", "title", "score"},
									"additionalProperties": false,
								},
							},
						},
						"required":             []string{"items"},
						"additionalProperties": false,
					},
				},
			},
		},
	})
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "OpenAI generate content failed")
	}

	if len(resp.Choices) == 0 {
		return nil, errors.Newf(errors.Internal, nil, "no content returned from OpenAI")
	}

	content := resp.Choices[0].Message.Content

	var response struct {
		Items []FilterResponseItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return nil, errors.Newf(errors.Internal, err, "failed to parse OpenAI response: %s", content)
	}

	if len(response.Items) != len(items) {
		return nil, errors.Newf(errors.Internal, nil, "OpenAI returned %d items, expected %d", len(response.Items), len(items))
	}

	result := make([]float32, len(response.Items))
	for i, item := range response.Items {
		result[i] = item.Score
	}

	return result, nil
}

func (f *OpenAIFilter) buildPrompt(items []FilterItem, criteria []string, cfg SourceFilterConfig) (string, error) {
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

func (f *OpenAIFilter) Close() error {
	return nil
}
