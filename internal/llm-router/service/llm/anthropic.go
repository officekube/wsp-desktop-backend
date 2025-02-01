package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"workspace-engine/internal/llm-router/models"

	"github.com/google/uuid"
)

type AnthropicProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}

// AnthropicMessage represents the message format for Anthropic's API
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AnthropicRequest represents the request structure for Anthropic's API
type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float32            `json:"temperature,omitempty"`
	TopP        float32            `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Stop        []string           `json:"stop,omitempty"`
}

// AnthropicResponse represents the response structure from Anthropic's API
type AnthropicResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      AnthropicUsage `json:"usage"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamResponse represents a streaming response chunk
type AnthropicStreamResponse struct {
	Type       string         `json:"type"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason,omitempty"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://api.anthropic.com/v1",
	}
}

func (p *AnthropicProvider) Generate(
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
	reqBody := AnthropicRequest{
		Model: p.model,
		Messages: []AnthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   maxTokens,
		Temperature: temperature,
		TopP:        topP,
	}

	// Add stop sequences if provided
	if stopSequences, ok := params["stopSequences"].([]string); ok {
		reqBody.Stop = stopSequences
	}

	// Create request
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error: status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var anthropicResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract text content
	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	// Create response
	result := &models.RouteResponse{
		ID:     anthropicResp.ID,
		Result: content,
		Model:  p.model,
		Usage: models.Usage{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
		Metadata: map[string]interface{}{
			"provider":    "anthropic",
			"stop_reason": anthropicResp.StopReason,
		},
	}

	return result, nil
}

func (p *AnthropicProvider) GenerateStream(
	ctx context.Context, prompt string, params map[string]interface{},
) (<-chan models.StreamResponse, error) {
	stream := make(chan models.StreamResponse)

	reqBody := AnthropicRequest{
		Model: p.model,
		Messages: []AnthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: true,
	}

	// Apply parameters
	p.applyParameters(&reqBody, params)

	// Create request
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

	// Make request
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

			// Skip empty lines
			if len(line) <= 1 {
				continue
			}

			// Parse SSE data
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}

			data := bytes.TrimPrefix(line, []byte("data: "))
			if bytes.Equal(data, []byte("[DONE]")) {
				return
			}

			var streamResp AnthropicStreamResponse
			if err := json.Unmarshal(data, &streamResp); err != nil {
				stream <- models.StreamResponse{
					Error: fmt.Errorf("failed to unmarshal stream response: %w", err),
				}
				return
			}

			// Handle errors
			if streamResp.Error != nil {
				stream <- models.StreamResponse{
					Error: fmt.Errorf("anthropic api error: %s", streamResp.Error.Message),
				}
				return
			}

			// Extract content
			var content string
			for _, block := range streamResp.Content {
				if block.Type == "text" {
					content += block.Text
				}
			}

			stream <- models.StreamResponse{
				ID:      uuid.New().String(),
				Content: content,
				Done:    streamResp.StopReason != "",
			}

			if streamResp.StopReason != "" {
				return
			}
		}
	}()

	return stream, nil
}

func (p *AnthropicProvider) GetModelInfo() models.ModelInfo {
	return models.ModelInfo{
		ID:       p.model,
		Name:     "Claude",
		Provider: "Anthropic",
		Capabilities: []string{
			"text-generation",
			"chat",
			"analysis",
		},
		MaxTokens: 100000, // Adjust based on the specific Claude model
		Pricing: models.Pricing{
			InputPrice:  0.008, // Adjust based on current pricing
			OutputPrice: 0.024, // Adjust based on current pricing
			Currency:    "USD",
		},
	}
}

func (p *AnthropicProvider) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return false
	}

	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Helper function to apply parameters to the request
func (p *AnthropicProvider) applyParameters(req *AnthropicRequest, params map[string]interface{}) {
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

// Add retry mechanism
func (p *AnthropicProvider) generateWithRetry(
	ctx context.Context, prompt string, params map[string]interface{}, maxRetries int,
) (*models.RouteResponse, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := p.Generate(ctx, prompt, params)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}

		// Exponential backoff
		waitTime := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitTime):
			continue
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// Helper function to determine if an error is retryable
func (p *AnthropicProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for rate limits and server errors
	if strings.Contains(err.Error(), "429") || // Rate limit
		strings.Contains(err.Error(), "500") || // Internal server error
		strings.Contains(err.Error(), "503") { // Service unavailable
		return true
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for network errors
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}

	return false
}
