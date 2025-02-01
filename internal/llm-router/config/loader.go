package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// LoadConfig loads the configuration from the specified path or default locations
func LoadConfig() (*Config, error) {
	// Check for config path in environment variable
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		// Check common config locations
		commonPaths := []string{
			".",
			"./config",
			"/etc/llm-router",
			"$HOME/.llm-router",
		}

		for _, path := range commonPaths {
			expandedPath := os.ExpandEnv(path)
			if _, err := os.Stat(filepath.Join(expandedPath, "config.yaml")); err == nil {
				configPath = expandedPath
				break
			}
		}
	}

	return Load(configPath)
}

// LoadTestConfig loads the configuration for testing
func LoadTestConfig() (*Config, error) {
	viper.Reset()
	viper.SetConfigName("config.test")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("../../test/config")

	config := &Config{}
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	if err := viper.Unmarshal(config); err != nil {
		return nil, err
	}

	return config, nil
}
