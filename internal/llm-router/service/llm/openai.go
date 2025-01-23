package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"time"

	"workspace-engine/internal/llm-router/models"

	"github.com/google/uuid"
	openai "github.com/sashabaranov/go-openai"
)

const DefaultTemperature = float32(0.7)
const MaxTokens = 1000
const Penalty = float32(0)
const TopP = float32(1)

type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	client := openai.NewClient(apiKey)

	return &OpenAIProvider{
		client: client,
		model:  model,
	}
}

func (p *OpenAIProvider) Generate(
	ctx context.Context, prompt string, params map[string]interface{},
) (*models.RouteResponse, error) {
	temperature := DefaultTemperature
	maxTokens := MaxTokens
	topP := TopP
	presencePenalty := Penalty
	frequencyPenalty := Penalty

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
		if pp, ok := params["presencePenalty"].(float64); ok {
			presencePenalty = float32(pp)
		}
		if fp, ok := params["frequencyPenalty"].(float64); ok {
			frequencyPenalty = float32(fp)
		}
	}

	// Create request
	req := openai.ChatCompletionRequest{
		Model:            p.model,
		Temperature:      temperature,
		MaxTokens:        maxTokens,
		TopP:             topP,
		PresencePenalty:  presencePenalty,
		FrequencyPenalty: frequencyPenalty,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	}

	// Add stop sequences if provided
	if stopSequences, ok := params["stopSequences"].([]string); ok {
		req.Stop = stopSequences
	}

	// Create timeout context if specified
	if timeout, ok := params["timeout"].(int); ok && timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()
	}

	// Make API call
	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai api error: %w", err)
	}

	// Extract response content
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	// Create response
	result := &models.RouteResponse{
		ID:     uuid.New().String(),
		Result: resp.Choices[0].Message.Content,
		Model:  p.model,
		Usage: models.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		Metadata: map[string]interface{}{
			"provider":      "openai",
			"finish_reason": resp.Choices[0].FinishReason,
			"created":       resp.Created,
		},
	}

	return result, nil
}

func (p *OpenAIProvider) GetModelInfo() models.ModelInfo {
	return models.ModelInfo{
		ID:       "gpt-4",
		Name:     "GPT-4",
		Provider: "OpenAI",
		Capabilities: []string{
			"text-generation",
			"code-generation",
		},
		MaxTokens: 8192,
		Pricing: models.Pricing{
			InputPrice:  0.03,
			OutputPrice: 0.06,
			Currency:    "USD",
		},
	}
}

func (p *OpenAIProvider) IsHealthy() bool {
	// Implement health check
	return true
}

// Add error retry handling
func (p *OpenAIProvider) generateWithRetry(
	ctx context.Context, prompt string, params map[string]interface{}, maxRetries int,
) (*models.RouteResponse, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := p.Generate(ctx, prompt, params)
		if err == nil {
			return result, nil
		}

		lastErr = err
		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, err
		}

		// Wait before retrying with exponential backoff
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
func isRetryableError(err error) bool {
	// Check for rate limits, temporary server errors, etc.
	if err == nil {
		return false
	}

	// Check for specific OpenAI API errors
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case 429: // Rate limit
			return true
		case 500, 502, 503, 504: // Server errors
			return true
		default:
			return false
		}
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for network errors
	if netErr, ok := err.(net.Error); ok {
		return netErr.Temporary()
	}

	return false
}

// Add streaming support
func (p *OpenAIProvider) GenerateStream(
	ctx context.Context, prompt string, params map[string]interface{},
) (<-chan models.StreamResponse, error) {
	stream := make(chan models.StreamResponse)

	req := openai.ChatCompletionRequest{
		Model:    p.model,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
		Stream:   true,
	}

	// Apply parameters similar to non-streaming version
	applyParameters(&req, params)

	streamResp, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	go func() {
		defer close(stream)
		defer streamResp.Close()

		for {
			response, err := streamResp.Recv()
			if errors.Is(err, io.EOF) {
				return
			}

			if err != nil {
				stream <- models.StreamResponse{
					Error: err,
				}
				return
			}

			if len(response.Choices) > 0 {
				stream <- models.StreamResponse{
					ID:      response.ID,
					Content: response.Choices[0].Delta.Content,
					Done:    response.Choices[0].FinishReason != "",
				}
			}
		}
	}()

	return stream, nil
}

// Helper function to apply parameters to the request
func applyParameters(req *openai.ChatCompletionRequest, params map[string]interface{}) {
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
	if pp, ok := params["presencePenalty"].(float64); ok {
		req.PresencePenalty = float32(pp)
	}
	if fp, ok := params["frequencyPenalty"].(float64); ok {
		req.FrequencyPenalty = float32(fp)
	}
	if stop, ok := params["stopSequences"].([]string); ok {
		req.Stop = stop
	}
}
