// Package tool implements the NEXUS minimal Tool Runtime & Capability (C13).
//
// C13 is the security-controlled execution boundary between agents and
// external capabilities. All tool execution goes through validation,
// authorization, scope check, and audit.
//
// M5 implements a minimal READ-ONLY tool interface and registry.
// Full tool runtime (sandboxing, credential resolution, rate limiting)
// is future scope.
//
// The canonical flow is:
//
//	Agent → Tool Request → Validate → Authorize → Scope Check → Execute → Audit → Agent
//
// Agents never interact with external capabilities directly.
package tool

import (
	"fmt"
	"time"
)

// ToolCategory classifies the type of tool.
type ToolCategory string

const (
	ToolCategoryRead    ToolCategory = "read"
	ToolCategoryWrite   ToolCategory = "write"
	ToolCategoryCompute ToolCategory = "compute"
	ToolCategoryNetwork ToolCategory = "network"
)

// RiskLevel indicates the risk of a tool operation.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

// ToolDefinition is the blueprint for a tool.
// It declares capabilities, permissions, and constraints.
type ToolDefinition struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Category    ToolCategory `json:"category"`
	RiskLevel   RiskLevel    `json:"risk_level"`
	// ReadOnly indicates this tool only reads data (no side effects).
	ReadOnly bool `json:"read_only"`
	// RequiredCapabilities are the capabilities needed to use this tool.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	// RequiredPermissions are the permissions needed to use this tool.
	RequiredPermissions []string `json:"required_permissions,omitempty"`
	// Timeout is the maximum execution time.
	Timeout time.Duration `json:"timeout"`
	// BusinessIDs limits which businesses can use this tool (nil = all).
	BusinessIDs []string `json:"business_ids,omitempty"`
}

// ToolRequest represents a request to execute a tool.
type ToolRequest struct {
	ID         string            `json:"id"`
	ToolID     string            `json:"tool_id"`
	AgentID    string            `json:"agent_id"`
	BusinessID string            `json:"business_id"`
	Input      map[string]string `json:"input,omitempty"`
	RequestAt  time.Time         `json:"request_at"`
}

// ToolResponse represents the result of a tool execution.
type ToolResponse struct {
	RequestID  string            `json:"request_id"`
	ToolID     string            `json:"tool_id"`
	Status     string            `json:"status"` // "success", "denied", "error"
	Output     map[string]string `json:"output,omitempty"`
	Error      string            `json:"error,omitempty"`
	ExecutedAt time.Time         `json:"executed_at"`
	AuditID    string            `json:"audit_id,omitempty"`
}

// ToolRegistry manages tool definitions and execution.
// It validates and authorizes tool requests before execution.
type ToolRegistry struct {
	tools map[string]*ToolDefinition
	now   func() time.Time
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*ToolDefinition),
		now:   time.Now,
	}
}

// NewToolRegistryWithClock creates a new tool registry with an injectable clock.
func NewToolRegistryWithClock(now func() time.Time) *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*ToolDefinition),
		now:   now,
	}
}

// RegisterTool adds a tool definition to the registry.
func (tr *ToolRegistry) RegisterTool(def *ToolDefinition) error {
	if def.ID == "" {
		return fmt.Errorf("tool ID is required")
	}
	if def.ReadOnly && def.Category != ToolCategoryRead {
		return fmt.Errorf("read-only tools must have read category")
	}

	tr.tools[def.ID] = def
	return nil
}

// ValidateRequest validates a tool request against the tool definition.
func (tr *ToolRegistry) ValidateRequest(req *ToolRequest) error {
	def, ok := tr.tools[req.ToolID]
	if !ok {
		return fmt.Errorf("tool %s not found", req.ToolID)
	}

	// Business isolation check
	if len(def.BusinessIDs) > 0 {
		found := false
		for _, bid := range def.BusinessIDs {
			if bid == req.BusinessID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("tool %s not available for business %s", req.ToolID, req.BusinessID)
		}
	}

	return nil
}

// ExecuteTool validates and executes a tool request.
// For M5, this only supports read-only tools.
func (tr *ToolRegistry) ExecuteTool(req *ToolRequest) (*ToolResponse, error) {
	def, ok := tr.tools[req.ToolID]
	if !ok {
		return nil, fmt.Errorf("tool %s not found", req.ToolID)
	}

	// Validate request
	if err := tr.ValidateRequest(req); err != nil {
		return &ToolResponse{
			RequestID:  req.ID,
			ToolID:     req.ToolID,
			Status:     "denied",
			Error:      err.Error(),
			ExecutedAt: tr.now(),
		}, nil
	}

	// M5: only read-only tools are supported
	if !def.ReadOnly {
		return &ToolResponse{
			RequestID:  req.ID,
			ToolID:     req.ToolID,
			Status:     "denied",
			Error:      "only read-only tools are supported in M5",
			ExecutedAt: tr.now(),
		}, nil
	}

	// Execute (in a real implementation, this would call the actual tool)
	now := tr.now()
	resp := &ToolResponse{
		RequestID:  req.ID,
		ToolID:     req.ToolID,
		Status:     "success",
		Output:     make(map[string]string),
		ExecutedAt: now,
		AuditID:    fmt.Sprintf("audit-%d", now.UnixNano()),
	}

	return resp, nil
}

// GetTool returns a tool definition by ID.
func (tr *ToolRegistry) GetTool(toolID string) (*ToolDefinition, bool) {
	def, ok := tr.tools[toolID]
	return def, ok
}

// ToolCount returns the total number of registered tools.
func (tr *ToolRegistry) ToolCount() int {
	return len(tr.tools)
}
