package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Providers ProvidersConfig `mapstructure:"providers"`
}

type ServerConfig struct {
	Port        int           `mapstructure:"port"`
	Host        string        `mapstructure:"host"`
	Timeout     time.Duration `mapstructure:"timeout"`
	CORS        CORSConfig    `mapstructure:"cors"`
	Environment string        `mapstructure:"environment"`
}

type CORSConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	AllowedMethods []string `mapstructure:"allowed_methods"`
}

type ProvidersConfig struct {
	OpenAI    ProviderConfig `mapstructure:"openai"`
	Anthropic ProviderConfig `mapstructure:"anthropic"`
}

type ProviderConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	APIKey       string        `mapstructure:"api_key"`
	DefaultModel string        `mapstructure:"default_model"`
	Models       []ModelConfig `mapstructure:"models"`
}

type ModelConfig struct {
	Name      string        `mapstructure:"name"`
	MaxTokens int           `mapstructure:"max_tokens"`
	Timeout   time.Duration `mapstructure:"timeout"`
}

// Load loads the configuration from config files and environment variables
func Load(configPath string) (*Config, error) {
	var config Config

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Add config path
	if configPath != "" {
		viper.AddConfigPath(configPath)
	}
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Read environment variables
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// validateConfig performs validation on the configuration
func validateConfig(config *Config) error {
	// Validate server config
	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", config.Server.Port)
	}

	// Validate providers
	if !config.Providers.OpenAI.Enabled && !config.Providers.Anthropic.Enabled {
		return fmt.Errorf("at least one provider must be enabled")
	}

	// Validate OpenAI config
	if config.Providers.OpenAI.Enabled {
		if config.Providers.OpenAI.APIKey == "" {
			return fmt.Errorf("OpenAI API key is required when OpenAI is enabled")
		}
		if len(config.Providers.OpenAI.Models) == 0 {
			return fmt.Errorf("at least one OpenAI model must be configured")
		}
	}

	// Validate Anthropic config
	if config.Providers.Anthropic.Enabled {
		if config.Providers.Anthropic.APIKey == "" {
			return fmt.Errorf("Anthropic API key is required when Anthropic is enabled")
		}
		if len(config.Providers.Anthropic.Models) == 0 {
			return fmt.Errorf("at least one Anthropic model must be configured")
		}
	}

	return nil
}

// Helper functions to get specific config values
func (c *Config) GetProviderAPIKey(provider string) string {
	switch provider {
	case "openai":
		return c.Providers.OpenAI.APIKey
	case "anthropic":
		return c.Providers.Anthropic.APIKey
	default:
		return ""
	}
}

func (c *Config) IsProviderEnabled(provider string) bool {
	switch provider {
	case "openai":
		return c.Providers.OpenAI.Enabled
	case "anthropic":
		return c.Providers.Anthropic.Enabled
	default:
		return false
	}
}

func (c *Config) GetModelConfig(provider, model string) (*ModelConfig, error) {
	var models []ModelConfig
	switch provider {
	case "openai":
		models = c.Providers.OpenAI.Models
	case "anthropic":
		models = c.Providers.Anthropic.Models
	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	for _, m := range models {
		if m.Name == model {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("model not found: %s", model)
}
