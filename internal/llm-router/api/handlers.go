package api

import (
	"io"
	"net/http"

	"workspace-engine/internal/llm-router/models"
	"workspace-engine/internal/llm-router/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	router *service.RouterService
}

func NewHandler(router *service.RouterService) *Handler {
	return &Handler{
		router: router,
	}
}

func (h *Handler) RoutePrompt(c *gin.Context) {
	var req models.RouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorResponse(
			c, http.StatusBadRequest, models.NewErrorResponse(
				"INVALID_REQUEST",
				"Invalid request body",
				err.Error(),
			),
		)
		return
	}

	resp, err := h.router.Route(c.Request.Context(), req)
	if err != nil {
		ErrorResponse(
			c, http.StatusInternalServerError, models.NewErrorResponse(
				"ROUTING_ERROR",
				"Failed to route request",
				err.Error(),
			),
		)
		return
	}

	SuccessResponse(c, http.StatusOK, resp)
}

func (h *Handler) GetModels(c *gin.Context) {
	availableModels := h.router.GetAvailableModels()
	c.JSON(http.StatusOK, availableModels)
}

func (h *Handler) GetHealth(c *gin.Context) {
	health := h.router.GetHealth()
	c.JSON(http.StatusOK, health)
}

func (h *Handler) StreamRoutePrompt(c *gin.Context) {
	var req models.RouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Set headers for SSE
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	streamChan, err := h.router.RouteStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Stream(
		func(w io.Writer) bool {
			if msg, ok := <-streamChan; ok {
				if msg.Error != nil {
					c.SSEvent("error", msg.Error.Error())
					return false
				}
				c.SSEvent("message", msg)
				return !msg.Done
			}
			return false
		},
	)
}
