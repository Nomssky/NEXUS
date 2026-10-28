package gateway

// Local runtime & model diagnostics (read-only). These endpoints expose the
// minimum an operator needs to understand model selection on this machine:
// whether a local runtime is configured, which models it reports as INSTALLED,
// a bounded hardware snapshot, the configured global preference, and the last
// routing decision. They never expose credentials, prompts, or provider
// response bodies, and they are identity-scoped like every other scoped API
// path. No mutation happens here.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ModelInfo is one locally installed model with best-effort metadata.
type ModelInfo struct {
	Name          string `json:"name"`
	Family        string `json:"family,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization_level,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
}

// HardwareInfo is a bounded, explicitly-flagged resource snapshot. Values are
// best-effort: MeasuredRAM/knowing flags make estimates distinguishable from
// observations.
type HardwareInfo struct {
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	CPUCores      int      `json:"cpu_cores"`
	RAMKnown      bool     `json:"ram_known"`
	TotalRAMBytes int64    `json:"total_ram_bytes,omitempty"`
	AvailRAMBytes int64    `json:"available_ram_bytes,omitempty"`
	GPUKnown      bool     `json:"gpu_known"`
	GPUCount      int      `json:"gpu_count"`
	CollectedAt   string   `json:"collected_at,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

// RoutingSelection is the last decision's user-facing explanation.
type RoutingSelection struct {
	ModelID      string   `json:"model_id,omitempty"`
	ProviderID   string   `json:"provider_id,omitempty"`
	Source       string   `json:"source,omitempty"`
	FallbackUsed bool     `json:"fallback_used"`
	Uncertain    bool     `json:"uncertain"`
	Explanation  string   `json:"explanation,omitempty"`
	Rejected     []string `json:"rejected,omitempty"`
}

// LocalRuntimeInfo is the full diagnostics payload.
type LocalRuntimeInfo struct {
	Configured       bool              `json:"local_runtime_configured"`
	ProviderID       string            `json:"provider_id,omitempty"`
	Endpoint         string            `json:"endpoint,omitempty"`
	Status           string            `json:"status"` // ready | unavailable | not_configured
	Error            string            `json:"error,omitempty"`
	Models           []ModelInfo       `json:"models,omitempty"`
	Hardware         HardwareInfo      `json:"hardware"`
	GlobalPreference string            `json:"global_preference,omitempty"`
	Selection        *RoutingSelection `json:"selection,omitempty"`
	ObservedAt       string            `json:"observed_at,omitempty"`
}

// WithLocalRuntime injects the diagnostics producer. A nil producer makes the
// endpoint report a not-configured state rather than fabricating data.
func WithLocalRuntime(fn func(ctx context.Context) LocalRuntimeInfo) ServerOption {
	return func(s *Server) { s.localRuntime = fn }
}

// handleLocalRuntime answers GET /api/v1/runtime/models.
func (s *Server) handleLocalRuntime(w http.ResponseWriter, r *http.Request) {
	info := LocalRuntimeInfo{Status: "not_configured", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	if s.localRuntime != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		info = s.localRuntime(ctx)
		if info.ObservedAt == "" {
			info.ObservedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(info)
}
