// Package modelrouter implements the NEXUS Model Router & Provider Abstraction (C12).
//
// The Model Router determines which AI model is used, through which provider,
// with what configuration, and when to switch models/providers. Agents request
// AI capabilities; the Router selects the best provider/model based on needs,
// policy, resources, and runtime conditions.
//
// Key invariants:
//   - Routing ≠ Authorization: routing does not grant authority
//   - Local-first: prefer local models when they meet needs
//   - Failover without unsafe duplicates
//   - Privacy-aware routing
//   - Provider credential isolation
//
// The locked conceptual flow is:
//
//	Agent → Model Router → Provider Adapter → Native Provider API → Normalized Response
package modelrouter

import (
	"fmt"
	"sync"
)

// ModelCapability represents a capability of a model.
type ModelCapability string

const (
	CapabilityReasoning        ModelCapability = "reasoning"
	CapabilityCoding           ModelCapability = "coding"
	CapabilityToolCalling      ModelCapability = "tool_calling"
	CapabilityStructuredOutput ModelCapability = "structured_output"
	CapabilityVision           ModelCapability = "vision"
	CapabilityAudio            ModelCapability = "audio"
	CapabilityEmbedding        ModelCapability = "embedding"
	CapabilityLongContext      ModelCapability = "long_context"
)

// ModelStatus indicates whether a model is available.
type ModelStatus string

const (
	ModelStatusActive   ModelStatus = "active"
	ModelStatusDegraded ModelStatus = "degraded"
	ModelStatusOffline  ModelStatus = "offline"
)

// RuntimeType indicates where the model runs.
type RuntimeType string

const (
	RuntimeLocal  RuntimeType = "local"
	RuntimeRemote RuntimeType = "remote"
)

// ModelDefinition is the blueprint for a model in the registry.
type ModelDefinition struct {
	ID               string            `json:"id"`
	ProviderID       string            `json:"provider_id"`
	DisplayName      string            `json:"display_name"`
	Version          string            `json:"version"`
	Capabilities     []ModelCapability `json:"capabilities"`
	ContextWindow    int               `json:"context_window"`
	MaxOutputTokens  int               `json:"max_output_tokens"`
	Status           ModelStatus       `json:"status"`
	Runtime          RuntimeType       `json:"runtime"`
	PricingInput     float64           `json:"pricing_input"`     // per token
	PricingOutput    float64           `json:"pricing_output"`    // per token
	DataRetention    string            `json:"data_retention"`    // "none", "standard", "extended"
	ExternalTransfer bool              `json:"external_transfer"` // allowed to send data externally
	Tags             []string          `json:"tags,omitempty"`
}

// ModelRegistry manages model definitions and provides capability queries.
type ModelRegistry struct {
	models map[string]*ModelDefinition
	mu     sync.RWMutex
}

// NewModelRegistry creates a new model registry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		models: make(map[string]*ModelDefinition),
	}
}

// RegisterModel adds a model to the registry.
func (mr *ModelRegistry) RegisterModel(def *ModelDefinition) error {
	if def.ID == "" {
		return fmt.Errorf("model ID is required")
	}
	if def.ProviderID == "" {
		return fmt.Errorf("provider ID is required")
	}

	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.models[def.ID] = def
	return nil
}

// GetModel returns a model by ID.
func (mr *ModelRegistry) GetModel(modelID string) (*ModelDefinition, bool) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	def, ok := mr.models[modelID]
	return def, ok
}

// FindByCapabilities returns models that have all required capabilities.
func (mr *ModelRegistry) FindByCapabilities(required []ModelCapability) []*ModelDefinition {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	var result []*ModelDefinition
	for _, def := range mr.models {
		if def.Status == ModelStatusOffline {
			continue
		}
		if def.hasAllCapabilities(required) {
			result = append(result, def)
		}
	}
	return result
}

// FindByRuntime returns models matching the runtime type.
func (mr *ModelRegistry) FindByRuntime(runtime RuntimeType) []*ModelDefinition {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	var result []*ModelDefinition
	for _, def := range mr.models {
		if def.Runtime == runtime && def.Status != ModelStatusOffline {
			result = append(result, def)
		}
	}
	return result
}

// ModelCount returns the total number of registered models.
func (mr *ModelRegistry) ModelCount() int {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return len(mr.models)
}

func (def *ModelDefinition) hasAllCapabilities(required []ModelCapability) bool {
	caps := make(map[ModelCapability]bool, len(def.Capabilities))
	for _, c := range def.Capabilities {
		caps[c] = true
	}
	for _, r := range required {
		if !caps[r] {
			return false
		}
	}
	return true
}
