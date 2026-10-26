// Configuration surface (control plane).
//
// This file exposes a narrow, read-only view of the process's boot
// configuration plus a bounded host-storage inspector. It intentionally does
// NOT write configuration, store credentials, probe providers, migrate data or
// browse arbitrary filesystem paths.
//
// Security posture:
//   - Every route lives under /api/v1/control/, so the existing fail-closed
//     X-API-Key middleware protects it.
//   - Responses never contain credential values. Only the *name* of the
//     environment variable and whether it is currently set are disclosed.
//   - Storage discovery is bounded to a small set of operator-meaningful
//     candidates (configured data directory, home, root, mounted volumes under
//     /mnt, /media, /srv and /data). There is no directory-listing API.
//   - Path validation is server-side and rejects traversal, symlinks and
//     sensitive system roots. It never creates or moves anything.
package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
)

// Credential status values used by the read-only configuration surface.
const (
	credentialPresent     = "present"
	credentialMissing     = "missing"
	credentialNotRequired = "not_required"
	// credentialUnknown is used for an injected adapter whose credential
	// requirements were not declared. It is never a "present" claim.
	credentialUnknown = "unknown"
)

// ModelProviderView is one configured OpenAI-compatible model provider. It
// never carries the API key itself.
type ModelProviderView struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name,omitempty"`
	Adapter            string   `json:"adapter"`
	Endpoint           string   `json:"endpoint"`
	ModelID            string   `json:"model_id"`
	APIKeyEnv          string   `json:"api_key_env"`
	CredentialStatus   string   `json:"credential_status"`
	TimeoutMS          int64    `json:"timeout_ms"`
	Capabilities       []string `json:"capabilities"`
	Configured         bool     `json:"configured"`
	LiveVerified       bool     `json:"live_verified"`
	RuntimeMutable     bool     `json:"runtime_mutable"`
	VerificationReason string   `json:"verification_reason"`
}

// ModelRoutingView describes how the process is currently wired. The build has
// no per-purpose model selector; this view never implies one exists.
type ModelRoutingView struct {
	Mode                 string `json:"mode"`
	Seeded               bool   `json:"seeded"`
	SeededProviderMode   string `json:"seeded_provider_mode,omitempty"`
	SeededProviderStatus string `json:"seeded_provider_status,omitempty"`
	PerPurposeSelection  bool   `json:"per_purpose_selection"`
	Note                 string `json:"note"`
}

// WebSearchView describes the actual web.search provider selected at boot.
type WebSearchView struct {
	Provider          string `json:"provider"`
	Kind              string `json:"kind"`
	Mode              string `json:"mode"`
	Registered        bool   `json:"registered"`
	Live              bool   `json:"live"`
	Keyless           bool   `json:"keyless"`
	CredentialEnv     string `json:"credential_env,omitempty"`
	CredentialStatus  string `json:"credential_status"`
	Endpoint          string `json:"endpoint,omitempty"`
	Language          string `json:"language,omitempty"`
	SelectedBy        string `json:"selected_by"`
	LiveVerified      bool   `json:"live_verified"`
	VerificationNote  string `json:"verification_note"`
	ScriptedFallback  bool   `json:"scripted_fallback"`
	UnsupportedReason string `json:"unsupported_reason,omitempty"`
}

// StorageView is the effective durable-storage posture.
type StorageView struct {
	Configured     bool     `json:"configured"`
	Mode           string   `json:"mode"`
	Persistence    string   `json:"persistence"`
	DataDir        string   `json:"data_dir,omitempty"`
	Exists         bool     `json:"exists"`
	IsDirectory    bool     `json:"is_directory"`
	Writable       bool     `json:"writable"`
	WriteProbe     string   `json:"write_probe"`
	TotalBytes     uint64   `json:"total_bytes,omitempty"`
	AvailableBytes uint64   `json:"available_bytes,omitempty"`
	UsedBytes      uint64   `json:"used_bytes,omitempty"`
	Filesystem     string   `json:"filesystem,omitempty"`
	Device         string   `json:"device,omitempty"`
	DeviceType     string   `json:"device_type,omitempty"`
	Bus            string   `json:"bus,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// ConfigReadiness reports boot-time preconditions. It is deliberately not a
// health or "connected" claim: no provider is contacted by this surface.
type ConfigReadiness struct {
	ModelProviderConfigured  bool     `json:"model_provider_configured"`
	ModelCredentialsPresent  bool     `json:"model_credentials_present"`
	WebSearchConfigured      bool     `json:"web_search_configured"`
	WebSearchLive            bool     `json:"web_search_live"`
	WebSearchCredentialReady bool     `json:"web_search_credential_ready"`
	DurableStorageConfigured bool     `json:"durable_storage_configured"`
	DurableStorageWritable   bool     `json:"durable_storage_writable"`
	LivePreconditionsMet     bool     `json:"live_preconditions_met"`
	LiveVerified             bool     `json:"live_verified"`
	GateZeroAuthorized       bool     `json:"gate_zero_authorized"`
	Blocking                 []string `json:"blocking,omitempty"`
	Notes                    []string `json:"notes,omitempty"`
}

// ConfigProvenance identifies where the response's snapshot came from and
// states that it cannot be mutated through this process.
type ConfigProvenance struct {
	Kind       string `json:"kind"`
	Precedence string `json:"precedence"`
	Immutable  bool   `json:"immutable"`
	CapturedAt string `json:"captured_at"`
}

// ConfigurationView is the complete redacted snapshot returned to the UI.
type ConfigurationView struct {
	Loaded             bool                `json:"loaded"`
	Environment        string              `json:"environment"`
	NexusID            string              `json:"nexus_id"`
	Source             string              `json:"source"`
	Fingerprint        string              `json:"fingerprint"`
	FingerprintVersion string              `json:"fingerprint_version"`
	Sources            map[string]string   `json:"sources"`
	Provenance         ConfigProvenance    `json:"provenance"`
	RuntimeMutable     bool                `json:"runtime_mutable"`
	RestartRequired    bool                `json:"restart_required"`
	CredentialStorage  string              `json:"credential_storage"`
	ModelProviders     []ModelProviderView `json:"model_providers"`
	ModelRouting       ModelRoutingView    `json:"model_routing"`
	WebSearch          WebSearchView       `json:"web_search"`
	Storage            StorageView         `json:"storage"`
	Readiness          ConfigReadiness     `json:"readiness"`
	Warnings           []string            `json:"warnings,omitempty"`
	GeneratedAt        string              `json:"generated_at"`
}

// StorageVolumeView is one bounded storage candidate. The path is host-local
// and only disclosed to an authenticated operator.
type StorageVolumeView struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Role           string `json:"role"`
	Path           string `json:"path"`
	MountPoint     string `json:"mount_point"`
	Filesystem     string `json:"filesystem,omitempty"`
	Device         string `json:"device,omitempty"`
	DeviceType     string `json:"device_type,omitempty"`
	Bus            string `json:"bus,omitempty"`
	TotalBytes     uint64 `json:"total_bytes,omitempty"`
	AvailableBytes uint64 `json:"available_bytes,omitempty"`
	UsedBytes      uint64 `json:"used_bytes,omitempty"`
	Writable       bool   `json:"writable"`
	IsDataDir      bool   `json:"is_data_dir"`
}

// StorageVolumesResponse is the bounded volume list.
type StorageVolumesResponse struct {
	HostKind    string              `json:"host_kind"`
	Discovery   string              `json:"discovery"`
	DataDir     string              `json:"data_dir,omitempty"`
	Volumes     []StorageVolumeView `json:"volumes"`
	Warnings    []string            `json:"warnings,omitempty"`
	GeneratedAt string              `json:"generated_at"`
}

// StorageValidateRequest is the only accepted body of the validation route.
type StorageValidateRequest struct {
	Path string `json:"path"`
}

// StorageCheck is one deterministic server-side validation result.
type StorageCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // pass | warn | fail | info
	Message string `json:"message"`
}

// StorageValidation is the validation-only response. Applying a path is not
// implemented; the UI must present that limitation honestly.
type StorageValidation struct {
	Path                 string         `json:"path"`
	Valid                bool           `json:"valid"`
	Exists               bool           `json:"exists"`
	ParentExists         bool           `json:"parent_exists"`
	IsDirectory          bool           `json:"is_directory"`
	Writable             bool           `json:"writable"`
	WriteProbe           string         `json:"write_probe"`
	TotalBytes           uint64         `json:"total_bytes,omitempty"`
	AvailableBytes       uint64         `json:"available_bytes,omitempty"`
	UsedBytes            uint64         `json:"used_bytes,omitempty"`
	Filesystem           string         `json:"filesystem,omitempty"`
	Device               string         `json:"device,omitempty"`
	DeviceType           string         `json:"device_type,omitempty"`
	Bus                  string         `json:"bus,omitempty"`
	SameAsCurrentDataDir bool           `json:"same_as_current_data_dir"`
	Checks               []StorageCheck `json:"checks"`
	ApplyingSupported    bool           `json:"applying_supported"`
	RestartRequired      bool           `json:"restart_required"`
	MigrationSupported   bool           `json:"migration_supported"`
	Notes                []string       `json:"notes,omitempty"`
}

type mountEntry struct {
	mountPoint string
	fsType     string
	source     string
}

// bootConfigState is the immutable boot projection used by the read-only
// surface. Credential presence and the web-search selection are sampled once,
// when the options are applied, so later environment changes cannot retcon
// what the process actually booted with.
type bootConfigState struct {
	snapshot                   config.Snapshot
	capturedAt                 time.Time
	modelCredentialStatus      []string
	webSearch                  capability.ResearchProviderDescriptor
	webSearchCredentialPresent bool
}

// WithConfigurationSnapshot supplies the exact immutable boot snapshot. It
// contains credential *references* only; no raw secret is ever placed on
// Server. Model credential presence is captured here, once.
func WithConfigurationSnapshot(snap config.Snapshot) ServerOption {
	return func(s *Server) {
		// A zero snapshot is not a loaded configuration; leave the surface
		// unavailable rather than serving an empty fingerprint as if it were
		// real.
		if snap.Fingerprint() == "" {
			return
		}
		if s.bootConfig == nil {
			s.bootConfig = &bootConfigState{}
		}
		s.bootConfig.snapshot = snap
		if s.bootConfig.capturedAt.IsZero() {
			s.bootConfig.capturedAt = s.now().UTC()
		}
		settings := snap.Settings()
		statuses := make([]string, len(settings.Models.Providers))
		for i, p := range settings.Models.Providers {
			statuses[i] = bootCredentialStatus(p.APIKeyEnv)
		}
		s.bootConfig.modelCredentialStatus = statuses
	}
}

// WithWebSearchSelection records the descriptor captured from the web-search
// adapter the launcher actually constructed. Credential presence is sampled
// here, once; request handling never reads NEXUS_CAPABILITY_WEB or the
// credential variable again.
func WithWebSearchSelection(desc capability.ResearchProviderDescriptor) ServerOption {
	return func(s *Server) {
		if s.bootConfig == nil {
			s.bootConfig = &bootConfigState{}
		}
		s.bootConfig.webSearch = desc
		s.bootConfig.webSearchCredentialPresent = desc.CredentialEnv != "" &&
			strings.TrimSpace(os.Getenv(desc.CredentialEnv)) != ""
	}
}

func bootCredentialStatus(envName string) string {
	if strings.TrimSpace(envName) == "" {
		return credentialMissing
	}
	if strings.TrimSpace(os.Getenv(envName)) == "" {
		return credentialMissing
	}
	return credentialPresent
}

// bootConfiguration returns the boot state only when a real snapshot was
// supplied. A web-search selection on its own is not a loaded configuration.
func (s *Server) bootConfiguration() (*bootConfigState, bool) {
	if s.bootConfig == nil || s.bootConfig.snapshot.Fingerprint() == "" {
		return nil, false
	}
	return s.bootConfig, true
}

func (s *Server) handleGetConfiguration(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.bootConfiguration(); !ok {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"configuration snapshot is not available in this process")
		return
	}
	view := buildConfigurationView(s.bootConfig, s.now)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetStorageVolumes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.bootConfiguration(); !ok {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"configuration snapshot is not available in this process")
		return
	}
	resp := buildStorageVolumes(s.bootConfig.snapshot.Settings().Storage.DataDir, os.Getenv, s.now)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleValidateStoragePath(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.bootConfiguration(); !ok {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"configuration snapshot is not available in this process")
		return
	}
	var body StorageValidateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid storage validation request")
		return
	}
	// Reject trailing JSON values instead of silently validating only the
	// first object in a request that contains more than one.
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid storage validation request")
		return
	}
	validation, status, msg := validateStoragePath(body.Path,
		s.bootConfig.snapshot.Settings().Storage.DataDir, os.Getenv)
	if status != http.StatusOK {
		s.writeError(w, r, status, "VALIDATION", msg)
		return
	}
	writeJSON(w, http.StatusOK, validation)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Configuration and storage metadata is operator-only and may change only
	// at boot; keep it out of shared/intermediary caches.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func buildConfigurationView(boot *bootConfigState, now func() time.Time) ConfigurationView {
	cfg := boot.snapshot.Settings()
	web := normalizeWebSearchDescriptor(boot.webSearch)
	view := ConfigurationView{
		Loaded:             true,
		Environment:        cfg.Nexus.Environment,
		NexusID:            cfg.Nexus.ID,
		Source:             "boot configuration (defaults < file < environment)",
		Fingerprint:        boot.snapshot.Fingerprint(),
		FingerprintVersion: config.FingerprintSchema,
		Sources:            sourceStrings(boot.snapshot.SourceMap()),
		Provenance: ConfigProvenance{
			Kind:       "boot_snapshot",
			Precedence: "defaults < file < environment",
			Immutable:  true,
			CapturedAt: boot.capturedAt.UTC().Format(time.RFC3339),
		},
		RuntimeMutable:    false,
		RestartRequired:   true,
		CredentialStorage: "environment_only",
		GeneratedAt:       now().UTC().Format(time.RFC3339),
	}

	modelCredentials := true
	for i, p := range cfg.Models.Providers {
		cred := credentialMissing
		if i < len(boot.modelCredentialStatus) {
			cred = boot.modelCredentialStatus[i]
		}
		if cred != credentialPresent {
			modelCredentials = false
		}
		timeout := p.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		caps := append([]string(nil), p.Capabilities...)
		if len(caps) == 0 {
			caps = []string{"reasoning", "tool_calling", "structured_output"}
		}
		view.ModelProviders = append(view.ModelProviders, ModelProviderView{
			ID:               p.ID,
			Name:             p.Name,
			Adapter:          "openai_compatible",
			Endpoint:         displayEndpoint(p.Endpoint),
			ModelID:          p.ModelID,
			APIKeyEnv:        p.APIKeyEnv,
			CredentialStatus: cred,
			TimeoutMS:        timeout.Milliseconds(),
			Capabilities:     caps,
			Configured:       true,
			LiveVerified:     false,
			RuntimeMutable:   false,
			VerificationReason: "live connection tests are disabled until explicit " +
				"Gate 0 authorization; this surface never calls a provider",
		})
	}
	if view.ModelProviders == nil {
		view.ModelProviders = []ModelProviderView{}
	}

	seeded := len(cfg.Models.Providers) == 0
	view.ModelRouting = ModelRoutingView{
		Mode:                 "configured",
		Seeded:               seeded,
		SeededProviderMode:   cfg.Models.SeededProviderMode,
		SeededProviderStatus: cfg.Models.SeededProviderStatus,
		PerPurposeSelection:  false,
		Note:                 "Providers are registered at boot in configuration order; no per-purpose routing selector is implemented.",
	}
	if seeded {
		view.ModelRouting.Mode = "simulated"
		if cfg.Models.SeededProviderMode == "scripted" {
			view.ModelRouting.Note = "The scripted simulation provider is active. It is deterministic test infrastructure, not a live model."
		} else {
			view.ModelRouting.Note = "The seeded simulator is active. It cannot perform live inference; configure a real OpenAI-compatible provider."
		}
		view.Warnings = append(view.Warnings,
			"No real model provider is configured; the simulated provider is development-only.")
	}

	view.WebSearch = buildWebSearchView(web, boot.webSearchCredentialPresent)

	view.Storage = inspectStorage(cfg.Storage.DataDir)
	if !view.Storage.Configured {
		view.Warnings = append(view.Warnings,
			"Durable storage is disabled; organizational records are process-local and do not survive restart.")
	} else if !view.Storage.Exists || !view.Storage.IsDirectory || !view.Storage.Writable {
		view.Warnings = append(view.Warnings,
			"The configured storage.data_dir is not currently usable; boot would fail closed.")
	}
	switch view.WebSearch.Mode {
	case "disabled":
		view.Warnings = append(view.Warnings,
			"web.search is disabled; research executions cannot retrieve sources.")
	case "scripted":
		view.Warnings = append(view.Warnings,
			"web.search uses the scripted provider; results are deterministic fixtures, not live evidence.")
	case "injected":
		view.Warnings = append(view.Warnings,
			"web.search uses an injected adapter whose liveness and credential requirements are not declared.")
	case "invalid":
		view.Warnings = append(view.Warnings,
			"The selected web.search provider is unsupported; boot fails closed.")
	}
	if !view.WebSearch.Registered && view.WebSearch.Mode != "disabled" && view.WebSearch.Mode != "invalid" {
		view.Warnings = append(view.Warnings,
			"The selected web.search provider is not registered in this process.")
	}
	if view.WebSearch.CredentialStatus == credentialMissing {
		view.Warnings = append(view.Warnings,
			fmt.Sprintf("%s is not set; the selected live search provider cannot start.", view.WebSearch.CredentialEnv))
	}

	modelConfigured := len(view.ModelProviders) > 0
	webConfigured := view.WebSearch.Registered &&
		view.WebSearch.Mode != "disabled" && view.WebSearch.Mode != "invalid"
	webLive := webConfigured && view.WebSearch.Mode == "live"
	webCredentialReady := webConfigured &&
		(view.WebSearch.CredentialStatus == credentialPresent ||
			view.WebSearch.CredentialStatus == credentialNotRequired)
	view.Readiness = ConfigReadiness{
		ModelProviderConfigured:  modelConfigured,
		ModelCredentialsPresent:  modelConfigured && modelCredentials,
		WebSearchConfigured:      webConfigured,
		WebSearchLive:            webLive,
		WebSearchCredentialReady: webCredentialReady,
		DurableStorageConfigured: view.Storage.Configured,
		DurableStorageWritable:   view.Storage.Configured && view.Storage.Exists && view.Storage.IsDirectory && view.Storage.Writable,
		LiveVerified:             false,
		GateZeroAuthorized:       false,
		Notes: []string{
			"Preconditions are evaluated from the boot snapshot only; no provider is contacted.",
			"Live activation additionally requires a recorded Gate 0 authorization and the prerequisite gates in the live-activation runbook.",
		},
	}
	if !modelConfigured {
		view.Readiness.Blocking = append(view.Readiness.Blocking, "No real model provider is configured.")
	} else if !modelCredentials {
		view.Readiness.Blocking = append(view.Readiness.Blocking, "One or more model provider credentials are not set.")
	}
	switch {
	case !webConfigured && view.WebSearch.Mode == "disabled":
		view.Readiness.Blocking = append(view.Readiness.Blocking, "web.search is disabled.")
	case !webConfigured && view.WebSearch.Mode == "invalid":
		view.Readiness.Blocking = append(view.Readiness.Blocking, "The selected web.search provider is unsupported.")
	case !webConfigured:
		view.Readiness.Blocking = append(view.Readiness.Blocking, "The selected web.search provider is not registered in this process.")
	case !webLive && view.WebSearch.Kind == capability.ResearchKindScripted:
		view.Readiness.Blocking = append(view.Readiness.Blocking, "web.search is using the scripted provider, not a live provider.")
	case !webLive && view.WebSearch.Kind == capability.ResearchKindInjected:
		view.Readiness.Blocking = append(view.Readiness.Blocking, "web.search uses an injected adapter whose liveness is unknown.")
	case !webLive:
		view.Readiness.Blocking = append(view.Readiness.Blocking, "web.search is not using a live provider.")
	case !webCredentialReady:
		view.Readiness.Blocking = append(view.Readiness.Blocking, "The selected live web-search credential is not set.")
	}
	if !view.Storage.Configured {
		view.Readiness.Blocking = append(view.Readiness.Blocking, "Durable storage is not configured.")
	} else if !view.Readiness.DurableStorageWritable {
		view.Readiness.Blocking = append(view.Readiness.Blocking, "The configured data directory is not writable.")
	}
	if !view.Readiness.GateZeroAuthorized {
		view.Readiness.Blocking = append(view.Readiness.Blocking, "No recorded Gate 0 authorization is available to this process.")
	}
	view.Readiness.LivePreconditionsMet = len(view.Readiness.Blocking) == 0

	sort.Strings(view.Warnings)
	return view
}

// normalizeWebSearchDescriptor makes a missing descriptor the explicit
// disabled state. A non-empty kind is used as captured; this function never
// infers liveness from a provider name.
func normalizeWebSearchDescriptor(desc capability.ResearchProviderDescriptor) capability.ResearchProviderDescriptor {
	if desc.Kind == "" && desc.Provider == "" {
		d := capability.DescribeResearchProvider(nil)
		d.SelectedBy = "NEXUS_CAPABILITY_WEB"
		return d
	}
	if desc.Kind == "" {
		// A provider name without a captured kind has unknown provenance.
		desc.Kind = capability.ResearchKindInjected
	}
	if desc.SelectedBy == "" {
		desc.SelectedBy = "boot_configuration"
	}
	return desc
}

func buildWebSearchView(desc capability.ResearchProviderDescriptor, credentialIsPresent bool) WebSearchView {
	v := WebSearchView{
		Provider:         desc.Provider,
		Kind:             desc.Kind,
		Registered:       desc.Registered,
		Live:             desc.Live,
		Keyless:          desc.Keyless,
		CredentialEnv:    desc.CredentialEnv,
		Language:         desc.Language,
		SelectedBy:       desc.SelectedBy,
		LiveVerified:     false,
		ScriptedFallback: false,
		VerificationNote: "No live search is performed by this console; a safe test requires explicit Gate 0 authorization.",
	}
	if desc.Endpoint != "" {
		v.Endpoint = displayEndpoint(desc.Endpoint)
	}
	switch desc.Kind {
	case capability.ResearchKindDisabled:
		v.Provider = "disabled"
		v.Mode = "disabled"
		v.CredentialStatus = credentialNotRequired
		v.VerificationNote = "No web-search provider is selected."
	case capability.ResearchKindScripted:
		v.Mode = "scripted"
		v.CredentialStatus = credentialNotRequired
		v.ScriptedFallback = true
		v.VerificationNote = "The scripted provider returns deterministic fixtures; it is test-only and never live evidence."
	case capability.ResearchKindLive:
		v.Mode = "live"
		switch {
		case desc.Keyless:
			v.CredentialStatus = credentialNotRequired
		case desc.CredentialEnv != "":
			if credentialIsPresent {
				v.CredentialStatus = credentialPresent
			} else {
				v.CredentialStatus = credentialMissing
			}
		default:
			// A live adapter that declares no credential requirement and is
			// not keyless has unknown provenance; do not invent "not required".
			v.CredentialStatus = credentialUnknown
		}
	case capability.ResearchKindInjected:
		v.Mode = "injected"
		v.CredentialStatus = credentialUnknown
		v.VerificationNote = "The web-search adapter was injected by the embedding process; its liveness and credential requirements are not declared and are not verified."
	default:
		v.Provider = "invalid"
		v.Kind = "invalid"
		v.Mode = "invalid"
		v.CredentialStatus = credentialUnknown
		v.UnsupportedReason = "The selected web-search provider is not supported; boot fails closed."
		v.VerificationNote = "The selected provider is unsupported; boot fails closed."
	}
	return v
}

// sourceStrings converts the snapshot's typed section metadata to the wire
// shape. It is a fresh map; callers cannot mutate snapshot state through it.
func sourceStrings(in map[string]config.Source) map[string]string {
	out := make(map[string]string, len(in))
	for section, src := range in {
		out[section] = string(src)
	}
	return out
}

func inspectStorage(dataDir string) StorageView {
	v := StorageView{
		WriteProbe: "access_check",
		Warnings:   []string{},
	}
	if strings.TrimSpace(dataDir) == "" {
		v.Mode = "memory"
		v.Persistence = "none"
		v.Warnings = append(v.Warnings, "Records stay in memory and are lost on restart.")
		return v
	}
	v.Configured = true
	v.Mode = "durable"
	v.Persistence = "file-backed"
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		v.DataDir = dataDir
		v.Warnings = append(v.Warnings, "The configured path could not be resolved.")
		return v
	}
	v.DataDir = abs
	// Follow a symlink for the *configured* directory: mounted volumes are
	// commonly symlinked and the running process already accepted this path.
	// New path selection is stricter and rejects final symlinks.
	info, statErr := os.Stat(abs)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			v.Warnings = append(v.Warnings, "The configured data directory does not exist; boot would fail closed.")
		} else {
			v.Warnings = append(v.Warnings, "The configured data directory cannot be inspected.")
		}
		return v
	}
	v.Exists = true
	v.IsDirectory = info.IsDir()
	if !v.IsDirectory {
		v.Warnings = append(v.Warnings, "The configured data path is not a directory.")
		return v
	}
	v.Writable = writableByProcess(abs)
	if !v.Writable {
		v.Warnings = append(v.Warnings, "The configured data directory is not writable by the NEXUS process.")
	}
	if !readableByProcess(abs) {
		v.Warnings = append(v.Warnings, "The configured data directory is not readable by the NEXUS process; boot would fail closed.")
	}
	vol := inspectVolume(abs)
	v.TotalBytes = vol.TotalBytes
	v.AvailableBytes = vol.AvailableBytes
	v.UsedBytes = vol.UsedBytes
	v.Filesystem = vol.Filesystem
	v.Device = vol.Device
	v.DeviceType = vol.DeviceType
	v.Bus = vol.Bus
	return v
}

func buildStorageVolumes(dataDir string, env func(string) string, now func() time.Time) StorageVolumesResponse {
	resp := StorageVolumesResponse{
		HostKind:    "local_host",
		Discovery:   "bounded",
		Volumes:     []StorageVolumeView{},
		GeneratedAt: now().UTC().Format(time.RFC3339),
		Warnings: []string{
			"Volumes belong to the machine running NEXUS, not to the browser device.",
			"Changing storage.data_dir does not move existing records; migration is not implemented.",
		},
	}
	seen := map[string]bool{}
	add := func(path, role, label string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil || !filepath.IsAbs(abs) {
			return
		}
		if seen[abs] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return
		}
		seen[abs] = true
		vol := inspectVolume(abs)
		vol.ID = "vol-" + sanitizeID(abs)
		vol.Label = label
		vol.Role = role
		vol.Path = abs
		vol.IsDataDir = dataDir != "" && samePath(abs, dataDir)
		resp.Volumes = append(resp.Volumes, vol)
	}

	if dataDir != "" {
		resp.DataDir = dataDir
		if abs, err := filepath.Abs(dataDir); err == nil {
			resp.DataDir = abs
		}
		add(dataDir, "data_dir", "Configured data directory")
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "home", "Home directory")
	}
	add("/", "root", "System root")

	mounts := readMountInfo()
	for _, m := range mounts {
		if m.mountPoint == "/" {
			continue
		}
		if !isBoundedMount(m.mountPoint) {
			continue
		}
		add(m.mountPoint, "mount", "Mounted volume")
	}
	for _, extra := range strings.Split(env("NEXUS_STORAGE_ROOTS"), ":") {
		extra = strings.TrimSpace(extra)
		if extra == "" || !filepath.IsAbs(extra) {
			continue
		}
		add(extra, "configured_root", "Configured storage root")
	}

	sort.SliceStable(resp.Volumes, func(i, j int) bool {
		rank := func(role string) int {
			switch role {
			case "data_dir":
				return 0
			case "home":
				return 1
			case "root":
				return 2
			default:
				return 3
			}
		}
		if rank(resp.Volumes[i].Role) != rank(resp.Volumes[j].Role) {
			return rank(resp.Volumes[i].Role) < rank(resp.Volumes[j].Role)
		}
		return resp.Volumes[i].Path < resp.Volumes[j].Path
	})
	return resp
}

func validateStoragePath(rawPath, currentDataDir string, env func(string) string) (StorageValidation, int, string) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return StorageValidation{}, http.StatusBadRequest, "path is required"
	}
	if len(path) > 4096 || strings.ContainsRune(path, 0) {
		return StorageValidation{}, http.StatusBadRequest, "path is invalid"
	}
	if !filepath.IsAbs(path) {
		return StorageValidation{}, http.StatusBadRequest, "path must be absolute"
	}
	// Reject traversal segments before cleaning. filepath.Clean would resolve
	// `..` and could silently turn a traversal attempt into an allowed path.
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".." {
			return StorageValidation{}, http.StatusBadRequest, "path traversal is not permitted"
		}
	}
	clean := filepath.Clean(path)
	if clean == "/" {
		return StorageValidation{}, http.StatusBadRequest, "the filesystem root cannot be used as a data directory"
	}
	if isSensitiveStoragePath(clean) && !(currentDataDir != "" && samePath(clean, currentDataDir)) {
		return StorageValidation{}, http.StatusBadRequest,
			"sensitive system paths cannot be used as a data directory"
	}
	if !underAllowedStorageRoot(clean, currentDataDir, env) {
		return StorageValidation{}, http.StatusBadRequest,
			"path must be under an allowed data root (home, /mnt, /media, /srv, /opt, /var/lib, /data, /tmp, the configured data directory, or NEXUS_STORAGE_ROOTS)"
	}

	v := StorageValidation{
		Path:                 clean,
		WriteProbe:           "access_check",
		SameAsCurrentDataDir: currentDataDir != "" && samePath(clean, currentDataDir),
		ApplyingSupported:    false,
		RestartRequired:      true,
		MigrationSupported:   false,
		Checks:               []StorageCheck{},
		Notes: []string{
			"This endpoint validates only. It does not create directories, write configuration or move data.",
			"Applying a data-directory change requires an operator edit and a controlled restart; one process per data directory must be preserved.",
		},
	}

	info, err := os.Lstat(clean)
	switch {
	case err == nil:
		v.Exists = true
		if info.Mode()&os.ModeSymlink != 0 {
			v.Checks = append(v.Checks, StorageCheck{ID: "symlink", Status: "fail",
				Message: "The final path component is a symlink. Choose the resolved directory explicitly."})
		} else if !info.IsDir() {
			v.Checks = append(v.Checks, StorageCheck{ID: "directory", Status: "fail",
				Message: "The path exists but is not a directory."})
		} else {
			v.IsDirectory = true
			v.Checks = append(v.Checks, StorageCheck{ID: "directory", Status: "pass",
				Message: "The path is an existing directory."})
			resolved, rerr := filepath.EvalSymlinks(clean)
			if rerr != nil || !underAllowedStorageRoot(resolved, currentDataDir, env) ||
				(isSensitiveStoragePath(resolved) && !(currentDataDir != "" && samePath(resolved, currentDataDir))) {
				v.Checks = append(v.Checks, StorageCheck{ID: "symlink", Status: "fail",
					Message: "The resolved path is outside the allowed storage roots."})
			} else {
				v.Checks = append(v.Checks, StorageCheck{ID: "symlink", Status: "pass",
					Message: "No unsafe symlink escape detected."})
			}
			if !readableByProcess(clean) {
				v.Checks = append(v.Checks, StorageCheck{ID: "readable", Status: "fail",
					Message: "The path is not readable by the NEXUS process; existing records could not be loaded."})
			}
			v.Writable = writableByProcess(clean)
		}
	case os.IsNotExist(err):
		parent := filepath.Dir(clean)
		pinfo, perr := os.Stat(parent)
		if perr != nil || !pinfo.IsDir() {
			v.Checks = append(v.Checks, StorageCheck{ID: "parent", Status: "fail",
				Message: "The parent directory does not exist or is not a directory."})
			break
		}
		v.ParentExists = true
		pv, pstatus, pmsg := validateParentForPath(parent, currentDataDir, env)
		v.Writable = pv
		if pstatus != http.StatusOK || !pv {
			// A status-OK/non-writable parent is still a failed check: reporting
			// it as "exists and is writable" would be a false assurance.
			v.Checks = append(v.Checks, StorageCheck{ID: "parent", Status: "fail", Message: pmsg})
			if pstatus != http.StatusOK {
				break
			}
		} else {
			v.Checks = append(v.Checks, StorageCheck{ID: "parent", Status: "pass",
				Message: "The parent directory exists and is writable; the final directory does not exist yet."})
		}
		v.Checks = append(v.Checks, StorageCheck{ID: "directory", Status: "info",
			Message: "The path does not exist; NEXUS will not create it from this console."})
	default:
		v.Checks = append(v.Checks, StorageCheck{ID: "directory", Status: "fail",
			Message: "The path cannot be inspected."})
	}

	if v.Writable {
		v.Checks = append(v.Checks, StorageCheck{ID: "writable", Status: "pass",
			Message: "The NEXUS process can write here (permission access check)."})
	} else if len(v.Checks) > 0 {
		v.Checks = append(v.Checks, StorageCheck{ID: "writable", Status: "fail",
			Message: "The NEXUS process cannot write here."})
	}

	// Capacity is rooted at the nearest existing path: a new directory will
	// consume space on its parent's filesystem.
	capacityPath := clean
	if _, statErr := os.Stat(clean); statErr != nil {
		capacityPath = filepath.Dir(clean)
	}
	vol := inspectVolume(capacityPath)
	vol.Path = clean
	v.TotalBytes = vol.TotalBytes
	v.AvailableBytes = vol.AvailableBytes
	v.UsedBytes = vol.UsedBytes
	v.Filesystem = vol.Filesystem
	v.Device = vol.Device
	v.DeviceType = vol.DeviceType
	v.Bus = vol.Bus
	if v.TotalBytes > 0 {
		switch {
		case v.AvailableBytes < 64<<20:
			v.Checks = append(v.Checks, StorageCheck{ID: "capacity", Status: "fail",
				Message: "Less than 64 MiB is available on this filesystem."})
		case v.AvailableBytes < 1<<30:
			v.Checks = append(v.Checks, StorageCheck{ID: "capacity", Status: "warn",
				Message: "Less than 1 GiB is available on this filesystem."})
		default:
			v.Checks = append(v.Checks, StorageCheck{ID: "capacity", Status: "pass",
				Message: "Available capacity is above the 1 GiB warning threshold."})
		}
	}
	if samePath(clean, "/tmp") || strings.HasPrefix(clean, "/tmp/") {
		v.Checks = append(v.Checks, StorageCheck{ID: "persistence", Status: "warn",
			Message: "/tmp is commonly cleared on reboot; use it only for disposable test data."})
	}
	if v.SameAsCurrentDataDir {
		v.Checks = append(v.Checks, StorageCheck{ID: "current", Status: "info",
			Message: "This is the directory the running process already uses."})
	}

	v.Valid = true
	for _, c := range v.Checks {
		if c.Status == "fail" {
			v.Valid = false
			break
		}
	}
	if !v.Valid {
		v.Notes = append(v.Notes, "One or more checks failed; this path must not be applied.")
	}
	return v, http.StatusOK, ""
}

func validateParentForPath(parent, currentDataDir string, env func(string) string) (bool, int, string) {
	if !underAllowedStorageRoot(parent, currentDataDir, env) {
		return false, http.StatusBadRequest, "The parent path is outside the allowed storage roots."
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || !underAllowedStorageRoot(resolved, currentDataDir, env) ||
		(isSensitiveStoragePath(resolved) && !(currentDataDir != "" && samePath(resolved, currentDataDir))) {
		return false, http.StatusBadRequest, "The parent path resolves outside the allowed storage roots."
	}
	if !writableByProcess(parent) {
		return false, http.StatusOK, "The parent directory is not writable by the NEXUS process."
	}
	return true, http.StatusOK, ""
}

// isSensitiveStoragePath rejects well-known system locations regardless of
// NEXUS_STORAGE_ROOTS. An operator must not be able to turn the storage
// inspector into a data-directory picker for /etc, /proc or a boot volume.
func isSensitiveStoragePath(path string) bool {
	for _, root := range []string{
		"/etc", "/proc", "/sys", "/dev", "/boot", "/root", "/run", "/usr",
		"/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32",
		"/var/run", "/var/log", "/var/spool",
	} {
		if samePath(path, root) || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

func underAllowedStorageRoot(path, currentDataDir string, env func(string) string) bool {
	roots := []string{"/mnt", "/media", "/srv", "/opt", "/var/lib", "/data", "/tmp"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, home)
	}
	if currentDataDir != "" {
		if abs, err := filepath.Abs(currentDataDir); err == nil {
			roots = append(roots, abs)
		}
	}
	for _, extra := range strings.Split(env("NEXUS_STORAGE_ROOTS"), ":") {
		extra = strings.TrimSpace(extra)
		if extra == "" || !filepath.IsAbs(extra) {
			continue
		}
		roots = append(roots, filepath.Clean(extra))
	}
	for _, root := range roots {
		if samePath(path, root) || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

func isBoundedMount(mountPoint string) bool {
	for _, root := range []string{"/mnt", "/media", "/srv", "/data"} {
		if mountPoint == root || strings.HasPrefix(mountPoint, root+"/") {
			return true
		}
	}
	return false
}

func inspectVolume(path string) StorageVolumeView {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	vol := StorageVolumeView{Path: abs}
	m := mountForPath(abs)
	if m != nil {
		vol.MountPoint = m.mountPoint
		vol.Filesystem = m.fsType
		vol.Device = m.source
		vol.DeviceType, vol.Bus = deviceAttributes(m.source)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(abs, &st); err == nil {
		bsize := uint64(st.Bsize)
		vol.TotalBytes = uint64(st.Blocks) * bsize
		vol.AvailableBytes = uint64(st.Bavail) * bsize
		free := uint64(st.Bfree) * bsize
		if vol.TotalBytes >= free {
			vol.UsedBytes = vol.TotalBytes - free
		}
	}
	vol.Writable = writableByProcess(abs)
	return vol
}

func mountForPath(path string) *mountEntry {
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	} else if r, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		resolved = r
	}
	mounts := readMountInfo()
	var best *mountEntry
	bestLen := -1
	for i := range mounts {
		m := &mounts[i]
		if samePath(resolved, m.mountPoint) || strings.HasPrefix(resolved, strings.TrimSuffix(m.mountPoint, "/")+"/") {
			if len(m.mountPoint) > bestLen {
				best = m
				bestLen = len(m.mountPoint)
			}
		}
	}
	if best == nil && len(mounts) > 0 {
		for i := range mounts {
			if mounts[i].mountPoint == "/" {
				return &mounts[i]
			}
		}
	}
	return best
}

func readMountInfo() []mountEntry {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	var out []mountEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			continue
		}
		out = append(out, mountEntry{
			mountPoint: unescapeMount(fields[4]),
			fsType:     fields[sep+1],
			source:     unescapeMount(fields[sep+2]),
		})
	}
	return out
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func deviceAttributes(source string) (deviceType, bus string) {
	if source == "" {
		return "unknown", "unknown"
	}
	base := filepath.Base(source)
	switch {
	case strings.HasPrefix(base, "nvme"):
		bus = "nvme"
	case strings.HasPrefix(base, "sd"), strings.HasPrefix(base, "hd"):
		bus = "sata"
	case strings.HasPrefix(base, "vd"):
		bus = "virtio"
	case strings.HasPrefix(base, "dm-"), strings.Contains(source, "/mapper/"):
		bus = "device_mapper"
	case source == "overlay" || source == "tmpfs" || source == "none" || strings.HasPrefix(source, "//"):
		bus = "virtual"
	default:
		bus = "unknown"
	}
	deviceType = "unknown"
	if bus == "nvme" || bus == "sata" || bus == "virtio" {
		device := base
		if bus == "nvme" {
			if i := strings.Index(device, "p"); i > 0 {
				device = device[:i]
			}
		} else {
			device = strings.TrimRight(device, "0123456789")
		}
		if data, err := os.ReadFile("/sys/block/" + device + "/rotational"); err == nil {
			switch strings.TrimSpace(string(data)) {
			case "0":
				deviceType = "ssd"
			case "1":
				deviceType = "hdd"
			}
		}
	}
	return deviceType, bus
}

func writableByProcess(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Creating an entry in a directory requires write *and* search (execute)
	// permission. Access(W_OK) alone reports a 0200 directory as writable even
	// though creates fail with EACCES, so require X_OK for directories.
	mode := uint32(2) // W_OK
	if info.IsDir() {
		mode |= 1 // X_OK
	}
	return syscall.Access(path, mode) == nil
}

func readableByProcess(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	mode := uint32(4) // R_OK
	if info.IsDir() {
		// Listing a directory needs read and search permission.
		mode |= 1 // X_OK
	}
	return syscall.Access(path, mode) == nil
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func sanitizeID(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '/', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() > 96 {
		return b.String()[:96]
	}
	return b.String()
}

// displayEndpoint strips URL userinfo, query and fragment before an endpoint
// is shown to an operator. A base URL should not carry any of them, and this
// surface must never become a credential-echo path if a configuration
// validator is ever loosened.
func displayEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(invalid endpoint)"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
