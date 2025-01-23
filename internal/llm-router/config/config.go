package config

type Config struct {
	Environment  string
	OpenAIKey    string
	AnthropicKey string
	Port         string
	LogLevel     string
}

func Load() (*Config, error) {
	// Implement configuration loading from environment variables or file
	return &Config{
		Environment:  "production",
		OpenAIKey:    "your-openai-key",
		AnthropicKey: "your-anthropic-key",
		Port:         "8081",
		LogLevel:     "info",
	}, nil
}
