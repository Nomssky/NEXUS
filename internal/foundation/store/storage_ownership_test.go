package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	storeHelperEnv         = "NEXUS_TEST_STORE_HELPER"
	storeHelperDirEnv      = "NEXUS_TEST_STORE_DIR"
	storeHelperGracefulEnv = "NEXUS_TEST_STORE_GRACEFUL"
)

func requireStorageLockSupport(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
	default:
		t.Skipf("exclusive storage ownership is not supported on %s", runtime.GOOS)
	}
}

func requireStoreCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *StoreError
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("expected store error %s, got %v", code, err)
	}
}

func mustOpenStore(t *testing.T, dir string) *FileStore {
	t.Helper()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store %s: %v", dir, err)
	}
	return fs
}

// TestStoreOwnershipHelperProcess is not a test body in the ordinary sense: it
// is the real child-process side of the cross-process ownership tests. The
// parent starts this exact test binary and communicates through files/stdin.
func TestStoreOwnershipHelperProcess(t *testing.T) {
	if os.Getenv(storeHelperEnv) != "1" {
		return
	}
	fs, err := NewFileStore(os.Getenv(storeHelperDirEnv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper open: %v\n", err)
		os.Exit(2)
	}
	fmt.Println("HELPER_READY")
	if os.Getenv(storeHelperGracefulEnv) == "1" {
		var b [1]byte
		_, _ = io.ReadFull(os.Stdin, b[:])
		if err := fs.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "helper close: %v\n", err)
			os.Exit(3)
		}
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

type storeHelper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
}

func startStoreHelper(t *testing.T, dir string, graceful bool) *storeHelper {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreOwnershipHelperProcess$")
	env := append(os.Environ(), storeHelperEnv+"=1", storeHelperDirEnv+"="+dir)
	if graceful {
		env = append(env, storeHelperGracefulEnv+"=1")
	}
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	ready := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
			return
		}
		if err := scanner.Err(); err != nil {
			readErr <- err
			return
		}
		readErr <- io.EOF
	}()

	select {
	case line := <-ready:
		if line != "HELPER_READY" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("unexpected helper readiness line %q (stderr: %s)", line, stderr.String())
		}
	case err := <-readErr:
		_ = cmd.Wait()
		t.Fatalf("helper exited before readiness: %v (stderr: %s)", err, stderr.String())
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper did not become ready (stderr: %s)", stderr.String())
	}
	return &storeHelper{cmd: cmd, stdin: stdin, stderr: &stderr}
}

func TestFileStoreRejectsSecondOwnerInProcess(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	fs := mustOpenStore(t, dir)
	defer func() { _ = fs.Close() }()

	if _, err := NewFileStore(dir); !errors.Is(err, ErrStorageOwned) {
		t.Fatalf("second open must fail with ErrStorageOwned, got %v", err)
	}
	// The failed contender must not disturb the owner.
	if err := fs.Put(&Record{ID: "owned", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatalf("owner write after refused contender: %v", err)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("close owner: %v", err)
	}
	fs2 := mustOpenStore(t, dir)
	if got, err := fs2.Get("owned"); err != nil || got == nil {
		t.Fatalf("record must survive ownership handover: got=%v err=%v", got, err)
	}
	_ = fs2.Close()
}

func TestFileStoreCrashReleasesOwnershipAcrossProcesses(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	helper := startStoreHelper(t, dir, false)

	// A different process (this test process) is refused while the child owns.
	if _, err := NewFileStore(dir); !errors.Is(err, ErrStorageOwned) {
		_ = helper.cmd.Process.Kill()
		_ = helper.cmd.Wait()
		t.Fatalf("parent open must be refused while the child owns the store, got %v", err)
	}

	// SIGKILL is a crash, not an orderly close. The kernel must release the
	// flock when the child dies; the on-disk lock file intentionally remains.
	if err := helper.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = helper.cmd.Wait()

	if _, err := os.Stat(filepath.Join(dir, storageLockName)); err != nil {
		t.Fatalf("the lock marker must remain on disk after a crash: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		fs, err := NewFileStore(dir)
		if err == nil {
			_ = fs.Close()
			break
		}
		if !errors.Is(err, ErrStorageOwned) {
			t.Fatalf("unexpected reopen error: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("crash did not release storage ownership: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFileStoreOrderlyExitReleasesOwnershipAcrossProcesses(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	helper := startStoreHelper(t, dir, true)

	if _, err := NewFileStore(dir); !errors.Is(err, ErrStorageOwned) {
		_ = helper.stdin.Close()
		_ = helper.cmd.Process.Kill()
		_ = helper.cmd.Wait()
		t.Fatalf("parent open must be refused while the child owns the store, got %v", err)
	}

	// Tell the child to close its store and exit normally.
	if _, err := helper.stdin.Write([]byte("x")); err != nil {
		t.Fatalf("signal helper: %v", err)
	}
	_ = helper.stdin.Close()
	if err := helper.cmd.Wait(); err != nil {
		t.Fatalf("helper graceful exit: %v (stderr: %s)", err, helper.stderr.String())
	}

	fs := mustOpenStore(t, dir)
	_ = fs.Close()
}

func TestFileStoreFailedOpenReleasesOwnership(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	t.Logf("test dir: %s", dir)
	typeDir := filepath.Join(dir, "memory")
	if err := os.Mkdir(typeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(typeDir, "bad.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("a corrupt record must abort the open")
	}
	if err := os.Remove(corrupt); err != nil {
		t.Fatal(err)
	}
	// If the failed open had retained the lock, this would return
	// ErrStorageOwned instead of succeeding.
	fs := mustOpenStore(t, dir)
	_ = fs.Close()
}

// TestStoreRejectsUnsafeBatchJournal records that a crash journal carrying an
// unsafe record id or type aborts the open rather than being applied.
func TestStoreRejectsUnsafeBatchJournal(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	fs := mustOpenStore(t, dir)
	if err := fs.Put(&Record{ID: "real", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	_ = fs.Close()

	writeJournal := func(t *testing.T, records []Record) {
		t.Helper()
		jdata, err := json.Marshal(batchJournal{Version: 1, Records: records})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, batchJournalName), jdata, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("unsafe id", func(t *testing.T) {
		writeJournal(t, []Record{{ID: "../escape", Type: RecordTypeMemory, Status: RecordStatusActive}})
		if _, err := NewFileStore(dir); err == nil {
			t.Fatal("a batch journal with an unsafe id must abort the open")
		}
		if err := os.Remove(filepath.Join(dir, batchJournalName)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unsafe type", func(t *testing.T) {
		writeJournal(t, []Record{{ID: "ok", Type: RecordType("../escape"), Status: RecordStatusActive}})
		if _, err := NewFileStore(dir); err == nil {
			t.Fatal("a batch journal with an unsafe type must abort the open")
		}
		if err := os.Remove(filepath.Join(dir, batchJournalName)); err != nil {
			t.Fatal(err)
		}
	})

	// A journal with safe records still recovers.
	writeJournal(t, []Record{{ID: "journal-recovered", Type: RecordTypeMemory, Status: RecordStatusActive}})
	reopened := mustOpenStore(t, dir)
	if got, err := reopened.Get("journal-recovered"); err != nil || got == nil {
		t.Fatalf("safe batch journal record must recover: got=%v err=%v", got, err)
	}
	_ = reopened.Close()
}

func TestFileStoreCloseIsIdempotentAndBlocksOperations(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	fs := mustOpenStore(t, dir)
	if err := fs.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("second close must be a no-op: %v", err)
	}

	requireStoreCode(t, fs.Put(&Record{ID: "x", Type: RecordTypeMemory}), "STORE_CLOSED")
	if _, err := fs.Get("k"); err == nil {
		t.Fatal("Get after Close must fail")
	} else {
		requireStoreCode(t, err, "STORE_CLOSED")
	}
	if _, err := fs.List(Filter{}); err == nil {
		t.Fatal("List after Close must fail")
	}
	if _, err := fs.Count(Filter{}); err == nil {
		t.Fatal("Count after Close must fail")
	}
	if err := fs.Delete("k"); err == nil {
		t.Fatal("Delete after Close must fail")
	}
	if err := fs.DeleteBatch([]string{"k"}); err == nil {
		t.Fatal("DeleteBatch after Close must fail")
	}
	if err := fs.DeleteBatchHard([]string{"k"}); err == nil {
		t.Fatal("DeleteBatchHard after Close must fail")
	}
	if got := fs.CountAll(); got != 0 {
		t.Fatalf("CountAll after Close = %d, want 0", got)
	}
	if _, err := os.Stat(filepath.Join(dir, storageLockName)); err != nil {
		t.Fatalf("the lock marker must remain on disk after an orderly close: %v", err)
	}
}

func TestStoragePathValidation(t *testing.T) {
	requireStorageLockSupport(t)
	parent := t.TempDir()
	filePath := filepath.Join(parent, "occupied")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(parent, "dangling")
	if err := os.Symlink(filepath.Join(parent, "missing-target"), dangling); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"relative", "relative/store", "absolute"},
		{"root", string(filepath.Separator), "filesystem root"},
		{"sensitive", filepath.Join(string(filepath.Separator), "etc", "nexus-test"), "sensitive"},
		{"existing file", filePath, "not a directory"},
		{"missing parent", filepath.Join(parent, "missing", "store"), "parent"},
		{"dangling symlink", dangling, "resolve"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFileStore(tc.path)
			if err == nil {
				t.Fatalf("path %q must be rejected", tc.path)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error for %q must mention %q, got %v", tc.path, tc.want, err)
			}
		})
	}
}

func TestStoragePathResolvesConfiguredSymlink(t *testing.T) {
	requireStorageLockSupport(t)
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	fs := mustOpenStore(t, link)
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if fs.dir != canonicalTarget {
		t.Fatalf("store must operate on the resolved target %q, got %q", canonicalTarget, fs.dir)
	}
	if _, err := os.Stat(filepath.Join(canonicalTarget, storageLockName)); err != nil {
		t.Fatalf("lock must live at the resolved target: %v", err)
	}
	if err := fs.Put(&Record{ID: "via-link", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(canonicalTarget, string(RecordTypeMemory), "via-link.json")); err != nil {
		t.Fatalf("record must be written at the resolved target: %v", err)
	}
	_ = fs.Close()

	// A sensitive symlink target is rejected after resolution.
	sensitiveLink := filepath.Join(base, "etc-link")
	if err := os.Symlink("/etc", sensitiveLink); err == nil {
		if _, err := NewFileStore(sensitiveLink); err == nil || !strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("symlink to a sensitive root must be rejected, got %v", err)
		}
	}
}

func TestStoragePermissions(t *testing.T) {
	requireStorageLockSupport(t)
	parent := t.TempDir()

	// A newly created store root is owner-only.
	newDir := filepath.Join(parent, "new-store")
	fs := mustOpenStore(t, newDir)
	assertMode(t, newDir, 0o700)
	assertMode(t, filepath.Join(newDir, storageLockName), 0o600)

	if err := fs.Put(&Record{ID: "perm", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(newDir, string(RecordTypeMemory)), 0o700)
	assertMode(t, filepath.Join(newDir, string(RecordTypeMemory), "perm.json"), 0o600)

	// The batch journal is owner-only too.
	if err := fs.publishBatchJournal([]*Record{{ID: "perm", Type: RecordTypeMemory, Status: RecordStatusActive}}); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(newDir, batchJournalName), 0o600)
	if err := fs.removeBatchJournal(); err != nil {
		t.Fatal(err)
	}
	_ = fs.Close()

	// Existing directories are tightened without changing owner bits.
	existing := filepath.Join(parent, "existing-store")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o751); err != nil {
		t.Fatal(err)
	}
	typeDir := filepath.Join(existing, string(RecordTypeMemory))
	if err := os.Mkdir(typeDir, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(typeDir, 0o775); err != nil {
		t.Fatal(err)
	}
	fs2 := mustOpenStore(t, existing)
	assertMode(t, existing, 0o700)
	assertMode(t, typeDir, 0o700)
	_ = fs2.Close()

	// A pre-existing permissive lock file is tightened on acquisition.
	lockDir := filepath.Join(parent, "lock-store")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockDir, storageLockName)
	if err := os.WriteFile(lockPath, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o666); err != nil {
		t.Fatal(err)
	}
	fs3 := mustOpenStore(t, lockDir)
	assertMode(t, lockPath, 0o600)
	_ = fs3.Close()
}

func TestStorageDoesNotRewriteExistingRecordModes(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	fs := mustOpenStore(t, dir)
	if err := fs.Put(&Record{ID: "mode", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(dir, string(RecordTypeMemory), "mode.json")
	if err := os.Chmod(recordPath, 0o640); err != nil {
		t.Fatal(err)
	}
	_ = fs.Close()

	fs2 := mustOpenStore(t, dir)
	assertMode(t, recordPath, 0o640)
	if got, err := fs2.Get("mode"); err != nil || got == nil {
		t.Fatalf("record must remain readable: got=%v err=%v", got, err)
	}
	_ = fs2.Close()
}

func TestStorageRejectsSymlinkEscapeOnWrite(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	outside := t.TempDir()
	fs := mustOpenStore(t, dir)

	if err := os.Symlink(outside, filepath.Join(dir, "evil")); err != nil {
		t.Fatal(err)
	}
	err := fs.Put(&Record{ID: "x", Type: RecordType("evil"), Status: RecordStatusActive})
	if err == nil {
		t.Fatal("writing through a symlinked type directory must fail")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "x.json")); !os.IsNotExist(statErr) {
		t.Fatal("record escaped the owned storage root")
	}

	for _, rec := range []*Record{
		{ID: "../escape", Type: RecordTypeMemory, Status: RecordStatusActive},
		{ID: "ok", Type: RecordType("../escape"), Status: RecordStatusActive},
		{ID: "a/b", Type: RecordTypeMemory, Status: RecordStatusActive},
	} {
		requireStoreCode(t, fs.Put(rec), "INVALID_RECORD")
	}
	_ = fs.Close()
}

// A planted symlink on disk must abort hydration instead of being followed or
// silently skipped: either would make the reopened index diverge from the
// durable data the store claims to expose.
func TestStorageRejectsSymlinkEscapeOnLoad(t *testing.T) {
	requireStorageLockSupport(t)
	dir := t.TempDir()
	fs := mustOpenStore(t, dir)
	if err := fs.Put(&Record{ID: "seed", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	_ = fs.Close()

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"id":"outside","type":"memory","status":"active","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	recordLink := filepath.Join(dir, string(RecordTypeMemory), "planted.json")
	if err := os.Symlink(outside, recordLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("a symlinked record file must abort the open")
	}
	if err := os.Remove(recordLink); err != nil {
		t.Fatal(err)
	}

	typeLink := filepath.Join(dir, "planted-type")
	if err := os.Symlink(t.TempDir(), typeLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("a symlinked type directory must abort the open")
	}
	if err := os.Remove(typeLink); err != nil {
		t.Fatal(err)
	}

	reopened := mustOpenStore(t, dir)
	if got, err := reopened.Get("seed"); err != nil || got == nil {
		t.Fatalf("original record must load once the symlinks are removed: got=%v err=%v", got, err)
	}
	_ = reopened.Close()
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
