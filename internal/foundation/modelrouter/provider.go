package modelrouter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProviderStatus indicates whether a provider is available.
type ProviderStatus string

const (
	ProviderStatusHealthy   ProviderStatus = "healthy"
	ProviderStatusDegraded  ProviderStatus = "degraded"
	ProviderStatusUnhealthy ProviderStatus = "unhealthy"
	ProviderStatusOffline   ProviderStatus = "offline"
)

// ProviderConfig holds configuration for a provider.
type ProviderConfig struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Endpoint   string            `json:"endpoint,omitempty"`
	APIKey     string            `json:"api_key,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Timeout    time.Duration     `json:"timeout"`
	MaxRetries int               `json:"max_retries"`
}

// Provider is the interface that all model providers must implement.
// It abstracts the native provider API behind a consistent interface.
type Provider interface {
	// Identify returns the provider ID.
	Identify() string
	// HealthCheck verifies the provider is available.
	HealthCheck() error
	// ListModels returns the models available from this provider.
	ListModels() ([]string, error)
	// Invoke sends a generation request and returns the response.
	// The context propagates the caller's cancellation/deadline to the
	// provider call (E-005): implementations must honor ctx.
	Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
}

// GenerateRequest is a normalized request to a model provider.
type GenerateRequest struct {
	RequestID      string           `json:"request_id"`
	ModelID        string           `json:"model_id"`
	Messages       []Message        `json:"messages"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    float64          `json:"temperature,omitempty"`
	Tools          []ToolDefinition `json:"tools,omitempty"`
	Stream         bool             `json:"stream,omitempty"`
	ResponseFormat string           `json:"response_format,omitempty"`
}

// Message represents a conversation message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolDefinition represents a tool available to the model.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// GenerateResponse is a normalized response from a model provider.
type GenerateResponse struct {
	RequestID    string `json:"request_id"`
	ModelID      string `json:"model_id"`
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	LatencyMs    int64  `json:"latency_ms"`
}

// LocalProvider is a concrete provider for local inference (Ollama, etc).
type LocalProvider struct {
	config ProviderConfig
	status ProviderStatus
	mu     sync.RWMutex
}

// NewLocalProvider creates a new local provider.
func NewLocalProvider(config ProviderConfig) *LocalProvider {
	return &LocalProvider{
		config: config,
		status: ProviderStatusHealthy,
	}
}

func (lp *LocalProvider) Identify() string { return lp.config.ID }

func (lp *LocalProvider) HealthCheck() error {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	if lp.status == ProviderStatusOffline {
		return fmt.Errorf("provider %s is offline", lp.config.ID)
	}
	return nil
}

func (lp *LocalProvider) ListModels() ([]string, error) {
	return nil, nil // implemented by concrete runtime
}

func (lp *LocalProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lp.mu.RLock()
	if lp.status == ProviderStatusOffline {
		lp.mu.RUnlock()
		return nil, fmt.Errorf("provider %s is offline", lp.config.ID)
	}
	lp.mu.RUnlock()

	// In a real implementation, this would call Ollama/HF/etc
	return &GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		FinishReason: "stop",
	}, nil
}

// RemoteProvider is a concrete provider for remote APIs (OpenRouter, etc).
type RemoteProvider struct {
	config ProviderConfig
	status ProviderStatus
	mu     sync.RWMutex
}

// NewRemoteProvider creates a new remote provider.
func NewRemoteProvider(config ProviderConfig) *RemoteProvider {
	return &RemoteProvider{
		config: config,
		status: ProviderStatusHealthy,
	}
}

func (rp *RemoteProvider) Identify() string { return rp.config.ID }

func (rp *RemoteProvider) HealthCheck() error {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	if rp.status == ProviderStatusOffline {
		return fmt.Errorf("provider %s is offline", rp.config.ID)
	}
	return nil
}

func (rp *RemoteProvider) ListModels() ([]string, error) {
	return nil, nil // implemented by concrete runtime
}

func (rp *RemoteProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rp.mu.RLock()
	if rp.status == ProviderStatusOffline {
		rp.mu.RUnlock()
		return nil, fmt.Errorf("provider %s is offline", rp.config.ID)
	}
	rp.mu.RUnlock()

	// In a real implementation, this would call the remote API
	return &GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		FinishReason: "stop",
	}, nil
}
