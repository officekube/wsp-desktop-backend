package service

import (
	"context"
	"errors"

	"workspace-engine/internal/llm-router/models"
	"workspace-engine/internal/llm-router/service/llm"
)

type RouterService struct {
	providers map[string]llm.Provider
}

func NewRouterService(providers map[string]llm.Provider) *RouterService {
	return &RouterService{
		providers: providers,
	}
}

func (s *RouterService) Route(ctx context.Context, req models.RouteRequest) (*models.RouteResponse, error) {
	provider := s.selectProvider(req)
	if provider == nil {
		return nil, errors.New("no suitable provider found")
	}

	return provider.Generate(ctx, req.Prompt, req.Parameters)
}

func (s *RouterService) RouteStream(ctx context.Context, req models.RouteRequest) (
	<-chan models.StreamResponse, error,
) {
	provider := s.selectProvider(req)
	if provider == nil {
		return nil, errors.New("no suitable provider found")
	}

	return provider.GenerateStream(ctx, req.Prompt, req.Parameters)
}

func (s *RouterService) selectProvider(req models.RouteRequest) llm.Provider {
	if req.PreferredModel != "" {
		if provider, ok := s.providers[req.PreferredModel]; ok {
			return provider
		}
	}

	// TODO: Implement provider selection logic based on requirements
	// This is a simplified version
	for _, provider := range s.providers {
		if provider.IsHealthy() {
			return provider
		}
	}

	return nil
}

func (s *RouterService) GetAvailableModels() []models.ModelInfo {
	var infos []models.ModelInfo
	for _, provider := range s.providers {
		infos = append(infos, provider.GetModelInfo())
	}
	return infos
}

func (s *RouterService) GetHealth() models.HealthStatus {
	status := models.HealthStatus{
		Models: make(map[string]models.ModelStatus),
	}

	allHealthy := true
	for _, provider := range s.providers {
		info := provider.GetModelInfo()
		isHealthy := provider.IsHealthy()
		if !isHealthy {
			allHealthy = false
		}

		status.Models[info.ID] = models.ModelStatus{
			Status:  map[bool]string{true: "available", false: "unavailable"}[isHealthy],
			Latency: 0, //TODO: Implement actual latency measurement
		}
	}

	status.Status = map[bool]string{true: "healthy", false: "degraded"}[allHealthy]
	return status
}
