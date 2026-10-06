# Tool Registry (v2)

The single tool registry lives at `internal/foundation/tool`. The v2 layer
adds manifest validation and adapter binding as additive capabilities on the
same store — there is no second registry.

## Manifest

Every capability declares a `ToolManifest`:

```
id, version, name, description, category,
input_schema, output_schema,
side_effect_class,
network_requirement,
credential_requirement,
resource_limits,
supported_operations,
scope_requirement,
security_class
```

Shapes on the concrete type: [manifest.go](../internal/foundation/tool/manifest.go).

### Validation (fail-closed)

A manifest is **rejected on `Register`** when any of these hold:

- blank id, name, version
- unsupported `category` (only `read | write | compute | network`)
- missing or malformed schema (no fields, duplicate names, unknown field
  `type`, negative bounds)
- unknown `side_effect_class`, unknown `network_requirement`, unknown
  `security_class`, unknown `scope_requirement`
- `network_requirement=none` declared while `security_class=network`
- empty or duplicate `supported_operations`
- negative resource limits, or `max_duration > 30s` (platform cap)
- `credential_requirement.required=true` without a reference or without a
  business scope

### Retuned deterministic builtins

`echo`, `calculator`, `transform` are registered through the same v2 path
(`ToolRegistry.RegisterBuiltin`): their manifests pin `read` side-effect
class, `sandboxed` security class, sub-2s duration caps, and closed input
schemas. They have no credential or network requirement.

## Adapter

```go
type Adapter interface {
    Operations() []string
    Invoke(ctx context.Context, inv Invocation) (RawResult, error)
}
```

`Operations()` must exactly match the manifest's `supported_operations`
(for single-op manifests, the wrapped adapter can be projected to one op via
`opScopedAdapter`).

`Invocation` carries: tool id, operation, input, scope (business/division/actor/agent),
a `CredentialHandle` (opaque; the raw secret only through `handle.Secret()`),
and clamped per-invocation `ResourceLimits`.

## Flow at runtime

`Platform.Invoke` calls into `Registry.Manifest` for the declaration and
`Registry.Adapter` for the implementation, validating the operation is
declared before dispatch. There is no shadow path: agent runtime and the
intelligence loop share the exact same `Platform`.

## Catalog

`Platform.Catalog(businessID, divisionID)` returns the sorted, deterministic,
scope-visible manifests. No bisection or filtering is applied based on
credentials. It's how the model is informed about available tools without
enlisting discovery into permission.

## Compatibility

v1 `ToolDefinition`s still register the same way; the v2 pathway mirrors them
as first-class manifests with their semantics preserved.

