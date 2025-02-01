package llm

import (
	"context"

	"workspace-engine/internal/llm-router/models"
)

type Provider interface {
	Generate(ctx context.Context, prompt string, params map[string]interface{}) (*models.RouteResponse, error)
	GenerateStream(ctx context.Context, prompt string, params map[string]interface{}) (
		<-chan models.StreamResponse, error,
	)
	GetModelInfo() models.ModelInfo
	IsHealthy() bool
}
