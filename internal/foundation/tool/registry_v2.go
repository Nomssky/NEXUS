package tool

// Tool Registry v2 (contracts/CAPABILITY_TOOL_CONTRACTS.md §5 step 1): the
// *same* registry as v1, extended with manifest validation, adapter binding and
// deterministic listing. There is no second registry: v1 definitions keep
// working through RegisterTool, and their manifests are derived from the
// v1 fields.

import (
	"context"
	"fmt"
	"sort"
)

// registration binds a validated manifest to its adapter.
type registration struct {
	manifest ToolManifest
	adapter  Adapter
}

// Register validates the manifest, binds the adapter and registers the tool.
// It fails closed: malformed manifest, unsupported class, duplicate id or an
// adapter whose operations disagree with the manifest are all rejected and
// nothing is stored.
func (tr *ToolRegistry) Register(manifest ToolManifest, adapter Adapter) error {
	if adapter == nil {
		return fmt.Errorf("tool %s: adapter is required", manifest.ID)
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	ops := append([]string(nil), adapter.Operations()...)
	sort.Strings(ops)
	declared := manifest.SortedOperations()
	if len(ops) != len(declared) {
		return fmt.Errorf("tool %s: adapter operations %v do not match the manifest %v", manifest.ID, ops, declared)
	}
	for i := range ops {
		if ops[i] != declared[i] {
			return fmt.Errorf("tool %s: adapter operations %v do not match the manifest %v", manifest.ID, ops, declared)
		}
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if _, exists := tr.manifests[manifest.ID]; exists {
		return fmt.Errorf("tool %s: already registered", manifest.ID)
	}
	tr.manifests[manifest.ID] = registration{manifest: manifest, adapter: adapter}
	// Keep the v1 definition index in sync so agent-registry allowlist
	// validation (GetTool) and the execution scope resolver resolve v2 tools
	// exactly like the retained builtin definitions.
	if _, ok := tr.tools[manifest.ID]; !ok {
		tr.tools[manifest.ID] = &ToolDefinition{
			ID: manifest.ID, Name: manifest.Name, Version: manifest.Version,
			Category: manifest.Category, RiskLevel: RiskLevelMedium,
			ReadOnly:    manifest.SideEffectClass == SideEffectRead,
			Timeout:     manifest.ResourceLimits.MaxDuration,
			Description: manifest.Description,
		}
	}
	return nil
}

// Manifest returns a registered manifest.
func (tr *ToolRegistry) Manifest(toolID string) (ToolManifest, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	reg, ok := tr.manifests[toolID]
	if !ok {
		return ToolManifest{}, false
	}
	return reg.manifest, true
}

// Adapter returns the bound adapter for a tool id.
func (tr *ToolRegistry) Adapter(toolID string) (Adapter, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	reg, ok := tr.manifests[toolID]
	if !ok {
		return nil, false
	}
	return reg.adapter, true
}

// ListManifests returns every registered manifest sorted by id (deterministic).
func (tr *ToolRegistry) ListManifests() []ToolManifest {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	out := make([]ToolManifest, 0, len(tr.manifests))
	for _, reg := range tr.manifests {
		out = append(out, reg.manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// deriveManifestFromDefinition turns a v1 ToolDefinition into a manifest so the
// three shipped tools (echo, calculator, transform) keep working unchanged while
// participating in manifest validation and catalog discovery (contract §2).
func deriveManifestFromDefinition(def *ToolDefinition) ToolManifest {
	side := SideEffectRead
	if !def.ReadOnly {
		side = SideEffectWrite
	}
	ops := []string{"execute"}
	manifest := ToolManifest{
		ID:                 def.ID,
		Version:            def.Version,
		Name:               def.Name,
		Description:        def.Description,
		Category:           def.Category,
		SideEffectClass:    side,
		NetworkRequirement: NetworkNone,
		ScopeRequirement:   ScopeBusiness,
		SecurityClass:      SecuritySandboxed,
		Operations:         ops,
		ResourceLimits:     ResourceLimits{MaxDuration: def.Timeout},
		InputSchema:        builtinInputSchema(def.ID),
		OutputSchema: Schema{Fields: []SchemaField{
			{Name: "output", Type: FieldString, Required: false, MaxLength: 65536},
			{Name: "echoed", Type: FieldString, MaxLength: 65536},
			{Name: "result", Type: FieldString, MaxLength: 65536},
			{Name: "transformed", Type: FieldString, MaxLength: 65536},
		}},
	}
	if manifest.Version == "" {
		manifest.Version = "1.0.0"
	}
	if manifest.Name == "" {
		manifest.Name = def.ID
	}
	return manifest
}

// builtinAdapter binds a v1 executor function as a v2 adapter so the shipped
// deterministic tools run through the same registry v2 stores them in.
type builtinAdapter struct {
	id  string
	fn  func(ctx context.Context, input map[string]string) (map[string]string, error)
	ops []string
}

func (b *builtinAdapter) Operations() []string { return b.ops }

func (b *builtinAdapter) Invoke(ctx context.Context, inv Invocation) (RawResult, error) {
	if err := ctx.Err(); err != nil {
		return RawResult{}, err
	}
	out, err := b.fn(ctx, inv.Input)
	if err != nil {
		return RawResult{}, err
	}
	return RawResult{Result: out}, nil
}

// RegisterBuiltin wires a deterministic executor as a v2 tool (used by the
// shipped echo/calculator/transform catalogue).
func (tr *ToolRegistry) RegisterBuiltin(def ToolDefinition, fn func(ctx context.Context, input map[string]string) (map[string]string, error)) error {
	cp := def
	manifest := deriveManifestFromDefinition(&cp)
	tr.mu.Lock()
	tr.tools[def.ID] = &cp
	tr.mu.Unlock()
	return tr.Register(manifest, &builtinAdapter{id: def.ID, fn: fn, ops: manifest.Operations})
}

// builtinInputSchema describes the deterministic builtin tool inputs.
func builtinInputSchema(id string) Schema {
	switch id {
	case "echo":
		return Schema{Fields: []SchemaField{{Name: "text", Type: FieldString, MaxLength: 4096}}}
	case "calculator":
		return Schema{Fields: []SchemaField{
			{Name: "a", Type: FieldString, MaxLength: 64},
			{Name: "b", Type: FieldString, MaxLength: 64},
			{Name: "op", Type: FieldString, Enum: []string{"add", "sub", "mul", "div"}, MaxLength: 8},
		}}
	case "transform":
		return Schema{Fields: []SchemaField{
			{Name: "text", Type: FieldString, MaxLength: 4096},
			{Name: "op", Type: FieldString, Enum: []string{"upper", "lower", "reverse"}, MaxLength: 8},
		}}
	default:
		return Schema{Fields: []SchemaField{{Name: "input", Type: FieldString, MaxLength: 4096}}}
	}
}
