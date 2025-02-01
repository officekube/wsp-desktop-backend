package api

import (
	"workspace-engine/internal/llm-router/config"
	"workspace-engine/internal/llm-router/service"
	"workspace-engine/internal/llm-router/service/llm"

	"github.com/gin-gonic/gin"
)

func NewRouter(cfg *config.Config) *gin.Engine {
	// Set Gin mode
	if cfg.Server.Environment == "production" {
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
	providers := map[string]llm.Provider{}

	openAI := cfg.Providers.OpenAI
	if openAI.Enabled {
		providers["openai_default"] = llm.NewOpenAIProvider(openAI.APIKey, openAI.DefaultModel)
		for _, model := range openAI.Models {
			providers["openai_"+model.Name] = llm.NewOpenAIProvider(openAI.APIKey, model.Name)
		}
	}

	anthropic := cfg.Providers.Anthropic
	if anthropic.Enabled {
		providers["anthropic_default"] = llm.NewAnthropicProvider(anthropic.APIKey, anthropic.DefaultModel)
		for _, model := range anthropic.Models {
			providers["anthropic_"+model.Name] = llm.NewAnthropicProvider(anthropic.APIKey, model.Name)
		}
	}

	openRouter := cfg.Providers.OpenRouter
	if openRouter.Enabled {
		httpHeaders := map[string]string{
			"HTTP-Referer": "officekube.io",
			"X-Title":      "LLM Router",
		}
		providers["openrouter_default"] = llm.NewOpenRouterProvider(
			openRouter.APIKey, openRouter.DefaultModel, httpHeaders,
		)
		for _, model := range openRouter.Models {
			providers["openrouter_"+model.Name] = llm.NewOpenRouterProvider(openRouter.APIKey, model.Name, httpHeaders)
		}
	}

	groq := cfg.Providers.Groq
	if groq.Enabled {
		providers["groq_default"] = llm.NewGroqProvider(groq.APIKey, groq.DefaultModel)
		for _, model := range groq.Models {
			providers["groq_"+model.Name] = llm.NewGroqProvider(groq.APIKey, model.Name)
		}
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
