package tool

import (
	"testing"
)

// TEST-M5-032: Register tool
func TestToolRegister(t *testing.T) {
	tr := NewToolRegistry()
	def := &ToolDefinition{
		ID:        "tool-1",
		Name:      "Read File",
		Category:  ToolCategoryRead,
		ReadOnly:  true,
		RiskLevel: RiskLevelLow,
	}

	err := tr.RegisterTool(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.ToolCount() != 1 {
		t.Errorf("expected 1 tool, got %d", tr.ToolCount())
	}
}

// TEST-M5-033: Execute read-only tool
func TestToolExecuteReadOnly(t *testing.T) {
	tr := NewToolRegistry()
	tr.RegisterTool(&ToolDefinition{
		ID:        "tool-1",
		Name:      "Read File",
		Category:  ToolCategoryRead,
		ReadOnly:  true,
		RiskLevel: RiskLevelLow,
	})

	req := &ToolRequest{
		ID:         "req-1",
		ToolID:     "tool-1",
		AgentID:    "agent-1",
		BusinessID: "biz-1",
	}

	resp, err := tr.ExecuteTool(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected success, got %v", resp.Status)
	}
	if resp.AuditID == "" {
		t.Error("expected audit ID")
	}
}

// TEST-M5-034: Deny non-read-only tool
func TestToolDenyNonReadOnly(t *testing.T) {
	tr := NewToolRegistry()
	tr.RegisterTool(&ToolDefinition{
		ID:        "tool-1",
		Name:      "Write File",
		Category:  ToolCategoryWrite,
		ReadOnly:  false,
		RiskLevel: RiskLevelMedium,
	})

	req := &ToolRequest{
		ID:         "req-1",
		ToolID:     "tool-1",
		AgentID:    "agent-1",
		BusinessID: "biz-1",
	}

	resp, err := tr.ExecuteTool(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "denied" {
		t.Errorf("expected denied, got %v", resp.Status)
	}
}

// TEST-M5-035: Business isolation in tool
func TestToolBusinessIsolation(t *testing.T) {
	tr := NewToolRegistry()
	tr.RegisterTool(&ToolDefinition{
		ID:          "tool-1",
		Name:        "Read File",
		Category:    ToolCategoryRead,
		ReadOnly:    true,
		RiskLevel:   RiskLevelLow,
		BusinessIDs: []string{"biz-1"},
	})

	req := &ToolRequest{
		ID:         "req-1",
		ToolID:     "tool-1",
		AgentID:    "agent-1",
		BusinessID: "biz-2", // wrong business
	}

	resp, err := tr.ExecuteTool(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "denied" {
		t.Errorf("expected denied for wrong business, got %v", resp.Status)
	}
}

// TEST-M5-036: Tool not found
func TestToolNotFound(t *testing.T) {
	tr := NewToolRegistry()

	req := &ToolRequest{
		ID:         "req-1",
		ToolID:     "nonexistent",
		AgentID:    "agent-1",
		BusinessID: "biz-1",
	}

	_, err := tr.ExecuteTool(req)
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

// TEST-M5-037: Tool categories
func TestToolCategories(t *testing.T) {
	categories := []ToolCategory{ToolCategoryRead, ToolCategoryWrite, ToolCategoryCompute, ToolCategoryNetwork}
	if len(categories) != 4 {
		t.Errorf("expected 4 tool categories, got %d", len(categories))
	}
}

// TEST-M5-038: Risk levels
func TestToolRiskLevels(t *testing.T) {
	levels := []RiskLevel{RiskLevelLow, RiskLevelMedium, RiskLevelHigh, RiskLevelCritical}
	if len(levels) != 4 {
		t.Errorf("expected 4 risk levels, got %d", len(levels))
	}
}
