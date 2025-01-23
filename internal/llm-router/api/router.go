package api

import (
	"workspace-engine/internal/llm-router/config"
	"workspace-engine/internal/llm-router/service"
	"workspace-engine/internal/llm-router/service/llm"

	"github.com/gin-gonic/gin"
)

func NewRouter(cfg *config.Config) *gin.Engine {
	// Set Gin mode
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(LoggingMiddleware())
	router.Use(ErrorMiddleware())
	router.Use(CORSMiddleware())

	// Initialize providers
	providers := map[string]llm.Provider{
		"openai": llm.NewOpenAIProvider(cfg.OpenAIKey, "gpt-4"),
		//TODO: Add more providers as needed
	}

	// Initialize services
	routerService := service.NewRouterService(providers)
	handler := NewHandler(routerService)

	// API routes
	api := router.Group("/api/v1")
	{
		// Public endpoints
		api.GET("/health", handler.GetHealth)

		// Protected endpoints
		protected := api.Group("")
		protected.Use(AuthMiddleware())
		{
			protected.POST("/route", handler.RoutePrompt)
			protected.GET("/route/stream", handler.StreamRoutePrompt)
			protected.GET("/models", handler.GetModels)
		}
	}

	return router
}
