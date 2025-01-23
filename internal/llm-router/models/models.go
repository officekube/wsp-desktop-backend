package models

type RouteRequest struct {
	Prompt         string                 `json:"prompt"`
	PreferredModel string                 `json:"preferredModel,omitempty"`
	Parameters     map[string]interface{} `json:"parameters,omitempty"`
	Context        RequestContext         `json:"context,omitempty"`
}

type RequestContext struct {
	Priority string `json:"priority,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
}

type RouteResponse struct {
	ID       string                 `json:"id"`
	Result   string                 `json:"result"`
	Model    string                 `json:"model"`
	Usage    Usage                  `json:"usage"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type ModelInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	Capabilities []string `json:"capabilities"`
	MaxTokens    int      `json:"maxTokens"`
	Pricing      Pricing  `json:"pricing"`
}

type Pricing struct {
	InputPrice  float64 `json:"inputPrice"`
	OutputPrice float64 `json:"outputPrice"`
	Currency    string  `json:"currency"`
}

type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Models    map[string]ModelStatus `json:"models"`
}

type ModelStatus struct {
	Status  string `json:"status"`
	Latency int    `json:"latency"`
}

// StreamResponse represents a streaming response chunk
type StreamResponse struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`
	Done    bool   `json:"done"`
	Error   error  `json:"error,omitempty"`
}

type ErrorResponse struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func NewErrorResponse(code string, message string, details interface{}) ErrorResponse {
	return ErrorResponse{
		Code:    code,
		Message: message,
		Details: details,
	}
}
