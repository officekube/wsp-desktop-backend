package main

import (
	"fmt"
	"log"

	"workspace-engine/internal/llm-router/api"
	"workspace-engine/internal/llm-router/config"
	"workspace-engine/pkg/logger"
)

func main() {
	// Initialize configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger.NewLogger()

	// Initialize router
	router := api.NewRouter(cfg)

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Info("Starting server", "address", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
