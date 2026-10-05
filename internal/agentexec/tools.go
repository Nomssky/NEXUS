package agentexec

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// deterministic example tools: echo / calculator / transform. The tool
// boundary idea: agentexec never executes arbitrary host code. An
// agent's AllowedTools ⊆ registry. These are enough to prove the
// registry's validation + allowlist + collision behavior.

// BuiltinExecutor returns the deterministic tool executor map keyed by
// tool id, paired with the registry definitions in registerBuiltins.
func BuiltinExecutor() map[string]func(ctx context.Context, input map[string]string) (map[string]string, error) {
	return map[string]func(ctx context.Context, input map[string]string) (map[string]string, error){
		"echo": func(_ context.Context, input map[string]string) (map[string]string, error) {
			return map[string]string{"echoed": input["text"]}, nil
		},
		"calculator": func(_ context.Context, input map[string]string) (map[string]string, error) {
			a, err := strconv.ParseFloat(strings.TrimSpace(input["a"]), 64)
			if err != nil {
				return nil, fmt.Errorf("calculator: invalid a")
			}
			b, err := strconv.ParseFloat(strings.TrimSpace(input["b"]), 64)
			if err != nil {
				return nil, fmt.Errorf("calculator: invalid b")
			}
			var out string
			switch input["op"] {
			case "add":
				out = strconv.FormatFloat(a+b, 'f', -1, 64)
			case "sub":
				out = strconv.FormatFloat(a-b, 'f', -1, 64)
			case "mul":
				out = strconv.FormatFloat(a*b, 'f', -1, 64)
			case "div":
				if b == 0 {
					return nil, fmt.Errorf("calculator: division by zero")
				}
				out = strconv.FormatFloat(a/b, 'f', -1, 64)
			default:
				return nil, fmt.Errorf("calculator: unsupported op %q", input["op"])
			}
			return map[string]string{"result": out}, nil
		},
		"transform": func(_ context.Context, input map[string]string) (map[string]string, error) {
			// Deterministic structured-data transform: upper/lower/reverse
			// of input["text"] per input["op"].
			text := input["text"]
			switch input["op"] {
			case "upper":
				return map[string]string{"transformed": strings.ToUpper(text)}, nil
			case "lower":
				return map[string]string{"transformed": strings.ToLower(text)}, nil
			case "reverse":
				b := []rune(text)
				for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
					b[i], b[j] = b[j], b[i]
				}
				return map[string]string{"transformed": string(b)}, nil
			default:
				return nil, fmt.Errorf("transform: unsupported op %q", input["op"])
			}
		},
	}
}

// registerBuiltins seeds the registry with the deterministic example tool
// definitions when they are not already there (idempotent by id).
//
// All three are pure functions with no side effects, so they are registered
// under the READ category: the tool registry only accepts read-only tools in
// the read category (ToolRegistry.RegisterTool) and only read tools are
// executable on the M5 path. A pure computation therefore belongs here rather
// than in a write/compute category it may never execute from. Definitions are
// known-good, so a registration error can only mean a duplicate id (skipped
// above).
func registerBuiltins(tr *tool.ToolRegistry) {
	defs := []*tool.ToolDefinition{
		{ID: "echo", Name: "Echo", Version: "v1", Category: tool.ToolCategoryRead, RiskLevel: tool.RiskLevelLow, ReadOnly: true, Timeout: 2 * time.Second, Description: "Echo the input text back."},
		{ID: "calculator", Name: "Calculator", Version: "v1", Category: tool.ToolCategoryRead, RiskLevel: tool.RiskLevelLow, ReadOnly: true, Timeout: 2 * time.Second, Description: "Deterministic arithmetic: op in add|sub|mul|div."},
		{ID: "transform", Name: "Transform", Version: "v1", Category: tool.ToolCategoryRead, RiskLevel: tool.RiskLevelLow, ReadOnly: true, Timeout: 2 * time.Second, Description: "Deterministic string transform: op in upper|lower|reverse."},
	}
	for _, d := range defs {
		if _, ok := tr.GetTool(d.ID); !ok {
			_ = tr.RegisterTool(d)
		}
	}
}
