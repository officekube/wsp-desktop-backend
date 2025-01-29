package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"workspace-engine/internal/llm-router/models"
)

const (
	openRouterBaseURL = "https://openrouter.ai/api/v1"
)

type OpenRouterProvider struct {
	apiKey      string
	model       string
	client      *http.Client
	httpHeaders map[string]string
}

type OpenRouterRequest struct {
	Model       string                 `json:"model"`
	Messages    []OpenRouterMessage    `json:"messages"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float32                `json:"temperature,omitempty"`
	TopP        float32                `json:"top_p,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
	Stop        []string               `json:"stop,omitempty"`
	Headers     map[string]interface{} `json:"headers,omitempty"`
}

type OpenRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenRouterResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Created int64    `json:"created"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewOpenRouterProvider(apiKey, model string, httpHeaders map[string]string) *OpenRouterProvider {
	return &OpenRouterProvider{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: 30 * time.Second},
		httpHeaders: httpHeaders,
	}
}

func (p *OpenRouterProvider) Generate(
	ctx context.Context, prompt string, params map[string]interface{},
) (*models.RouteResponse, error) {
	// Parse parameters with default values
	temperature := float32(0.7)
	maxTokens := 1000
	topP := float32(1.0)

	// Override defaults with provided parameters
	if params != nil {
		if temp, ok := params["temperature"].(float64); ok {
			temperature = float32(temp)
		}
		if tokens, ok := params["maxTokens"].(int); ok {
			maxTokens = tokens
		}
		if tp, ok := params["topP"].(float64); ok {
			topP = float32(tp)
		}
	}

	// Create request
	reqBody := OpenRouterRequest{
		Model: p.model,
		Messages: []OpenRouterMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   maxTokens,
		Temperature: temperature,
		TopP:        topP,
		Headers:     p.getRequestHeaders(),
	}

	// Add stop sequences if provided
	if stopSequences, ok := params["stopSequences"].([]string); ok {
		reqBody.Stop = stopSequences
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, "POST", openRouterBaseURL+"/chat/completions", bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	for k, v := range p.httpHeaders {
		req.Header.Set(k, v)
	}

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter api error: status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var openRouterResp OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&openRouterResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openRouterResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	// Create response
	result := &models.RouteResponse{
		ID:     openRouterResp.ID,
		Result: openRouterResp.Choices[0].Message.Content,
		Model:  openRouterResp.Model,
		Usage: models.Usage{
			PromptTokens:     openRouterResp.Usage.PromptTokens,
			CompletionTokens: openRouterResp.Usage.CompletionTokens,
			TotalTokens:      openRouterResp.Usage.TotalTokens,
		},
		Metadata: map[string]interface{}{
			"provider":      "openrouter",
			"finish_reason": openRouterResp.Choices[0].FinishReason,
			"created":       openRouterResp.Created,
		},
	}

	return result, nil
}

func (p *OpenRouterProvider) GenerateStream(
	ctx context.Context, prompt string, params map[string]interface{},
) (<-chan models.StreamResponse, error) {
	stream := make(chan models.StreamResponse)

	reqBody := OpenRouterRequest{
		Model: p.model,
		Messages: []OpenRouterMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream:  true,
		Headers: p.getRequestHeaders(),
	}

	// Apply parameters
	p.applyParameters(&reqBody, params)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, "POST", openRouterBaseURL+"/chat/completions", bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range p.httpHeaders {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	go func() {
		defer close(stream)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					stream <- models.StreamResponse{
						Error: fmt.Errorf("stream read error: %w", err),
					}
				}
				return
			}

			if len(line) <= 1 {
				continue
			}

			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}

			data := bytes.TrimPrefix(line, []byte("data: "))
			if bytes.Equal(data, []byte("[DONE]")) {
				return
			}

			var streamResp OpenRouterResponse
			if err := json.Unmarshal(data, &streamResp); err != nil {
				stream <- models.StreamResponse{
					Error: fmt.Errorf("failed to unmarshal stream response: %w", err),
				}
				return
			}

			if len(streamResp.Choices) > 0 {
				stream <- models.StreamResponse{
					ID:      streamResp.ID,
					Content: streamResp.Choices[0].Message.Content,
					Done:    streamResp.Choices[0].FinishReason != "",
				}

				if streamResp.Choices[0].FinishReason != "" {
					return
				}
			}
		}
	}()

	return stream, nil
}

func (p *OpenRouterProvider) GetModelInfo() models.ModelInfo {
	return models.ModelInfo{
		ID:       p.model,
		Name:     p.model,
		Provider: "openrouter",
		Capabilities: []string{
			"text-generation",
			"chat",
		},
		MaxTokens: 8192, // This should be configured based on the specific model
		Pricing: models.Pricing{
			InputPrice:  0.0, // Set based on OpenRouter pricing
			OutputPrice: 0.0, // Set based on OpenRouter pricing
			Currency:    "USD",
		},
	}
}

func (p *OpenRouterProvider) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", openRouterBaseURL+"/models", nil)
	if err != nil {
		return false
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func (p *OpenRouterProvider) getRequestHeaders() map[string]interface{} {
	headers := make(map[string]interface{})
	for k, v := range p.httpHeaders {
		headers[k] = v
	}
	return headers
}

func (p *OpenRouterProvider) applyParameters(req *OpenRouterRequest, params map[string]interface{}) {
	if params == nil {
		return
	}

	if temp, ok := params["temperature"].(float64); ok {
		req.Temperature = float32(temp)
	}
	if tokens, ok := params["maxTokens"].(int); ok {
		req.MaxTokens = tokens
	}
	if topP, ok := params["topP"].(float64); ok {
		req.TopP = float32(topP)
	}
	if stop, ok := params["stopSequences"].([]string); ok {
		req.Stop = stop
	}
}
