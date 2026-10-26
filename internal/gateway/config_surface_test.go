package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
)

const configTestControlKey = "config-surface-test-key"

func configTestServer(t *testing.T, cfg config.Config, opts ...ServerOption) *Server {
	t.Helper()
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	base := []ServerOption{
		WithControlAPIKey(configTestControlKey),
		WithConfigurationSnapshot(cfg.Snapshot()),
	}
	return NewServer(engine, ":0", append(base, opts...)...)
}

func testBraveDescriptor() capability.ResearchProviderDescriptor {
	return capability.ResearchProviderDescriptor{
		Provider:      "brave",
		Kind:          capability.ResearchKindLive,
		Registered:    true,
		Live:          true,
		CredentialEnv: "BRAVE_API_KEY",
		Endpoint:      capability.DefaultBraveEndpoint,
		SelectedBy:    "NEXUS_CAPABILITY_WEB",
	}
}

func configTestRequest(t *testing.T, srv *Server, method, path string, body []byte, key string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Nexus.ID = "nx:nexus:config-test"
	cfg.Storage.DataDir = t.TempDir()
	cfg.Models.Providers = []config.ModelProviderConfig{{
		ID:        "primary",
		Name:      "Primary OpenAI-compatible endpoint",
		Endpoint:  "https://models.example.invalid/v1",
		ModelID:   "test-model",
		APIKeyEnv: "NEXUS_CONFIG_TEST_MODEL_KEY",
	}}
	return cfg
}

// TEST-CONFIG-001: the control-plane key gate covers every new route.
func TestConfigurationSurfaceRequiresControlKey(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	// A process with no control key configured must fail closed with 403, not
	// fall through to the route.
	noKeyEngine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	noKeySrv := NewServer(noKeyEngine, ":0", WithConfigurationSnapshot(cfg.Snapshot()))
	w := configTestRequest(t, noKeySrv, http.MethodGet, "/api/v1/control/config", nil, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled status = %d, want 403; body=%s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name string
		key  string
		want int
	}{
		{name: "rejected with wrong key", key: "wrong", want: http.StatusUnauthorized},
		{name: "rejected with missing key", key: "", want: http.StatusUnauthorized},
		{name: "accepted with key", key: configTestControlKey, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, tc.key)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.want != http.StatusOK {
				var envelope struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode error: %v", err)
				}
				if envelope.Error.Code == "" {
					t.Fatal("expected error envelope")
				}
			}
		})
	}
}

// TEST-CONFIG-002: the snapshot never echoes a credential value and reports
// presence as a boolean-like status.
func TestConfigurationSurfaceRedactsCredentials(t *testing.T) {
	const secret = "sk-config-surface-must-not-leak"
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", secret)
	t.Setenv("BRAVE_API_KEY", secret)
	t.Setenv("NEXUS_CAPABILITY_WEB", "brave")
	cfg := testConfig(t)
	cfg.Models.Providers[0].Endpoint = "https://user:url-secret@models.example.invalid/v1?token=query-secret"
	srv := configTestServer(t, cfg, WithWebSearchSelection(testBraveDescriptor()))

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	for _, leaked := range []string{secret, "url-secret", "query-secret"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Fatalf("credential value %q leaked in configuration response", leaked)
		}
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.ModelProviders) != 1 {
		t.Fatalf("model providers = %d, want 1", len(view.ModelProviders))
	}
	p := view.ModelProviders[0]
	if p.CredentialStatus != credentialPresent {
		t.Fatalf("credential status = %q, want present", p.CredentialStatus)
	}
	if p.APIKeyEnv != "NEXUS_CONFIG_TEST_MODEL_KEY" {
		t.Fatalf("api_key_env = %q", p.APIKeyEnv)
	}
	if p.Endpoint != "https://models.example.invalid/v1" {
		t.Fatalf("endpoint = %q, want redacted URL", p.Endpoint)
	}
	if p.LiveVerified {
		t.Fatal("a boot-time configuration must not claim live verification")
	}
	if view.WebSearch.Provider != "brave" || view.WebSearch.CredentialStatus != credentialPresent {
		t.Fatalf("web search = %+v", view.WebSearch)
	}
	if view.Readiness.LiveVerified || view.Readiness.GateZeroAuthorized {
		t.Fatal("readiness must not claim live verification or Gate 0 authorization")
	}
	if view.CredentialStorage != "environment_only" {
		t.Fatalf("credential storage = %q, want environment_only", view.CredentialStorage)
	}
}

// TEST-CONFIG-003: missing credentials and a disabled search provider are
// reported as blocking, not as a healthy state.
func TestConfigurationSurfaceReportsBlockers(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	cfg.Models.Providers = nil
	srv := configTestServer(t, cfg)

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.ModelRouting.Mode != "simulated" || !view.ModelRouting.Seeded {
		t.Fatalf("model routing = %+v", view.ModelRouting)
	}
	if view.WebSearch.Provider != "disabled" {
		t.Fatalf("web search provider = %q", view.WebSearch.Provider)
	}
	if view.Readiness.LivePreconditionsMet {
		t.Fatal("live preconditions must not be met with no live provider and no Gate 0")
	}
	joined := strings.Join(view.Readiness.Blocking, "\n")
	for _, want := range []string{"No real model provider", "web.search is disabled", "Gate 0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("blocking list missing %q: %v", want, view.Readiness.Blocking)
		}
	}
}

// TEST-CONFIG-004: storage is inspected through bound candidates only.
func TestConfigurationSurfaceStorageIsBounded(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config/storage", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp StorageVolumesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HostKind != "local_host" || resp.Discovery != "bounded" {
		t.Fatalf("response = %+v", resp)
	}
	foundData := false
	for _, v := range resp.Volumes {
		if v.IsDataDir {
			foundData = true
			if !v.Writable {
				t.Fatalf("temp data dir should be writable: %+v", v)
			}
			if v.TotalBytes == 0 {
				t.Fatalf("expected capacity for data dir: %+v", v)
			}
		}
		if strings.HasPrefix(v.Path, "/etc") || strings.HasPrefix(v.Path, "/proc") || strings.HasPrefix(v.Path, "/sys") {
			t.Fatalf("sensitive path exposed: %s", v.Path)
		}
	}
	if !foundData {
		t.Fatalf("configured data dir missing from volumes: %+v", resp.Volumes)
	}
}

// TEST-CONFIG-005: validation is server-side, non-mutating and honest about
// not supporting apply or migration.
func TestValidateStoragePath(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)
	dir := t.TempDir()

	post := func(path string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(StorageValidateRequest{Path: path})
		return configTestRequest(t, srv, http.MethodPost,
			"/api/v1/control/config/storage/validate", body, configTestControlKey)
	}

	t.Run("existing directory", func(t *testing.T) {
		w := post(dir)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var v StorageValidation
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !v.Valid || !v.Exists || !v.IsDirectory || !v.Writable {
			t.Fatalf("validation = %+v", v)
		}
		if v.ApplyingSupported || !v.RestartRequired || v.MigrationSupported {
			t.Fatalf("apply/migration claims must stay false: %+v", v)
		}
		if v.WriteProbe != "access_check" {
			t.Fatalf("write probe = %q", v.WriteProbe)
		}
	})

	t.Run("new directory under allowed root", func(t *testing.T) {
		child := filepath.Join(dir, "nested-data")
		w := post(child)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var v StorageValidation
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !v.Valid || v.Exists || !v.ParentExists || !v.Writable {
			t.Fatalf("validation = %+v", v)
		}
		if _, statErr := os.Stat(child); !os.IsNotExist(statErr) {
			t.Fatalf("validation must not create the path (stat err = %v)", statErr)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		w := post(dir + "/../etc")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("sensitive root rejected", func(t *testing.T) {
		for _, path := range []string{"/etc", "/etc/nexus", "/proc", "/sys", "/boot", "/var/log/nexus"} {
			w := post(path)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: status = %d; body=%s", path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("sensitive root stays rejected when roots are widened", func(t *testing.T) {
		t.Setenv("NEXUS_STORAGE_ROOTS", "/")
		for _, path := range []string{"/etc", "/proc", "/sys", "/dev", "/boot", "/root", "/var/log"} {
			w := post(path)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: status = %d; body=%s", path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("relative path rejected", func(t *testing.T) {
		w := post("relative/path")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("regular file rejected", func(t *testing.T) {
		file := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		w := post(file)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var v StorageValidation
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if v.Valid {
			t.Fatalf("regular file must not validate: %+v", v)
		}
	})

	t.Run("symlink rejected", func(t *testing.T) {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		w := post(link)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var v StorageValidation
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if v.Valid {
			t.Fatalf("symlink must not validate: %+v", v)
		}
	})

	t.Run("symlinked parent escaping allowed roots rejected", func(t *testing.T) {
		link := filepath.Join(dir, "etc-link")
		if err := os.Symlink("/etc", link); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		w := post(filepath.Join(link, "nexus-data"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
		}
		var v StorageValidation
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if v.Valid {
			t.Fatalf("symlinked parent escape must not validate: %+v", v)
		}
		sawParentFailure := false
		for _, check := range v.Checks {
			if check.ID == "parent" && check.Status == "fail" {
				sawParentFailure = true
			}
		}
		if !sawParentFailure {
			t.Fatalf("missing parent failure check: %+v", v.Checks)
		}
	})
}

// TEST-CONFIG-006: control responses are uncacheable, actor credentials cannot
// substitute for the control key, and method/prefix errors stay behind the gate.
func TestConfigurationSurfaceControlHardening(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)
	configPath := "/api/v1/control/config"

	t.Run("actor credentials do not substitute for the control key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, configPath, nil)
		req.Header.Set("X-Actor-ID", "nx:human:test")
		req.Header.Set("X-Actor-Credential", "actor-credential-must-not-grant-control")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "actor-credential-must-not-grant-control") {
			t.Fatal("actor credential was echoed in the error response")
		}
	})

	t.Run("success and failure responses are not cacheable", func(t *testing.T) {
		unauthorized := configTestRequest(t, srv, http.MethodGet, configPath, nil, "")
		if unauthorized.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d; body=%s", unauthorized.Code, unauthorized.Body.String())
		}
		if got := unauthorized.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("401 Cache-Control = %q, want no-store", got)
		}

		ok := configTestRequest(t, srv, http.MethodGet, configPath, nil, configTestControlKey)
		if ok.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", ok.Code, ok.Body.String())
		}
		if got := ok.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("200 Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("bare control prefix is gated before routing", func(t *testing.T) {
		w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control", nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
		w = configTestRequest(t, srv, http.MethodGet, "/api/v1/control", nil, configTestControlKey)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("method mismatch is rejected before routing without a key", func(t *testing.T) {
		w := configTestRequest(t, srv, http.MethodDelete, configPath, nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
		w = configTestRequest(t, srv, http.MethodDelete, configPath, nil, configTestControlKey)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405; body=%s", w.Code, w.Body.String())
		}
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
			t.Fatalf("Allow = %q, want GET", allow)
		}
	})
}

// TEST-CONFIG-007: malformed, oversized, unknown-field and multi-value bodies
// are rejected without echoing caller-controlled paths or secret values.
func TestValidateStoragePathRejectsMalformedRequests(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	postRaw := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/control/config/storage/validate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", configTestControlKey)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed json", body: `{"path":`},
		{name: "empty path", body: `{"path":""}`},
		{name: "whitespace path", body: `{"path":"   "}`},
		{name: "relative path", body: `{"path":"tmp/data"}`},
		{name: "traversal", body: `{"path":"/tmp/../etc"}`},
		{name: "nul byte", body: `{"path":"/tmp/\u0000data"}`},
		{name: "wrong type", body: `{"path":123}`},
		{name: "array body", body: `["/tmp"]`},
		{name: "unknown field", body: `{"path":"/tmp/nexus-ok","extra":true}`},
		{name: "trailing value", body: `{"path":"/tmp/nexus-ok"}{"path":"/etc"}`},
		{name: "oversized", body: `{"path":"/tmp/` + strings.Repeat("a", 5000) + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postRaw(tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Error.Code != "VALIDATION" {
				t.Fatalf("code = %q, want VALIDATION", envelope.Error.Code)
			}
			if strings.Contains(w.Body.String(), "nexus-ok") {
				t.Fatal("caller-controlled path was echoed in the error response")
			}
		})
	}

	t.Run("sensitive path is not echoed", func(t *testing.T) {
		w := postRaw(`{"path":"/etc/shadow"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "/etc/shadow") {
			t.Fatal("sensitive path was echoed in the error response")
		}
	})
}

// TEST-CONFIG-008: a non-writable parent is a failed check, not a pass with a
// contradictory writability warning.
func TestValidateStoragePathParentWritabilityIsHonest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed for root")
	}
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	base := t.TempDir()
	parent := filepath.Join(base, "read-only-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	child := filepath.Join(parent, "new-data")
	body, _ := json.Marshal(StorageValidateRequest{Path: child})
	w := configTestRequest(t, srv, http.MethodPost,
		"/api/v1/control/config/storage/validate", body, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var v StorageValidation
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Valid || v.Writable {
		t.Fatalf("non-writable parent must fail validation: %+v", v)
	}
	sawParentFailure := false
	for _, check := range v.Checks {
		if check.ID != "parent" {
			continue
		}
		if check.Status == "pass" {
			t.Fatal("parent was reported as writable when it is not")
		}
		if check.Status == "fail" {
			sawParentFailure = true
			if !strings.Contains(strings.ToLower(check.Message), "not writable") {
				t.Fatalf("parent failure did not explain writability: %q", check.Message)
			}
		}
	}
	if !sawParentFailure {
		t.Fatalf("missing parent failure check: %+v", v.Checks)
	}
	if _, statErr := os.Stat(child); !os.IsNotExist(statErr) {
		t.Fatalf("validation must not create the path (stat err = %v)", statErr)
	}
}

// TEST-CONFIG-009: a directory with W_OK but no X_OK is not usable for creating
// entries, so it must not be reported writable.
func TestValidateStoragePathDirectoryRequiresExecute(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed for root")
	}
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	base := t.TempDir()
	target := filepath.Join(base, "write-only-dir")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })

	if probe, err := os.Create(filepath.Join(target, "probe")); err == nil {
		_ = probe.Close()
		t.Skip("environment does not enforce directory search permission")
	}

	body, _ := json.Marshal(StorageValidateRequest{Path: target})
	w := configTestRequest(t, srv, http.MethodPost,
		"/api/v1/control/config/storage/validate", body, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var v StorageValidation
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Writable || v.Valid {
		t.Fatalf("directory without search permission must not be writable: %+v", v)
	}
}

// TEST-CONFIG-010: a directory that is writable but unreadable cannot hydrate
// the file store, so it must not validate as usable.
func TestValidateStoragePathDirectoryRequiresRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed for root")
	}
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	base := t.TempDir()
	target := filepath.Join(base, "unreadable-dir")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })

	if entries, err := os.ReadDir(target); err == nil {
		t.Skipf("environment does not enforce directory read permission (saw %d entries)", len(entries))
	}

	body, _ := json.Marshal(StorageValidateRequest{Path: target})
	w := configTestRequest(t, srv, http.MethodPost,
		"/api/v1/control/config/storage/validate", body, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var v StorageValidation
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Valid {
		t.Fatalf("unreadable directory must not validate as usable: %+v", v)
	}
	sawReadableFailure := false
	for _, check := range v.Checks {
		if check.ID == "readable" && check.Status == "fail" {
			sawReadableFailure = true
		}
	}
	if !sawReadableFailure {
		t.Fatalf("missing readable failure check: %+v", v.Checks)
	}
}

// TEST-CONFIG-011: configured/live-selected providers and credentials are not
// proof of live readiness; Gate 0 stays false and blocks the claim.
func TestConfigurationSurfaceDoesNotClaimLiveReadiness(t *testing.T) {
	const secret = "sk-config-surface-live-claim-must-not-leak"
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", secret)
	t.Setenv("BRAVE_API_KEY", secret)
	t.Setenv("NEXUS_CAPABILITY_WEB", "brave")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg, WithWebSearchSelection(testBraveDescriptor()))

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !view.WebSearch.Live {
		t.Fatalf("brave should be selected as a live provider: %+v", view.WebSearch)
	}
	if view.WebSearch.LiveVerified || view.Readiness.LiveVerified || view.Readiness.GateZeroAuthorized {
		t.Fatalf("configuration must not claim live verification: %+v / %+v", view.WebSearch, view.Readiness)
	}
	if view.Readiness.LivePreconditionsMet {
		t.Fatalf("live preconditions must not be met without Gate 0: %+v", view.Readiness)
	}
	joined := strings.Join(view.Readiness.Blocking, "\n")
	if !strings.Contains(joined, "Gate 0") {
		t.Fatalf("Gate 0 must remain a blocking item: %v", view.Readiness.Blocking)
	}
	if !view.Readiness.ModelCredentialsPresent || !view.Readiness.WebSearchCredentialReady {
		t.Fatalf("boot-captured credentials should be reported present: %+v", view.Readiness)
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("credential value leaked in the readiness response")
	}
}

// TEST-CONFIG-012: the surface projects the boot snapshot's fingerprint,
// schema version, section sources and immutable provenance.
func TestConfigurationSurfaceReportsProvenance(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	cfg := testConfig(t)
	srv := configTestServer(t, cfg)

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Fingerprint != cfg.Snapshot().Fingerprint() {
		t.Fatalf("fingerprint = %q, want %q", view.Fingerprint, cfg.Snapshot().Fingerprint())
	}
	if view.FingerprintVersion != config.FingerprintSchema {
		t.Fatalf("fingerprint_version = %q, want %q", view.FingerprintVersion, config.FingerprintSchema)
	}
	if !strings.HasPrefix(view.Fingerprint, view.FingerprintVersion+":") {
		t.Fatalf("fingerprint %q does not carry version %q", view.Fingerprint, view.FingerprintVersion)
	}
	if view.Provenance.Kind != "boot_snapshot" || !view.Provenance.Immutable {
		t.Fatalf("provenance = %+v", view.Provenance)
	}
	if view.Provenance.Precedence != "defaults < file < environment" {
		t.Fatalf("provenance precedence = %q", view.Provenance.Precedence)
	}
	if view.RuntimeMutable || view.RestartRequired != true {
		t.Fatalf("read-only framing = runtime_mutable:%v restart_required:%v",
			view.RuntimeMutable, view.RestartRequired)
	}
	for _, section := range config.SectionNames() {
		got, ok := view.Sources[section]
		if !ok {
			t.Fatalf("sources missing section %q: %v", section, view.Sources)
		}
		if got != string(config.SourceDefault) {
			t.Fatalf("source for %q = %q, want default for a defaults-only config", section, got)
		}
	}
	if _, ok := view.Sources["rate_limit"]; !ok {
		t.Fatal("rate_limit source must be reported")
	}
	if _, ok := view.Sources["http"]; !ok {
		t.Fatal("HTTP/CORS source must be reported")
	}
}

// TEST-CONFIG-013: environment changes after boot cannot retcon the reported
// credential status or web-search selection.
func TestConfigurationSurfaceProjectsBootCapture(t *testing.T) {
	const secret = "sk-boot-capture-secret"
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", secret)
	t.Setenv("BRAVE_API_KEY", secret)
	cfg := testConfig(t)
	srv := configTestServer(t, cfg, WithWebSearchSelection(testBraveDescriptor()))

	var before ConfigurationView
	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if before.WebSearch.Provider != "brave" || before.WebSearch.CredentialStatus != credentialPresent {
		t.Fatalf("boot web search = %+v", before.WebSearch)
	}
	if before.ModelProviders[0].CredentialStatus != credentialPresent {
		t.Fatalf("boot model credential = %q", before.ModelProviders[0].CredentialStatus)
	}

	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("NEXUS_CAPABILITY_WEB", "wikipedia")

	w = configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var after ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if after.WebSearch.Provider != before.WebSearch.Provider ||
		after.WebSearch.Kind != before.WebSearch.Kind ||
		after.WebSearch.SelectedBy != before.WebSearch.SelectedBy {
		t.Fatalf("web search selection changed after boot: before=%+v after=%+v", before.WebSearch, after.WebSearch)
	}
	if after.WebSearch.CredentialStatus != credentialPresent {
		t.Fatalf("web credential status changed after boot: %q", after.WebSearch.CredentialStatus)
	}
	if after.ModelProviders[0].CredentialStatus != credentialPresent {
		t.Fatalf("model credential status changed after boot: %q", after.ModelProviders[0].CredentialStatus)
	}
	if after.Fingerprint != before.Fingerprint {
		t.Fatalf("fingerprint changed after boot: %q -> %q", before.Fingerprint, after.Fingerprint)
	}
}

// TEST-CONFIG-014: the scripted adapter reports its canonical identifier and
// never masquerades as a live provider.
func TestConfigurationSurfaceScriptedDescriptor(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	desc := capability.DescribeResearchProvider(&capability.ScriptedResearchProvider{})
	desc.Registered = true
	desc.SelectedBy = "NEXUS_CAPABILITY_WEB"
	cfg := testConfig(t)
	srv := configTestServer(t, cfg, WithWebSearchSelection(desc))

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.WebSearch.Provider != "scripted-research" {
		t.Fatalf("scripted provider id = %q, want canonical scripted-research", view.WebSearch.Provider)
	}
	if view.WebSearch.Kind != capability.ResearchKindScripted || view.WebSearch.Mode != "scripted" {
		t.Fatalf("scripted descriptor misreported: %+v", view.WebSearch)
	}
	if view.WebSearch.Live || view.WebSearch.LiveVerified {
		t.Fatalf("scripted provider claimed liveness: %+v", view.WebSearch)
	}
	if view.WebSearch.CredentialStatus != credentialNotRequired {
		t.Fatalf("scripted credential status = %q", view.WebSearch.CredentialStatus)
	}
	if !view.WebSearch.ScriptedFallback || !view.WebSearch.Registered {
		t.Fatalf("scripted descriptor flags = %+v", view.WebSearch)
	}
	joined := strings.Join(view.Readiness.Blocking, "\n")
	if !strings.Contains(joined, "scripted provider") {
		t.Fatalf("scripted provider must remain a blocking item: %v", view.Readiness.Blocking)
	}
}

// TEST-CONFIG-015: a provider injected by an embedder is reported as injected
// with unknown liveness and credential requirements, not guessed live.
func TestConfigurationSurfaceInjectedDescriptor(t *testing.T) {
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	desc := capability.DescribeResearchProvider(injectedResearchProvider{})
	if desc.Kind != capability.ResearchKindInjected {
		t.Fatalf("described kind = %q, want injected", desc.Kind)
	}
	desc.Registered = true
	cfg := testConfig(t)
	srv := configTestServer(t, cfg, WithWebSearchSelection(desc))

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.WebSearch.Kind != capability.ResearchKindInjected || view.WebSearch.Mode != "injected" {
		t.Fatalf("injected descriptor misreported: %+v", view.WebSearch)
	}
	if view.WebSearch.Live || view.WebSearch.LiveVerified {
		t.Fatalf("injected provider claimed liveness: %+v", view.WebSearch)
	}
	if view.WebSearch.CredentialStatus != credentialUnknown {
		t.Fatalf("injected credential status = %q, want unknown", view.WebSearch.CredentialStatus)
	}
	if view.WebSearch.CredentialEnv != "" {
		t.Fatalf("injected provider must not invent a credential env: %q", view.WebSearch.CredentialEnv)
	}
	joined := strings.Join(view.Readiness.Blocking, "\n")
	if !strings.Contains(joined, "injected adapter") {
		t.Fatalf("injected provider must remain a blocking item: %v", view.Readiness.Blocking)
	}
}

// TEST-CONFIG-016: descriptor endpoints are projected without userinfo, query
// or fragment, so a credential-bearing URL cannot leak.
func TestConfigurationSurfaceWebSearchEndpointRedacted(t *testing.T) {
	const secret = "sk-websearch-endpoint-secret"
	t.Setenv("NEXUS_CONFIG_TEST_MODEL_KEY", "not-a-real-secret")
	desc := testBraveDescriptor()
	desc.Endpoint = "https://user:" + secret + "@search.example.invalid/v1?token=" + secret + "#frag"
	cfg := testConfig(t)
	srv := configTestServer(t, cfg, WithWebSearchSelection(desc))

	w := configTestRequest(t, srv, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("web-search endpoint credential leaked in configuration response")
	}
	var view ConfigurationView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.WebSearch.Endpoint != "https://search.example.invalid/v1" {
		t.Fatalf("endpoint = %q, want redacted URL", view.WebSearch.Endpoint)
	}
}

// injectedResearchProvider intentionally implements only the base adapter
// interface: it declares no kind, liveness or credential requirement.
type injectedResearchProvider struct{}

func (injectedResearchProvider) Identify() string { return "injected-adapter" }
func (injectedResearchProvider) Search(context.Context, string, int) ([]capability.Source, error) {
	return nil, nil
}

// TEST-CONFIG-017: routes answer 503 in the contract envelope when the process
// has no boot configuration snapshot.
func TestConfigurationSurfaceUnavailableWithoutConfiguration(t *testing.T) {
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv := NewServer(engine, ":0", WithControlAPIKey(configTestControlKey))

	// A web-search descriptor without a loaded snapshot is not a boot
	// configuration and must not make the surface available.
	webOnly := NewServer(engine, ":0",
		WithControlAPIKey(configTestControlKey),
		WithWebSearchSelection(testBraveDescriptor()))
	w := configTestRequest(t, webOnly, http.MethodGet, "/api/v1/control/config", nil, configTestControlKey)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("web-selection-only status = %d, want 503; body=%s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "config", method: http.MethodGet, path: "/api/v1/control/config"},
		{name: "storage", method: http.MethodGet, path: "/api/v1/control/config/storage"},
		{name: "validate", method: http.MethodPost, path: "/api/v1/control/config/storage/validate",
			body: []byte(`{"path":"/tmp"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := configTestRequest(t, srv, tc.method, tc.path, tc.body, configTestControlKey)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Error.Code != "SERVICE_UNAVAILABLE" {
				t.Fatalf("code = %q, want SERVICE_UNAVAILABLE", envelope.Error.Code)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}
