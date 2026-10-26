/**
 * Read-only configuration and storage inspection types.
 *
 * Mirrors internal/gateway/config_surface.go. These responses never contain a
 * credential value: only the environment-variable name and a presence status.
 */

/**
 * `unknown` is used for injected adapters whose credential requirements were
 * not declared. It is never a "present" claim.
 */
export type CredentialStatus = 'present' | 'missing' | 'not_required' | 'unknown';

/** Section-level provenance reported by the boot snapshot. */
export type ConfigurationSource = 'default' | 'file' | 'environment' | 'mixed';

export interface ConfigurationProvenance {
  kind: string;
  precedence: string;
  immutable: boolean;
  captured_at: string;
}

export interface ModelProviderView {
  id: string;
  name?: string;
  adapter: 'openai_compatible';
  endpoint: string;
  model_id: string;
  api_key_env: string;
  credential_status: CredentialStatus;
  timeout_ms: number;
  capabilities: string[];
  configured: boolean;
  live_verified: boolean;
  runtime_mutable: boolean;
  verification_reason: string;
}

export interface ModelRoutingView {
  mode: 'configured' | 'simulated';
  seeded: boolean;
  seeded_provider_mode?: string;
  seeded_provider_status?: string;
  per_purpose_selection: boolean;
  note: string;
}

/**
 * Canonical adapter identifier (for example "scripted-research", "wikipedia"
 * or "brave"). Older servers may send the loose legacy value "scripted".
 */
export type WebSearchProvider = string;

/** How the adapter was selected at boot. */
export type WebSearchKind = 'disabled' | 'scripted' | 'live' | 'injected' | 'invalid';
export type WebSearchMode = 'disabled' | 'scripted' | 'live' | 'injected' | 'invalid';

export interface WebSearchView {
  provider: WebSearchProvider;
  /** Optional for compatibility with servers that predate the boot descriptor. */
  kind?: WebSearchKind;
  mode: WebSearchMode;
  /** Optional for compatibility with servers that predate the boot descriptor. */
  registered?: boolean;
  live: boolean;
  keyless: boolean;
  credential_env?: string;
  credential_status: CredentialStatus;
  endpoint?: string;
  language?: string;
  selected_by: string;
  live_verified: boolean;
  verification_note: string;
  scripted_fallback: boolean;
  unsupported_reason?: string;
}

export interface StorageView {
  configured: boolean;
  mode: 'memory' | 'durable';
  persistence: string;
  data_dir?: string;
  exists: boolean;
  is_directory: boolean;
  writable: boolean;
  write_probe: string;
  total_bytes?: number;
  available_bytes?: number;
  used_bytes?: number;
  filesystem?: string;
  device?: string;
  device_type?: string;
  bus?: string;
  warnings?: string[];
}

export interface ConfigReadiness {
  model_provider_configured: boolean;
  model_credentials_present: boolean;
  web_search_configured: boolean;
  web_search_live: boolean;
  web_search_credential_ready: boolean;
  durable_storage_configured: boolean;
  durable_storage_writable: boolean;
  live_preconditions_met: boolean;
  live_verified: boolean;
  gate_zero_authorized: boolean;
  blocking?: string[];
  notes?: string[];
}

export interface ConfigurationSnapshot {
  loaded: boolean;
  environment: string;
  nexus_id: string;
  source: string;
  /** Versioned SHA-256 identifier of the boot snapshot; absent on older servers. */
  fingerprint?: string;
  /** Fingerprint schema version, e.g. "sha256:v2"; absent on older servers. */
  fingerprint_version?: string;
  /** Section-level provenance; absent on older servers. */
  sources?: Record<string, ConfigurationSource>;
  provenance?: ConfigurationProvenance;
  runtime_mutable: boolean;
  restart_required: boolean;
  credential_storage: 'environment_only';
  model_providers: ModelProviderView[];
  model_routing: ModelRoutingView;
  web_search: WebSearchView;
  storage: StorageView;
  readiness: ConfigReadiness;
  warnings?: string[];
  generated_at: string;
}

export type StorageRole = 'data_dir' | 'home' | 'root' | 'mount' | 'configured_root';

export interface StorageVolumeView {
  id: string;
  label: string;
  role: StorageRole;
  path: string;
  mount_point: string;
  filesystem?: string;
  device?: string;
  device_type?: string;
  bus?: string;
  total_bytes?: number;
  available_bytes?: number;
  used_bytes?: number;
  writable: boolean;
  is_data_dir: boolean;
}

export interface StorageVolumesResponse {
  host_kind: 'local_host';
  discovery: 'bounded';
  data_dir?: string;
  volumes: StorageVolumeView[];
  warnings?: string[];
  generated_at: string;
}

export type StorageCheckStatus = 'pass' | 'warn' | 'fail' | 'info';

export interface StorageCheck {
  id: string;
  status: StorageCheckStatus;
  message: string;
}

export interface StorageValidation {
  path: string;
  valid: boolean;
  exists: boolean;
  parent_exists: boolean;
  is_directory: boolean;
  writable: boolean;
  write_probe: 'access_check';
  total_bytes?: number;
  available_bytes?: number;
  used_bytes?: number;
  filesystem?: string;
  device?: string;
  device_type?: string;
  bus?: string;
  same_as_current_data_dir: boolean;
  checks: StorageCheck[];
  applying_supported: false;
  restart_required: true;
  migration_supported: false;
  notes?: string[];
}
