package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// prodProviderJSON is a minimal production configuration with a complete real
// provider; the caller supplies the storage.data_dir.
func prodProviderJSON(dataDir string) string {
	return `{
		"nexus": {"environment": "production"},
		"storage": {"data_dir": "` + dataDir + `"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`
}

// F2 — production without storage.data_dir fails at startup (config validation)
// and never constructs an in-memory App.
func TestAppProductionRequiresDurableStorage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"nexus": {"environment": "production"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{ConfigFile: path, Environ: noEnv()})
	if err == nil {
		t.Fatal("production without storage.data_dir must fail to start")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// F2 — production with a valid, isolated durable directory constructs an App
// backed by a real FileStore (never the nil in-memory store).
func TestAppProductionUsesFileStore(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	path := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(path, []byte(prodProviderJSON(dataDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(Options{ConfigFile: path, Environ: noEnv()})
	if err != nil {
		t.Fatalf("production with durable storage must construct: %v", err)
	}
	if a.Store() == nil {
		t.Fatal("production must use a durable FileStore, not the nil in-memory store")
	}
	// The durable directory is the one we configured.
	if _, statErr := os.Stat(dataDir); statErr != nil {
		t.Fatalf("configured storage directory must exist: %v", statErr)
	}
}

// F2 — a storage path that is a FILE (not a directory) fails closed; there is
// no fallback to an in-memory store.
func TestAppProductionStoragePathIsFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	fileInsteadOfDir := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(fileInsteadOfDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(prodProviderJSON(fileInsteadOfDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{ConfigFile: path, Environ: noEnv()})
	if err == nil {
		t.Fatal("a storage path that is a file must fail startup, not fall back to memory")
	}
}
