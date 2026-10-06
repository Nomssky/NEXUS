package tool

// Adapter seam and the bounded result contract (contracts/CAPABILITY_TOOL_CONTRACTS
// §5, §7). Adapters receive already-authorized, schema-validated input and an
// opaque credential handle: they never see identity, membership, policy or the
// runtime's authority decisions.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseInt(v string) (int64, error) { return strconv.ParseInt(strings.TrimSpace(v), 10, 64) }
func parseFloat(v string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(v), 64)
}

// CredentialHandle is the opaque credential reference handed to an adapter. The
// raw secret is reachable only through the resolver's own accessor inside the
// adapter call, and is never serialized, logged or returned (contract §6).
type CredentialHandle struct {
	// Reference is the safe, loggable reference (never a value).
	Reference string `json:"reference,omitempty"`
	// Present reports whether a secret was resolved.
	Present bool `json:"present"`
	// resolve is the in-process accessor; it is unexported so a handle cannot
	// be serialized into a result, observation or event.
	resolve func() (string, error)
}

// Secret returns the raw secret for adapter use only. It is deliberately
// hard to leak: callers must hold a handle, and the intelligence layer never
// receives one.
func (h CredentialHandle) Secret() (string, error) {
	if !h.Present || h.resolve == nil {
		return "", fmt.Errorf("credential unavailable")
	}
	return h.resolve()
}

// NewCredentialHandle builds a handle for an adapter (internal use).
func NewCredentialHandle(reference string, resolve func() (string, error)) CredentialHandle {
	return CredentialHandle{Reference: reference, Present: resolve != nil, resolve: resolve}
}

// Invocation is one authorized request to an adapter.
type Invocation struct {
	ToolID     string
	Operation  string
	Input      map[string]string
	BusinessID string
	DivisionID string
	ActorID    string
	AgentID    string
	Credential CredentialHandle
	Limits     ResourceLimits
}

// Adapter is a registered capability implementation.
type Adapter interface {
	// Operations must match the manifest's supported_operations exactly.
	Operations() []string
	// Invoke performs the operation. Implementations must honour ctx.
	Invoke(ctx context.Context, inv Invocation) (RawResult, error)
}

// RawResult is an adapter's unnormalized output. It is bounded *and redacted*
// by the platform before it can reach a model, observation, event or response
// (contract §7, §36).
type RawResult struct {
	Result   map[string]string
	Metadata map[string]string
	Headers  map[string]string
}

// Result is the canonical, bounded, redacted tool result (contract §7).
type Result struct {
	ToolID    string            `json:"tool_id"`
	Operation string            `json:"operation,omitempty"`
	Status    string            `json:"status"` // success | denied | error
	Result    map[string]string `json:"result,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Error     string            `json:"error,omitempty"`
	Duration  time.Duration     `json:"duration"`
	Truncated bool              `json:"truncated,omitempty"`
	// Bytes is the measured size of the result payload after bounds.
	Bytes int `json:"bytes"`
}

// Result status values.
const (
	StatusSuccess = "success"
	StatusDenied  = "denied"
	StatusError   = "error"
)
