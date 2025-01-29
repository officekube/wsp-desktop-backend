package llm

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterProvider(t *testing.T) {
	// Skip if no API key is provided
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	provider := NewOpenRouterProvider(
		apiKey,
		"openai/gpt-3.5-turbo",
		map[string]string{
			"HTTP-Referer": "test.com",
			"X-Title":      "Test App",
		},
	)

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
		"IsHealthy", func(t *testing.T) {
			assert.True(t, provider.IsHealthy())
		},
	)
}
