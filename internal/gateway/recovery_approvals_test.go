package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// ---- v34: durable approval history read boundary ---------------------------
//
// Approval history is a field ON the ExecutionRecord. The recovery detail read
// already proves the caller may see that record (business membership, exact
// business match, division scope), so exposing history reuses that boundary
// unchanged: it cannot widen who may see an execution, and history cannot be
// visible to a caller who cannot see the execution itself.

const (
	recApprID      = "rec-approvals"       // business-wide scope, in recBiz
	recApprOtherID = "rec-approvals-other" // belongs to a DIFFERENT business
	recApprDivID   = "rec-approvals-div"   // inside recDiv, so div-scoped reads work
)

// approvalView mirrors the exposed subset. decided_by is deliberately NOT here:
// the durable read contract excludes identity references, and the live approval
// surface gates approver identity behind BUSINESS-WIDE membership. See the
// report — that is an open decision, not something this surface settles.
type approvalView struct {
	ApprovalID string    `json:"approval_id"`
	Outcome    string    `json:"outcome"`
	DecidedAt  time.Time `json:"decided_at"`
}

type approvalEnvelope struct {
	Source      string         `json:"source"`
	ExecutionID string         `json:"execution_id"`
	BusinessID  string         `json:"business_id"`
	State       string         `json:"state"`
	Approvals   []approvalView `json:"approvals"`
	Raw         map[string]any `json:"-"`
}

// seedApprovalHistory writes a complete pause -> grant -> completion history.
func seedApprovalHistory(t *testing.T, j *execjournal.Journal, id, biz, div string) {
	t.Helper()
	if err := j.Accepted(id, biz, div, "user-wide"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	// The pause is a provisional terminal carrying APPROVAL_REQUIRED.
	if err := j.Terminal(id, execjournal.StateFailed, execjournal.ApprovalRequiredClass); err != nil {
		t.Fatalf("pause: %v", err)
	}
	for _, d := range []struct{ outcome, by string }{
		{execjournal.ApprovalRequested, ""},
		{execjournal.ApprovalGranted, "approver-9"},
	} {
		if err := j.RecordApproval(id, "ap-"+id, d.outcome, d.by); err != nil {
			t.Fatalf("record %s: %v", d.outcome, err)
		}
	}
	if err := j.SupersedeApprovedPause(id, execjournal.StateCompleted, ""); err != nil {
		t.Fatalf("supersede: %v", err)
	}
}

func decodeApprovals(t *testing.T, body []byte) approvalEnvelope {
	t.Helper()
	var env approvalEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

func detailPath(id string) string {
	return "/api/v1/recovery/executions/" + id + "?business_id=" + recBiz
}

// An authorized caller reads the history, in order, alongside the truthful
// terminal state the pause was superseded to.
func TestApprovalHistoryIsReadableByAnAuthorizedCaller(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	seedApprovalHistory(t, j, recApprID, recBiz, "")

	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("authorized read must succeed, got %d: %s", w.Code, w.Body)
	}
	env := decodeApprovals(t, w.Body.Bytes())
	if len(env.Approvals) != 2 {
		t.Fatalf("expected 2 recorded decisions, got %d: %+v", len(env.Approvals), env.Approvals)
	}
	if env.Approvals[0].Outcome != execjournal.ApprovalRequested {
		t.Errorf("history must start with the request, got %q", env.Approvals[0].Outcome)
	}
	if env.Approvals[1].Outcome != execjournal.ApprovalGranted {
		t.Errorf("history must record the grant second, got %q", env.Approvals[1].Outcome)
	}
	if env.Approvals[1].ApprovalID != "ap-"+recApprID {
		t.Errorf("each decision must keep its approval id, got %q", env.Approvals[1].ApprovalID)
	}
	if env.Approvals[1].DecidedAt.IsZero() {
		t.Error("each decision must carry its decision time")
	}
	// The supersession is visible: the execution's own state is the real one.
	if env.State != string(execjournal.StateCompleted) {
		t.Errorf("history must not shadow the terminal state, got %q", env.State)
	}
}

// Approver identity is not exposed on this surface.
func TestApprovalHistoryDoesNotExposeApproverIdentity(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")
	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d", w.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	arr, ok := raw["approvals"].([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("approvals missing: %s", w.Body)
	}
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry %d not an object", i)
		}
		for _, forbidden := range []string{"decided_by", "actor_id", "approver_id", "requester_id", "reason"} {
			if _, present := m[forbidden]; present {
				t.Errorf("approval history must not expose %q: %v", forbidden, m)
			}
		}
	}
}

// An execution with no approvals keeps its exact previous response shape.
func TestEmptyApprovalHistoryIsOmitted(t *testing.T) {
	f := newRecoveryFixture(t)
	w := f.get(t, detailPath(recWideID), wideHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d", w.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["approvals"]; present {
		t.Errorf("an execution with no history must omit the field entirely: %s", w.Body)
	}
	// The established fields are untouched.
	for _, k := range []string{"source", "execution_id", "business_id", "state", "created_at", "updated_at"} {
		if _, present := raw[k]; !present {
			t.Errorf("existing field %q must remain", k)
		}
	}
}

// No credential: nothing is disclosed.
func TestApprovalHistoryRequiresAuthentication(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")
	w := f.get(t, detailPath(recApprID), nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated read must be 401, got %d", w.Code)
	}
	if body := w.Body.String(); containsAny(body, "approvals", "requested", "granted", "approver-9") {
		t.Fatalf("an unauthorized response must not disclose history: %s", body)
	}
}

// Cross-business: another business' execution is not visible, and the failure
// must not confirm that it exists.
func TestApprovalHistoryIsNotCrossBusiness(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprOtherID, recOtherBiz, "")

	// Ask for it under OUR business id.
	w := f.get(t, "/api/v1/recovery/executions/"+recApprOtherID+"?business_id="+recBiz, wideHeaders())
	if w.Code != http.StatusNotFound {
		t.Fatalf("a cross-business read must be 404, got %d: %s", w.Code, w.Body)
	}
	if body := w.Body.String(); containsAny(body, "approvals", "requested", "granted", "approver-9", recOtherBiz) {
		t.Fatalf("a cross-business 404 must not disclose history: %s", body)
	}
}

// Cross-division: a division-scoped member may not read an execution outside
// its division, even inside the same business.
func TestApprovalHistoryIsNotCrossDivision(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")

	w := f.get(t, detailPath(recApprID), divHeaders())
	if w.Code != http.StatusNotFound {
		t.Fatalf("a division-scoped caller must not read outside its division, got %d: %s", w.Code, w.Body)
	}
	if body := w.Body.String(); containsAny(body, "approvals", "requested", "granted", "approver-9") {
		t.Fatalf("a cross-division 404 must not disclose history: %s", body)
	}
}

// The same division-scoped caller CAN read its own division's execution: the
// boundary narrows, it does not exclude division members outright.
func TestApprovalHistoryIsReadableWithinOwnDivision(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprDivID, recBiz, recDiv)
	w := f.get(t, detailPath(recApprDivID), divHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("a division member must read its own division, got %d: %s", w.Code, w.Body)
	}
}

// Unknown execution keeps the established not-found behavior.
func TestApprovalHistoryUnknownExecutionStaysNotFound(t *testing.T) {
	f := newRecoveryFixture(t)
	w := f.get(t, detailPath(recGoneID), wideHeaders())
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown execution must stay 404, got %d", w.Code)
	}
}

// History survives a restart: a brand-new store and journal over the same
// directory still projects it.
func TestApprovalHistorySurvivesRestart(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")

	closeStore(t, f.store)
	st, err := store.NewFileStore(f.storeDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec, ok, err := j2.Get(recApprID)
	if err != nil || !ok {
		t.Fatalf("record must survive restart: ok=%v err=%v", ok, err)
	}
	if len(rec.Approvals) != 2 {
		t.Fatalf("history must survive restart, got %d entries", len(rec.Approvals))
	}
	if rec.State != execjournal.StateCompleted {
		t.Fatalf("terminal state must survive restart, got %q", rec.State)
	}
}

// A corrupt record must not be presented as an empty history: the read fails
// closed instead.
func TestCorruptJournalDataIsNotPresentedAsEmptyHistory(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")

	// Corrupt the durable payload through the SAME store the engine's journal
	// reads through, so the read path really sees the corrupt record.
	env, err := f.store.Get("nexus.execution:" + recApprID)
	if err != nil || env == nil {
		t.Fatalf("setup: %v", err)
	}
	env.Data = []byte("{ not json")
	if err := f.store.Put(env); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("corrupt durable data must fail closed, got %d: %s", w.Code, w.Body)
	}
	if body := w.Body.String(); containsAny(body, "\"approvals\"", "requested", "granted") {
		t.Fatalf("corrupt data must not be rendered as history: %s", body)
	}
}

func containsAny(body string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(body, n) {
			return true
		}
	}
	return false
}

// ---- v35: integrity at the read boundary -----------------------------------
//
// An invalid approval entry must make the WHOLE durable record unreadable, so
// the API can never answer with a partial history that looks complete.

func seedRawApprovals(t *testing.T, f *recoveryFixture, id string, data []byte) {
	t.Helper()
	env, err := f.store.Get("nexus.execution:" + id)
	if err != nil || env == nil {
		t.Fatalf("setup: %v", err)
	}
	env.Data = data
	if err := f.store.Put(env); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// A record whose history contains an out-of-vocabulary outcome fails closed at
// the API, and the valid entries beside it are NOT returned.
func TestAPIRejectsInvalidApprovalOutcomeWithoutPartialHistory(t *testing.T) {
	f := newRecoveryFixture(t)
	seedApprovalHistory(t, f.engine.ExecutionJournal(), recApprID, recBiz, "")
	// Rewrite the payload: a valid pair, an invalid outcome, then a valid entry.
	seedRawApprovals(t, f, recApprID, []byte(`{
	  "version":1,"id":"`+recApprID+`","business_id":"`+recBiz+`","state":"completed",
	  "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",
	  "approvals":[
	    {"approval_id":"ap-x","outcome":"requested","decided_at":"2026-01-01T00:00:00Z"},
	    {"approval_id":"ap-x","outcome":"maybe-approved","decided_at":"2026-01-01T00:00:00Z"},
	    {"approval_id":"ap-x","outcome":"granted","decided_at":"2026-01-01T00:00:00Z"}]}`))

	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("an invalid approval outcome must fail closed, got %d: %s", w.Code, w.Body)
	}
	if body := w.Body.String(); containsAny(body, "requested", "granted", "maybe-approved") {
		t.Fatalf("a partial history must never be presented: %s", body)
	}
}

// An execution whose approval gate degraded without an approval id stays
// readable: empty is a legitimate recorded value, not corruption.
func TestAPIAcceptsApprovalHistoryWithEmptyApprovalID(t *testing.T) {
	f := newRecoveryFixture(t)
	if err := f.engine.ExecutionJournal().Accepted(recApprID, recBiz, "", "user-wide"); err != nil {
		t.Fatal(err)
	}
	seedRawApprovals(t, f, recApprID, []byte(`{
	  "version":1,"id":"`+recApprID+`","business_id":"`+recBiz+`","state":"completed",
	  "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",
	  "approvals":[{"approval_id":"","outcome":"requested","decided_at":"2026-01-01T00:00:00Z"}]}`))

	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("an empty approval_id must remain readable, got %d: %s", w.Code, w.Body)
	}
	env := decodeApprovals(t, w.Body.Bytes())
	if len(env.Approvals) != 1 || env.Approvals[0].Outcome != "requested" {
		t.Fatalf("the degraded entry must be projected: %+v", env.Approvals)
	}
	if env.Approvals[0].ApprovalID != "" {
		t.Fatalf("an empty approval_id must stay empty, got %q", env.Approvals[0].ApprovalID)
	}
}

// Multiple approval gates on one execution each keep their own entry: history
// grows with gates and must never collapse or duplicate them.
func TestRepeatedApprovalGatesKeepDistinctEntries(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	if err := j.Accepted(recApprID, recBiz, "", "user-wide"); err != nil {
		t.Fatal(err)
	}
	// The pause writes a terminal, so it is recorded ONCE. A later gate hits a
	// record that is already terminal `failed`, so its own pause write is a
	// no-op and only its approval facts are appended. The one supersede happens
	// when the execution finally terminates.
	if err := j.Terminal(recApprID, execjournal.StateFailed, execjournal.ApprovalRequiredClass); err != nil {
		t.Fatalf("pause: %v", err)
	}
	for i, gate := range []string{"ap-1", "ap-2"} {
		if err := j.RecordApproval(recApprID, gate, execjournal.ApprovalRequested, ""); err != nil {
			t.Fatalf("gate %d requested: %v", i, err)
		}
		if err := j.RecordApproval(recApprID, gate, execjournal.ApprovalGranted, "approver"); err != nil {
			t.Fatalf("gate %d granted: %v", i, err)
		}
	}
	if err := j.SupersedeApprovedPause(recApprID, execjournal.StateCompleted, ""); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	w := f.get(t, detailPath(recApprID), wideHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	env := decodeApprovals(t, w.Body.Bytes())
	if len(env.Approvals) != 4 {
		t.Fatalf("two gates must yield four distinct entries, got %d: %+v", len(env.Approvals), env.Approvals)
	}
	want := []struct{ id, outcome string }{
		{"ap-1", execjournal.ApprovalRequested}, {"ap-1", execjournal.ApprovalGranted},
		{"ap-2", execjournal.ApprovalRequested}, {"ap-2", execjournal.ApprovalGranted},
	}
	for i, wv := range want {
		if env.Approvals[i].ApprovalID != wv.id || env.Approvals[i].Outcome != wv.outcome {
			t.Errorf("entry %d must be %s/%s, got %s/%s", i, wv.id, wv.outcome,
				env.Approvals[i].ApprovalID, env.Approvals[i].Outcome)
		}
	}
}
