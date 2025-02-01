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
)

const (
	groqBaseURL = "https://api.groq.com/v1"
)

type GroqProvider struct {
	apiKey string
	model  string
	client *http.Client
}

type GroqRequest struct {
	Model       string        `json:"model"`
	Messages    []GroqMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	TopP        float32       `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

type GroqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GroqResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []GroqChoice `json:"choices"`
	Usage   GroqUsage    `json:"usage"`
}

type GroqChoice struct {
	Index        int         `json:"index"`
	Message      GroqMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type GroqUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewGroqProvider(apiKey, model string) *GroqProvider {
	return &GroqProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (p *GroqProvider) Generate(
	ctx context.Context, prompt string, params map[string]interface{},
) (*models.RouteResponse, error) {
	// Parse parameters with default values
	temperature := float32(0.7)
	maxTokens := 4096
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
	reqBody := GroqRequest{
		Model: p.model,
		Messages: []GroqMessage{
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

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", groqBaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq api error: status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var groqResp GroqResponse
	if err := json.NewDecoder(resp.Body).Decode(&groqResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(groqResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	// Create response
	result := &models.RouteResponse{
		ID:     groqResp.ID,
		Result: groqResp.Choices[0].Message.Content,
		Model:  groqResp.Model,
		Usage: models.Usage{
			PromptTokens:     groqResp.Usage.PromptTokens,
			CompletionTokens: groqResp.Usage.CompletionTokens,
			TotalTokens:      groqResp.Usage.TotalTokens,
		},
		Metadata: map[string]interface{}{
			"provider":      "groq",
			"finish_reason": groqResp.Choices[0].FinishReason,
			"created":       groqResp.Created,
			"latency":       resp.Header.Get("X-Completion-Latency"),
		},
	}

	return result, nil
}

func (p *GroqProvider) GenerateStream(
	ctx context.Context, prompt string, params map[string]interface{},
) (<-chan models.StreamResponse, error) {
	stream := make(chan models.StreamResponse)

	reqBody := GroqRequest{
		Model: p.model,
		Messages: []GroqMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: true,
	}

	// Apply parameters
	p.applyParameters(&reqBody, params)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", groqBaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")

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

			var streamResp GroqResponse
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

func (p *GroqProvider) GetModelInfo() models.ModelInfo {
	return models.ModelInfo{
		ID:       p.model,
		Name:     p.model,
		Provider: "groq",
		Capabilities: []string{
			"text-generation",
			"chat",
		},
		MaxTokens: 32768, // Adjust based on the specific model
		Pricing: models.Pricing{
			InputPrice:  0.0, // Set based on Groq pricing
			OutputPrice: 0.0, // Set based on Groq pricing
			Currency:    "USD",
		},
	}
}

func (p *GroqProvider) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", groqBaseURL+"/models", nil)
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

func (p *GroqProvider) applyParameters(req *GroqRequest, params map[string]interface{}) {
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

// Helper function to handle retries
func (p *GroqProvider) generateWithRetry(
	ctx context.Context, prompt string, params map[string]interface{}, maxRetries int,
) (*models.RouteResponse, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := p.Generate(ctx, prompt, params)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !p.isRetryableError(err) {
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

func (p *GroqProvider) isRetryableError(err error) bool {
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
