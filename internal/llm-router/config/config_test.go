package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadTestConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Test server config
	assert.Equal(t, 8081, cfg.Server.Port)
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 5*time.Second, cfg.Server.Timeout)

	// Test OpenAI provider config
	assert.True(t, cfg.Providers.OpenAI.Enabled)
	assert.Equal(t, "test-key", cfg.Providers.OpenAI.APIKey)
	assert.Equal(t, "gpt-3.5-turbo", cfg.Providers.OpenAI.DefaultModel)

	// Test model config
	model, err := cfg.GetModelConfig("openai", "gpt-3.5-turbo")
	require.NoError(t, err)
	assert.Equal(t, 4096, model.MaxTokens)
	assert.Equal(t, 5*time.Second, model.Timeout)
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Providers: ProvidersConfig{
					OpenAI: ProviderConfig{
						Enabled: true,
						APIKey:  "test-key",
						Models:  []ModelConfig{{Name: "gpt-4"}},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid port",
			config: &Config{
				Server: ServerConfig{
					Port: -1,
				},
			},
			expectError: true,
		},
		{
			name: "no providers enabled",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Providers: ProvidersConfig{
					OpenAI: ProviderConfig{
						Enabled: false,
					},
					Anthropic: ProviderConfig{
						Enabled: false,
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				err := validateConfig(tt.config)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			},
		)
	}
}
