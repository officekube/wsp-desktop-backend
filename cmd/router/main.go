package main

import (
	"log"

	"workspace-engine/internal/llm-router/api"
	"workspace-engine/internal/llm-router/config"
	"workspace-engine/pkg/logger"
)

func main() {
	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger.NewLogger()

	// Initialize router
	router := api.NewRouter(cfg)

	// Start server
	logger.Info("Starting server on ", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
