package llm

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroqProvider(t *testing.T) {
	// Skip if no API key is provided
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		t.Skip("GROQ_API_KEY not set")
	}

	provider := NewGroqProvider(apiKey, "llama2-70b-4096")

	t.Run(
		"Generate", func(t *testing.T) {
			ctx := context.Background()
			resp, err := provider.Generate(ctx, "Say hello", nil)
			require.NoError(t, err)
			assert.NotEmpty(t, resp.Result)
			assert.NotEmpty(t, resp.ID)
		},
	)

	t.Run(
		"GenerateStream", func(t *testing.T) {
			ctx := context.Background()
			stream, err := provider.GenerateStream(ctx, "Tell me a short story", nil)
			require.NoError(t, err)

			var fullResponse string
			for chunk := range stream {
				require.NoError(t, chunk.Error)
				fullResponse += chunk.Content
			}
			assert.NotEmpty(t, fullResponse)
		},
	)

	t.Run(
		"WithParameters", func(t *testing.T) {
			ctx := context.Background()
			params := map[string]interface{}{
				"temperature": 0.5,
				"maxTokens":   100,
				"topP":        0.9,
			}
			resp, err := provider.Generate(ctx, "Say hello", params)
			require.NoError(t, err)
			assert.NotEmpty(t, resp.Result)
		},
	)

	t.Run(
		"IsHealthy", func(t *testing.T) {
			assert.True(t, provider.IsHealthy())
		},
	)
}
