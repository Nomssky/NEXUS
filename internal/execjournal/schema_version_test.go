package execjournal

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// ---- v36: schema-version contract -----------------------------------------
//
// The audit found the contract ALREADY enforced: every read path decodes
// through unmarshalExecution/unmarshalInvocation, which check the version
// BEFORE interpreting any version-specific field; and every write path reads the
// existing record first and propagates that error, so an unsupported record can
// never be overwritten or downgraded.
//
// These tests close the real coverage gaps that audit exposed. They assert the
// policy through the journal interface, not private helpers.

const keyExec = "nexus.execution:"

// seedRawJSON writes a raw durable payload under the execution key, which is
// how a record written by another version would actually arrive.
func seedRawJSON(t *testing.T, st store.Store, id, data string) {
	t.Helper()
	if err := st.Put(&store.Record{ID: keyExec + id, Type: recordTypeExecution,
		Status: store.RecordStatusActive, BusinessID: "biz-1", Data: []byte(data)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func rawBytes(t *testing.T, st store.Store, id string) []byte {
	t.Helper()
	r, err := st.Get(keyExec + id)
	if err != nil || r == nil {
		t.Fatalf("raw read: %v", err)
	}
	return r.Data
}

// Newly written records carry the CURRENT schema version.
func TestNewRecordsCarryTheCurrentSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	j := New(st, fixedClock())
	if err := j.Accepted("v1", "biz-1", "div-1", "actor"); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := j.Get("v1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.Version != SchemaVersion {
		t.Fatalf("a new record must be written at the current schema version, got %d", rec.Version)
	}
	// And the persisted bytes really say so.
	if !bytes.Contains(rawBytes(t, st, "v1"), []byte(`"version":1`)) {
		t.Fatalf("persisted bytes must carry the schema version: %s", rawBytes(t, st, "v1"))
	}
	// And it survives a real reopen.
	rec2, _, err := reopenJournal(t, dir).Get("v1")
	if err != nil || rec2.Version != SchemaVersion {
		t.Fatalf("version must survive reopen: %+v err=%v", rec2, err)
	}
	// Terminal writes on an existing record preserve its version.
	if err := j.Terminal("v1", StateCompleted, ""); err != nil {
		t.Fatal(err)
	}
	if rec3, _, _ := j.Get("v1"); rec3.Version != SchemaVersion {
		t.Fatalf("a terminal write must not change the schema version, got %d", rec3.Version)
	}
	// An invocation record is versioned the same way.
	invID, err := j.RecordDispatchIntent("v1", "call-1", "http.request", "post", "mutation")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordOutcome(invID, InvCompleted, ""); err != nil {
		t.Fatal(err)
	}
	if inv, _, ok, err := j.getInvocation(invID); err != nil || !ok || inv.Version != SchemaVersion {
		t.Fatalf("invocation must be versioned at the current version: %+v ok=%v err=%v", inv, ok, err)
	}
}

// Every unsupported version fails closed on the single-record read — the path
// the API detail endpoint uses.
func TestUnsupportedVersionsFailClosedOnRead(t *testing.T) {
	for _, tc := range []struct{ name, payload string }{
		{"missing version", `{"id":"x","business_id":"biz-1","state":"completed"}`},
		{"zero version", `{"version":0,"id":"x","business_id":"biz-1","state":"completed"}`},
		{"negative version", `{"version":-1,"id":"x","business_id":"biz-1","state":"completed"}`},
		{"future version", `{"version":2,"id":"x","business_id":"biz-1","state":"completed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := mustStore(t, t.TempDir())
			seedRawJSON(t, st, "x", tc.payload)
			j := New(st, fixedClock())
			_, ok, err := j.Get("x")
			if err == nil {
				t.Fatal("an unsupported version must fail closed")
			}
			if ok {
				t.Fatal("no record may be returned alongside the error")
			}
			// Unsupported data is NOT a missing execution: the caller must be
			// able to tell "cannot read" from "does not exist".
			if _, absent, absentErr := j.Get("never-existed"); absent || absentErr != nil {
				t.Fatalf("control: a genuinely absent execution must read as absent, ok=%v err=%v", absent, absentErr)
			}
		})
	}
}

// A malformed record keeps its own classification, distinct from an
// unsupported version.
func TestMalformedAndUnsupportedStayDistinguishable(t *testing.T) {
	st := mustStore(t, t.TempDir())
	seedRawJSON(t, st, "corrupt", `{"version":1,"id":`)
	seedRawJSON(t, st, "future", `{"version":7,"id":"future","state":"completed"}`)
	j := New(st, fixedClock())

	_, _, corruptErr := j.Get("corrupt")
	_, _, futureErr := j.Get("future")
	if corruptErr == nil || futureErr == nil {
		t.Fatal("both must fail closed")
	}
	if !strings.Contains(corruptErr.Error(), "corrupt") {
		t.Errorf("a malformed record must be classified as corrupt, got %v", corruptErr)
	}
	if !strings.Contains(futureErr.Error(), "version") {
		t.Errorf("an unsupported version must be classified as a version problem, got %v", futureErr)
	}
	if corruptErr.Error() == futureErr.Error() {
		t.Error("the two classifications must be distinguishable")
	}
}

// NO write path may overwrite or downgrade an unsupported record, and every
// fail-closed path must leave the persisted bytes untouched.
func TestUnsupportedRecordsAreNeverOverwrittenOrDowngraded(t *testing.T) {
	for _, op := range []struct {
		name string
		run  func(j *Journal) error
	}{
		{"Accepted", func(j *Journal) error { return j.Accepted("x", "biz-1", "", "actor") }},
		{"Terminal", func(j *Journal) error { return j.Terminal("x", StateCompleted, "") }},
		{"RecordApproval", func(j *Journal) error {
			return j.RecordApproval("x", "ap-1", ApprovalRequested, "")
		}},
		{"SupersedeApprovedPause", func(j *Journal) error {
			return j.SupersedeApprovedPause("x", StateCompleted, "")
		}},
		{"Recover", func(j *Journal) error { _, err := j.Recover(); return err }},
	} {
		t.Run(op.name, func(t *testing.T) {
			st := mustStore(t, t.TempDir())
			// A record written by a FUTURE version: valid JSON, unknown version.
			seedRawJSON(t, st, "x", `{"version":9,"id":"x","business_id":"biz-1","state":"completed"}`)
			before := append([]byte(nil), rawBytes(t, st, "x")...)

			j := New(st, fixedClock())
			err := op.run(j)
			// Every one of these must fail, and for the VERSION reason: none may
			// silently succeed by treating the record as absent, and none may be
			// refused for some unrelated reason that would mask the version gap.
			if err == nil {
				t.Fatalf("%s must fail closed on an unsupported record, it succeeded", op.name)
			}
			if !strings.Contains(err.Error(), "unsupported execution record version") {
				t.Fatalf("%s must fail for the version reason, got %v", op.name, err)
			}
			after := rawBytes(t, st, "x")
			if !bytes.Equal(before, after) {
				t.Fatalf("%s must not rewrite or downgrade an unsupported record:\nbefore %s\nafter  %s",
					op.name, before, after)
			}
		})
	}
}

// Retention must refuse an unsupported record without removing anything.
func TestRetentionRefusesUnsupportedRecordsWithoutDeleting(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	j := New(st, fixedClock())
	if err := j.Accepted("keep", "biz-1", "", "actor"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("keep", StateCompleted, ""); err != nil {
		t.Fatal(err)
	}
	seedRawJSON(t, st, "future", `{"version":3,"id":"future","business_id":"biz-1","state":"completed"}`)
	before := append([]byte(nil), rawBytes(t, st, "future")...)

	// MinAge must be POSITIVE: a zero min age is refused by its own rule and
	// would mask the version guard entirely.
	rep, err := j.PlanRetention(RetentionPolicy{Enabled: true, MinAge: time.Hour})
	if err == nil {
		t.Fatalf("retention must refuse an unsupported record, report %+v", rep)
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("retention must refuse for the version reason, got %v", err)
	}
	if !bytes.Equal(before, rawBytes(t, st, "future")) {
		t.Fatal("a refused retention run must not modify the unsupported record")
	}
	if _, ok, _ := j.Get("keep"); !ok {
		t.Fatal("a refused retention run must not remove the eligible record either")
	}
}

// Within schema version 1 the v35 rule still holds: an unknown approval outcome
// fails closed, and a valid vocabulary still reads.
func TestApprovalVocabularyStaysClosedWithinVersionOne(t *testing.T) {
	st := mustStore(t, t.TempDir())
	seedRawExecution(t, st, approvalRecord("ok", entry("ap-1", ApprovalGranted)))
	seedRawExecution(t, st, approvalRecord("bad", entry("ap-1", "expired")))
	j := New(st, fixedClock())
	if _, ok, err := j.Get("ok"); err != nil || !ok {
		t.Fatalf("a valid version-1 history must read: ok=%v err=%v", ok, err)
	}
	if _, ok, err := j.Get("bad"); err == nil || ok {
		t.Fatalf("an unknown outcome at version 1 must still fail closed, got ok=%v err=%v", ok, err)
	}
}
