package backup

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// --- helpers ---------------------------------------------------------------

// seedStore builds a durable FileStore with a few records and returns its dir.
func seedStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fs, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	recs := []*store.Record{
		{ID: "id-alice", Type: store.RecordTypeIdentity, Status: store.RecordStatusActive, Data: []byte(`{"name":"alice"}`)},
		{ID: "biz-1", Type: store.RecordTypeBusiness, Status: store.RecordStatusActive, Data: []byte(`{"name":"biz"}`)},
		{ID: "cred-alice", Type: store.RecordTypeCredential, Status: store.RecordStatusActive, Data: []byte(`{"hash":"deadbeef"}`)},
		{ID: "mem-1", Type: store.RecordTypeMemory, Status: store.RecordStatusActive, Data: []byte(`{"k":"v"}`)},
	}
	for _, r := range recs {
		if err := fs.Put(r); err != nil {
			t.Fatalf("put %s: %v", r.ID, err)
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}
	return dir
}

// snapshotFiles returns path→sha256 for every durable file under dir.
func snapshotFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type tarEntry struct {
	name string
	data []byte
	link string // when set, a symlink entry (TypeSymlink)
	dir  bool
}

func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		var hdr *tar.Header
		switch {
		case e.link != "":
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeSymlink, Linkname: e.link, Mode: 0o777}
		case e.dir:
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeDir, Mode: 0o755}
		default:
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(e.data))}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.link == "" && !e.dir {
			if _, err := tw.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeArtifact(t *testing.T, entries []tarEntry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "artifact.tar")
	if err := os.WriteFile(p, buildTar(t, entries), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func manifestEntry(t *testing.T, man Manifest) tarEntry {
	t.Helper()
	data, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	return tarEntry{name: ManifestName, data: data}
}

func fileEntry(rel string, data []byte) (FileEntry, tarEntry) {
	sum := sha256.Sum256(data)
	return FileEntry{Path: rel, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])},
		tarEntry{name: rel, data: data}
}

// --- round-trip & equivalence ----------------------------------------------

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")

	man, err := Create(src, art)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if man.FormatVersion != FormatVersion || len(man.Files) != 4 {
		t.Fatalf("unexpected manifest: %+v", man)
	}
	if _, err := Verify(art); err != nil {
		t.Fatalf("verify: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Logical equivalence: reopen through the real store and compare records.
	if !sameLogicalState(t, src, dest) {
		t.Fatal("restored store is not logically equivalent to the source")
	}
	// Faithful copy: the restored files are byte-identical.
	if a, b := snapshotFiles(t, src), snapshotFiles(t, dest); !equalMaps(a, b) {
		t.Fatalf("restored files differ:\n src=%v\n dst=%v", a, b)
	}
}

// Backups and restored stores embed credential hashes and organizational
// data, so the artifact, restored directories and restored records must all
// be owner-only.
func TestBackupArtifactAndRestoredStoreAreOwnerOnly(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertMode(t, art, 0o600)

	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	assertMode(t, dest, 0o700)
	for _, typeDir := range []store.RecordType{
		store.RecordTypeIdentity, store.RecordTypeBusiness,
		store.RecordTypeCredential, store.RecordTypeMemory,
	} {
		assertMode(t, filepath.Join(dest, string(typeDir)), 0o700)
	}
	err := filepath.WalkDir(dest, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if got := dirMode(t, p); got != 0o700 {
				t.Errorf("restored directory %s mode = %04o, want 0700", p, got)
			}
			return nil
		}
		if got := dirMode(t, p); got != 0o600 {
			t.Errorf("restored file %s mode = %04o, want 0600", p, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk restored store: %v", err)
	}
}

// The live ownership marker is process state, not a durable record, and must
// never enter a backup manifest.
func TestBackupExcludesStorageLockMarker(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	man, err := Create(src, art)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, fe := range man.Files {
		if strings.Contains(fe.Path, ".nexus-store.lock") {
			t.Fatalf("backup must not include the ownership marker: %s", fe.Path)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if got := dirMode(t, path); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func dirMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestRestoreIsDeterministicAcrossDestinations(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	d1 := filepath.Join(t.TempDir(), "r1")
	d2 := filepath.Join(t.TempDir(), "r2")
	if _, err := Restore(art, d1); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(art, d2); err != nil {
		t.Fatal(err)
	}
	if !equalMaps(snapshotFiles(t, d1), snapshotFiles(t, d2)) {
		t.Fatal("two restores of the same artifact differ")
	}
}

func TestBackupDoesNotMutateSource(t *testing.T) {
	src := seedStore(t)
	before := snapshotFiles(t, src)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	if !equalMaps(before, snapshotFiles(t, src)) {
		t.Fatal("backup mutated the source data directory")
	}
}

func TestBackupIncludesBatchJournalAndRestoreRecovers(t *testing.T) {
	src := seedStore(t)
	// Simulate a crash left behind a valid batch journal (pre-batch record).
	journal := `{"version":1,"records":[{"id":"j-recovered","type":"identity","status":"active","version":1,` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","data":"eyJ4IjoxfQ=="}]}`
	if err := os.WriteFile(filepath.Join(src, ".batch-journal.json"), []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(t.TempDir(), "b.tar")
	man, err := Create(src, art)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := findEntry(man, ".batch-journal.json"); !ok {
		t.Fatal("manifest must include the batch journal")
	}

	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// Recovery ran on reopen: the journal is gone and the record now exists.
	if FileExists(filepath.Join(dest, ".batch-journal.json")) {
		t.Fatal("journal should have been consumed by store recovery")
	}
	if !FileExists(filepath.Join(dest, "identity", "j-recovered.json")) {
		t.Fatal("recovery should have materialized the pre-batch record")
	}
}

// --- integrity & validation -------------------------------------------------

func TestVerifyDetectsCorruptChecksum(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	// Flip a byte inside the artifact (a record's JSON contains '"name"').
	data, _ := os.ReadFile(art)
	idx := bytes.Index(data, []byte(`alice`))
	if idx < 0 {
		t.Fatal("expected record content in artifact")
	}
	data[idx] ^= 0x01
	if err := os.WriteFile(art, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(art); err == nil {
		t.Fatal("a checksum-corrupt artifact must be rejected")
	}
}

func TestVerifyRejectsMalformedManifest(t *testing.T) {
	art := writeArtifact(t, []tarEntry{{name: ManifestName, data: []byte("{not-json")}})
	if _, err := Verify(art); err == nil {
		t.Fatal("malformed manifest must be rejected")
	}
}

func TestVerifyRejectsUnsupportedFormatVersion(t *testing.T) {
	man := Manifest{Format: Format, FormatVersion: 99, StoreLayout: StoreLayout}
	art := writeArtifact(t, []tarEntry{manifestEntry(t, man)})
	if _, err := Verify(art); err == nil {
		t.Fatal("unsupported format version must be rejected")
	}
}

func TestVerifyRejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, bad := range []string{"../evil.json", "/etc/passwd", "a/../../b.json", "a/./b.json", `a\b.json`} {
		man := Manifest{Format: Format, FormatVersion: FormatVersion, Files: []FileEntry{{Path: bad, Size: 0, SHA256: ""}}}
		art := writeArtifact(t, []tarEntry{manifestEntry(t, man)})
		if _, err := Verify(art); err == nil {
			t.Fatalf("manifest path %q must be rejected", bad)
		}
	}
}

func TestVerifyRejectsUnlistedAndMissingEntries(t *testing.T) {
	fe, ent := fileEntry("identity/a.json", []byte(`{"id":"a"}`))
	man := Manifest{Format: Format, FormatVersion: FormatVersion, Files: []FileEntry{fe}}

	// Extra entry not listed in the manifest.
	extra := tarEntry{name: "identity/b.json", data: []byte(`{"id":"b"}`)}
	art := writeArtifact(t, []tarEntry{manifestEntry(t, man), ent, extra})
	if _, err := Verify(art); err == nil {
		t.Fatal("an unlisted entry must be rejected")
	}

	// Manifest lists a file the artifact omits.
	art2 := writeArtifact(t, []tarEntry{manifestEntry(t, man)})
	if _, err := Verify(art2); err == nil {
		t.Fatal("a missing listed entry must be rejected")
	}
}

func TestVerifyRejectsSymlinkEntry(t *testing.T) {
	man := Manifest{Format: Format, FormatVersion: FormatVersion}
	art := writeArtifact(t, []tarEntry{manifestEntry(t, man), {name: "identity/x.json", link: "/etc/passwd"}})
	if _, err := Verify(art); err == nil {
		t.Fatal("a symlink entry must be rejected")
	}
}

func TestVerifyRejectsTruncatedArtifact(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(art)
	if err := os.WriteFile(art, data[:len(data)-20], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(art); err == nil {
		t.Fatal("a truncated artifact must be rejected")
	}
}

func TestCreateRefusesExistingArtifact(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if err := os.WriteFile(art, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(src, art); err == nil {
		t.Fatal("backup must refuse to overwrite an existing artifact")
	}
	if b, _ := os.ReadFile(art); string(b) != "preexisting" {
		t.Fatal("the existing artifact must be left untouched")
	}
}

// --- restore safety ---------------------------------------------------------

func TestRestoreRefusesNonEmptyDestination(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir() // non-empty by construction (temp dir may be empty; add a file)
	if err := os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(art, dest); err == nil {
		t.Fatal("restore into a non-empty destination must be refused")
	}
	if !FileExists(filepath.Join(dest, "keep.txt")) {
		t.Fatal("the existing destination content must be untouched")
	}
}

func TestRestoreFailureLeavesEverythingUntouched(t *testing.T) {
	src := seedStore(t)
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatal(err)
	}
	// Corrupt the artifact so restore fails after starting.
	data, _ := os.ReadFile(art)
	idx := bytes.Index(data, []byte(`alice`))
	data[idx] ^= 0xFF
	if err := os.WriteFile(art, data, 0o644); err != nil {
		t.Fatal(err)
	}

	srcBefore := snapshotFiles(t, src)
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err == nil {
		t.Fatal("restore of a corrupt artifact must fail")
	}
	if !equalMaps(srcBefore, snapshotFiles(t, src)) {
		t.Fatal("a failed restore must not touch the source")
	}
	if FileExists(dest) {
		t.Fatal("a failed restore must not publish a partial destination")
	}
	// No staging directory left behind.
	parent := filepath.Dir(dest)
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.Name() != filepath.Base(dest) {
			continue
		}
		t.Fatalf("unexpected leftover %q", e.Name())
	}
}

// --- exclusions -------------------------------------------------------------

func TestBackupExcludesSandboxEnvAndTempFiles(t *testing.T) {
	src := seedStore(t)
	// Filesystem sandbox content (never part of the durable store backup).
	if err := os.MkdirAll(filepath.Join(src, "workspaces", "biz-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "workspaces", "biz-1", "secret.txt"), []byte("SANDBOX-CONTENT-MARKER-9137"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A host environment file at the root and a transient temp file in a type dir.
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("API_KEY=leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "identity", ".id-alice-123.tmp"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	art := filepath.Join(t.TempDir(), "b.tar")
	man, err := Create(src, art)
	if err != nil {
		t.Fatal(err)
	}
	for _, fe := range man.Files {
		switch {
		case fe.Path == ".env", fe.Path == ".batch-journal.json":
			t.Fatalf("backup must not include %s", fe.Path)
		case len(fe.Path) >= 4 && fe.Path[len(fe.Path)-4:] == ".tmp":
			t.Fatalf("backup must not include temp file %s", fe.Path)
		case filepath.ToSlash(fe.Path) == "workspaces/biz-1/secret.txt":
			t.Fatal("backup must not include sandbox content")
		}
	}
	// And the raw artifact must not contain the excluded bytes.
	raw, _ := os.ReadFile(art)
	if bytes.Contains(raw, []byte("API_KEY=leak")) || bytes.Contains(raw, []byte("SANDBOX-CONTENT-MARKER-9137")) {
		t.Fatal("artifact leaked excluded content")
	}
}

func TestBackupRejectsSymlinkedTypeDirectory(t *testing.T) {
	src := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "identity")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err == nil {
		t.Fatal("a symlinked type directory must be refused")
	}
}

// --- helpers for comparison -------------------------------------------------

func sameLogicalState(t *testing.T, dirA, dirB string) bool {
	t.Helper()
	a := loadAll(t, dirA)
	b := loadAll(t, dirB)
	if len(a) != len(b) {
		return false
	}
	for id, ra := range a {
		rb, ok := b[id]
		if !ok || ra.Type != rb.Type || !bytes.Equal(ra.Data, rb.Data) {
			return false
		}
	}
	return true
}

func loadAll(t *testing.T, dir string) map[string]*store.Record {
	t.Helper()
	fs, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store %s: %v", dir, err)
	}
	out := map[string]*store.Record{}
	// Enumerate every type found on disk.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recs, err := fs.List(store.Filter{Type: store.RecordType(e.Name()), Status: store.RecordStatusActive})
		if err != nil {
			t.Fatalf("list %s: %v", e.Name(), err)
		}
		for _, r := range recs {
			out[r.ID] = r
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("close store %s: %v", dir, err)
	}
	return out
}

func findEntry(man *Manifest, p string) (FileEntry, bool) {
	for _, fe := range man.Files {
		if fe.Path == p {
			return fe, true
		}
	}
	return FileEntry{}, false
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

var _ = time.Now // keep time imported for future timestamp assertions
