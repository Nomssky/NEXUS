package modelrouter

import (
	"sync"
	"time"
)

// HealthStatus represents the health of a provider.
type HealthStatus struct {
	ProviderID   string         `json:"provider_id"`
	Status       ProviderStatus `json:"status"`
	LastCheck    time.Time      `json:"last_check"`
	LatencyMs    int64          `json:"latency_ms"`
	ErrorCount   int            `json:"error_count"`
	SuccessCount int            `json:"success_count"`
}

// HealthRegistry monitors provider health.
type HealthRegistry struct {
	statuses map[string]*HealthStatus
	mu       sync.RWMutex
	now      func() time.Time
}

// NewHealthRegistry creates a new health registry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		statuses: make(map[string]*HealthStatus),
		now:      time.Now,
	}
}

// NewHealthRegistryWithClock creates a new health registry with an injectable clock.
func NewHealthRegistryWithClock(now func() time.Time) *HealthRegistry {
	return &HealthRegistry{
		statuses: make(map[string]*HealthStatus),
		now:      now,
	}
}

// RecordSuccess records a successful invocation.
func (hr *HealthRegistry) RecordSuccess(providerID string, latencyMs int64) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	now := hr.now()
	status, ok := hr.statuses[providerID]
	if !ok {
		status = &HealthStatus{ProviderID: providerID}
		hr.statuses[providerID] = status
	}

	status.SuccessCount++
	status.LastCheck = now
	status.LatencyMs = latencyMs
	status.Status = ProviderStatusHealthy
}

// RecordFailure records a failed invocation.
func (hr *HealthRegistry) RecordFailure(providerID string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	now := hr.now()
	status, ok := hr.statuses[providerID]
	if !ok {
		status = &HealthStatus{ProviderID: providerID}
		hr.statuses[providerID] = status
	}

	status.ErrorCount++
	status.LastCheck = now

	// Mark unhealthy after 3 consecutive failures
	if status.ErrorCount >= 3 {
		status.Status = ProviderStatusUnhealthy
	} else if status.ErrorCount >= 1 {
		status.Status = ProviderStatusDegraded
	}
}

// GetStatus returns the health status of a provider.
func (hr *HealthRegistry) GetStatus(providerID string) (*HealthStatus, bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	status, ok := hr.statuses[providerID]
	return status, ok
}

// IsHealthy returns true if the provider is healthy.
func (hr *HealthRegistry) IsHealthy(providerID string) bool {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	status, ok := hr.statuses[providerID]
	if !ok {
		return true // unknown providers are assumed healthy
	}
	return status.Status == ProviderStatusHealthy
}
