package tool

// Capability & Tool Platform v1 (contracts/CAPABILITY_TOOL_CONTRACTS.md §2, §3,
// §7, §8): manifest model, adapter seam and the bounded result contract. This
// file only declares types and validation — the single mediated invocation path
// lives in internal/capability so identity/scope/credential decisions stay in
// one place.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SideEffectClass is the security classification of a tool (contract §3).
type SideEffectClass string

const (
	SideEffectRead                       SideEffectClass = "read"
	SideEffectWrite                      SideEffectClass = "write"
	SideEffectExternalMutation           SideEffectClass = "external_mutation"
	SideEffectCredentialedExternalMutatn SideEffectClass = "credentialed_external_mutation"
)

var validSideEffects = map[SideEffectClass]struct{}{
	SideEffectRead: {}, SideEffectWrite: {}, SideEffectExternalMutation: {},
	SideEffectCredentialedExternalMutatn: {},
}

// RetryableClass reports whether automatic retry may ever be considered for an
// operation of this class. External mutations are never automatically retried
// (duplication risk); the v1 policy retries `read` operations only when the
// adapter marks them retryable (contract §3, §32/§33).
func (s SideEffectClass) RetryableClass() bool { return s == SideEffectRead }

// NetworkRequirement declares whether a tool needs outbound network access.
type NetworkRequirement string

const (
	NetworkNone         NetworkRequirement = "none"
	NetworkOutboundHTTP NetworkRequirement = "outbound_http"
)

var validNetwork = map[NetworkRequirement]struct{}{
	NetworkNone: {}, NetworkOutboundHTTP: {},
}

// SecurityClass declares the isolation class of the adapter.
type SecurityClass string

const (
	SecuritySandboxed    SecurityClass = "sandboxed"
	SecurityNetwork      SecurityClass = "network"
	SecurityCredentialed SecurityClass = "credentialed"
)

var validSecurity = map[SecurityClass]struct{}{
	SecuritySandboxed: {}, SecurityNetwork: {}, SecurityCredentialed: {},
}

// ScopeRequirement declares the runtime scope a tool needs (contract §2).
type ScopeRequirement string

const (
	ScopeBusiness  ScopeRequirement = "business"
	ScopeDivision  ScopeRequirement = "division"
	ScopeWorkspace ScopeRequirement = "workspace"
)

var validScopeReq = map[ScopeRequirement]struct{}{
	ScopeBusiness: {}, ScopeDivision: {}, ScopeWorkspace: {},
}

// FieldType is the closed set of schema field types.
type FieldType string

const (
	FieldString FieldType = "string"
	FieldInt    FieldType = "int"
	FieldFloat  FieldType = "float"
	FieldBool   FieldType = "bool"
)

var validFieldTypes = map[FieldType]struct{}{
	FieldString: {}, FieldInt: {}, FieldFloat: {}, FieldBool: {},
}

// SchemaField declares one input/output field. The v1 schema language is a
// closed, explicit field list: no expression language, no nested schemas.
type SchemaField struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Description string    `json:"description,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	MaxLength   int       `json:"max_length,omitempty"`
	Max         int64     `json:"max,omitempty"`
	Min         int64     `json:"min,omitempty"`
}

// Schema is a deterministic, closed field list.
type Schema struct {
	Fields []SchemaField `json:"fields"`
}

// Field returns the named field declaration.
func (s Schema) Field(name string) (SchemaField, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return SchemaField{}, false
}

// Names returns the field names in declaration order.
func (s Schema) Names() []string {
	out := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		out = append(out, f.Name)
	}
	return out
}

// ResourceLimits are the per-invocation bounds a manifest may request. The
// runtime clamps them to its own caps (contract §8); a tool can only ask for
// less.
type ResourceLimits struct {
	MaxDuration   time.Duration `json:"max_duration,omitempty"`
	MaxOutputByte int           `json:"max_output_bytes,omitempty"`
	MaxRequestByt int           `json:"max_request_bytes,omitempty"`
	MaxItems      int           `json:"max_items,omitempty"`
	MaxDepth      int           `json:"max_depth,omitempty"`
}

// CredentialRequirement declares the named credential a tool needs. The
// reference itself is resolved at the boundary; only its *name* and scope are
// part of the manifest (contract §6).
type CredentialRequirement struct {
	Required   bool   `json:"required"`
	Reference  string `json:"reference,omitempty"`
	BusinessID string `json:"business_id,omitempty"`
	DivisionID string `json:"division_id,omitempty"`
}

// ToolManifest is the machine-readable declaration of a capability (contract §2).
type ToolManifest struct {
	ID                    string                `json:"id"`
	Version               string                `json:"version"`
	Name                  string                `json:"name"`
	Description           string                `json:"description"`
	Category              ToolCategory          `json:"category"`
	InputSchema           Schema                `json:"input_schema"`
	OutputSchema          Schema                `json:"output_schema"`
	SideEffectClass       SideEffectClass       `json:"side_effect_class"`
	NetworkRequirement    NetworkRequirement    `json:"network_requirement"`
	CredentialRequirement CredentialRequirement `json:"credential_requirement"`
	ResourceLimits        ResourceLimits        `json:"resource_limits"`
	Operations            []string              `json:"supported_operations"`
	ScopeRequirement      ScopeRequirement      `json:"scope_requirement"`
	SecurityClass         SecurityClass         `json:"security_class"`
}

// Validate is the fail-closed manifest check (contract §2). It is called before
// registration and on hydration.
func (m ToolManifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("manifest: id is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("manifest %s: version is required", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest %s: name is required", m.ID)
	}
	switch m.Category {
	case ToolCategoryRead, ToolCategoryWrite, ToolCategoryCompute, ToolCategoryNetwork:
	default:
		return fmt.Errorf("manifest %s: unsupported category %q", m.ID, m.Category)
	}
	if _, ok := validSideEffects[m.SideEffectClass]; !ok {
		return fmt.Errorf("manifest %s: unsupported side_effect_class %q", m.ID, m.SideEffectClass)
	}
	if _, ok := validNetwork[m.NetworkRequirement]; !ok {
		return fmt.Errorf("manifest %s: unsupported network_requirement %q", m.ID, m.NetworkRequirement)
	}
	if _, ok := validSecurity[m.SecurityClass]; !ok {
		return fmt.Errorf("manifest %s: unsupported security_class %q", m.ID, m.SecurityClass)
	}
	if _, ok := validScopeReq[m.ScopeRequirement]; !ok {
		return fmt.Errorf("manifest %s: unsupported scope_requirement %q", m.ID, m.ScopeRequirement)
	}
	if err := validateSchema(m.ID+".input_schema", m.InputSchema); err != nil {
		return err
	}
	if err := validateSchema(m.ID+".output_schema", m.OutputSchema); err != nil {
		return err
	}
	if len(m.Operations) == 0 {
		return fmt.Errorf("manifest %s: supported_operations is required", m.ID)
	}
	seen := map[string]struct{}{}
	for _, op := range m.Operations {
		op = strings.TrimSpace(op)
		if op == "" {
			return fmt.Errorf("manifest %s: blank operation name", m.ID)
		}
		if _, dup := seen[op]; dup {
			return fmt.Errorf("manifest %s: duplicate operation %q", m.ID, op)
		}
		seen[op] = struct{}{}
	}
	if m.CredentialRequirement.Required {
		if strings.TrimSpace(m.CredentialRequirement.Reference) == "" {
			return fmt.Errorf("manifest %s: credential requirement needs a reference", m.ID)
		}
		if strings.TrimSpace(m.CredentialRequirement.BusinessID) == "" {
			return fmt.Errorf("manifest %s: credential requirement must be business scoped", m.ID)
		}
	}
	if m.NetworkRequirement == NetworkNone && m.SecurityClass == SecurityNetwork {
		return fmt.Errorf("manifest %s: network security class requires a network requirement", m.ID)
	}
	if err := m.ResourceLimits.validate(m.ID); err != nil {
		return err
	}
	return nil
}

func (r ResourceLimits) validate(id string) error {
	if r.MaxDuration < 0 || r.MaxOutputByte < 0 || r.MaxRequestByt < 0 || r.MaxItems < 0 || r.MaxDepth < 0 {
		return fmt.Errorf("manifest %s: resource limits must not be negative", id)
	}
	if r.MaxDuration > 0 && r.MaxDuration > 30*time.Second {
		return fmt.Errorf("manifest %s: max_duration exceeds the platform maximum", id)
	}
	return nil
}

func validateSchema(name string, s Schema) error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema %s: at least one field is required", name)
	}
	seen := map[string]struct{}{}
	for _, f := range s.Fields {
		if strings.TrimSpace(f.Name) == "" {
			return fmt.Errorf("schema %s: blank field name", name)
		}
		if _, dup := seen[f.Name]; dup {
			return fmt.Errorf("schema %s: duplicate field %q", name, f.Name)
		}
		seen[f.Name] = struct{}{}
		if _, ok := validFieldTypes[f.Type]; !ok {
			return fmt.Errorf("schema %s: unsupported type %q on field %q", name, f.Type, f.Name)
		}
		if f.MaxLength < 0 || f.Max < f.Min {
			return fmt.Errorf("schema %s: invalid bounds on field %q", name, f.Name)
		}
	}
	return nil
}

// Supports reports whether the manifest declares an operation.
func (m ToolManifest) Supports(operation string) bool {
	for _, op := range m.Operations {
		if op == operation {
			return true
		}
	}
	return false
}

// SortedOperations returns operations in deterministic order.
func (m ToolManifest) SortedOperations() []string {
	out := append([]string(nil), m.Operations...)
	sort.Strings(out)
	return out
}

// ValidateInput checks the input map against the input schema: required fields
// present, no unknown fields, enums and maximums respected. It is the schema
// gate of the invocation path (contract §5 step 2).
func (m ToolManifest) ValidateInput(input map[string]string) error {
	for _, f := range m.InputSchema.Fields {
		v, ok := input[f.Name]
		if !ok || v == "" {
			if f.Required {
				return fmt.Errorf("field %q is required", f.Name)
			}
			continue
		}
		switch f.Type {
		case FieldString:
			if f.MaxLength > 0 && len(v) > f.MaxLength {
				return fmt.Errorf("field %q exceeds max_length %d", f.Name, f.MaxLength)
			}
		case FieldInt:
			n, err := parseInt(v)
			if err != nil {
				return fmt.Errorf("field %q must be an integer", f.Name)
			}
			if err := bounds(f, n); err != nil {
				return err
			}
		case FieldFloat:
			if _, err := parseFloat(v); err != nil {
				return fmt.Errorf("field %q must be a number", f.Name)
			}
		case FieldBool:
			if v != "true" && v != "false" {
				return fmt.Errorf("field %q must be true or false", f.Name)
			}
		}
		if len(f.Enum) > 0 && !containsString(f.Enum, v) {
			return fmt.Errorf("field %q must be one of %s", f.Name, strings.Join(f.Enum, ","))
		}
	}
	for name := range input {
		if _, ok := m.InputSchema.Field(name); !ok {
			return fmt.Errorf("unknown field %q", name)
		}
	}
	return nil
}

func bounds(f SchemaField, n int64) error {
	if f.Max != 0 && n > f.Max {
		return fmt.Errorf("field %q exceeds max %d", f.Name, f.Max)
	}
	if f.Min != 0 && n < f.Min {
		return fmt.Errorf("field %q is below min %d", f.Name, f.Min)
	}
	return nil
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
