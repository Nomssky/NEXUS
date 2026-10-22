// Package execjournal is the durable execution journal (v10,
// contracts/DURABLE_EXECUTION_RECOVERY_CONTRACTS.md).
//
// It records the minimum durable lifecycle evidence for an execution and the
// dispatch-intent/outcome evidence for consequential tool invocations, using the
// existing durable FileStore — one independently atomic record write per
// transition. On startup it classifies interrupted executions conservatively
// (never replaying an ambiguous effect).
//
// It stores only identifiers, scope, coarse classes and timestamps. It never
// stores prompts, model output, tool inputs, credentials, secrets or
// chain-of-thought.
package execjournal

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// SchemaVersion is the durable record version. A record with a different
// version is rejected (fail closed), never guessed at.
const SchemaVersion = 1

// MaxListScan bounds how many durable execution records a list read considers
// for one business. It keeps the read model bounded and deterministic.
const MaxListScan = 5000

// Record types in the store. They are plain store.RecordType values so the
// journal rides the existing FileStore layout (<type>/<id>.json).
const (
	recordTypeExecution  store.RecordType = "execution"
	recordTypeInvocation store.RecordType = "invocation"
)

// State is the durable lifecycle state.
type State string

const (
	StateAccepted  State = "accepted"
	StateRunning   State = "running"
	StatePending   State = "pending_approval"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StateTimedOut  State = "timed_out"
	// StateUnknown is assigned ONLY by recovery to a nonterminal record whose
	// in-flight effect may have occurred. It is never assigned by the runtime.
	StateUnknown State = "unknown"
)

// validStates is the closed vocabulary; anything else is a corrupt record.
var validStates = map[State]struct{}{
	StateAccepted: {}, StateRunning: {}, StatePending: {},
	StateCompleted: {}, StateFailed: {}, StateCancelled: {},
	StateTimedOut: {}, StateUnknown: {},
}

// Terminal reports whether a state is terminal (never re-runnable).
func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateTimedOut:
		return true
	}
	return false
}

// Invocation statuses.
const (
	InvDispatched = "dispatched" // intent recorded, outcome unknown
	InvCompleted  = "completed"
	InvFailed     = "failed"
	InvUnknown    = "unknown"
)

// RecoveryMeta records that a state was set by the recovery pass.
type RecoveryMeta struct {
	Recovered   bool      `json:"recovered"`
	PriorState  State     `json:"prior_state,omitempty"`
	RecoveredAt time.Time `json:"recovered_at"`
	Reason      string    `json:"reason,omitempty"`
}

// Approval outcomes recorded in an execution's durable approval history. They
// are facts ABOUT the approval, kept separate from the execution's own terminal
// outcome so the two can never be conflated.
const (
	ApprovalRequested = "requested" // the execution parked on an approval gate
	ApprovalGranted   = "granted"   // an approver authorized the gated action
	ApprovalDenied    = "denied"    // an approver refused it
)

// Coarse error classes the journal itself recognises. They are stored as the
// record's error_class so the durable verdict stays machine-readable; they are
// never a raw message.
const (
	// ApprovalRequiredClass marks the provisional pause written when an
	// execution parks on an approval gate.
	ApprovalRequiredClass = "APPROVAL_REQUIRED"
	// ApprovalDeniedClass marks the terminal verdict of a denied approval.
	ApprovalDeniedClass = "POLICY_DENIED"
)

// ErrNotFound reports that an operation named a durable record that does not
// exist. Callers match with errors.Is.
var ErrNotFound = errors.New("execjournal: execution not found")

// validApprovalOutcomes is the closed vocabulary for durable approval history.
var validApprovalOutcomes = map[string]struct{}{
	ApprovalRequested: {}, ApprovalGranted: {}, ApprovalDenied: {},
}

// ApprovalDecision is one durable, ordered approval fact for an execution. It
// is append-only and independent of the execution's terminal state, so the
// history survives the terminal outcome and is readable after it.
type ApprovalDecision struct {
	ApprovalID string    `json:"approval_id"`
	Outcome    string    `json:"outcome"`
	DecidedBy  string    `json:"decided_by,omitempty"`
	DecidedAt  time.Time `json:"decided_at"`
}

// ExecutionRecord is the durable per-execution evidence.
type ExecutionRecord struct {
	Version    int           `json:"version"`
	ID         string        `json:"id"`
	BusinessID string        `json:"business_id"`
	DivisionID string        `json:"division_id,omitempty"`
	ActorID    string        `json:"actor_id,omitempty"`
	State      State         `json:"state"`
	ErrorClass string        `json:"error_class,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Recovery   *RecoveryMeta `json:"recovery,omitempty"`
	// Approvals is the durable approval history, appended in decision order. It
	// is additive and optional: a record written before this field existed
	// decodes with no history, and nothing is ever fabricated to fill it.
	Approvals []ApprovalDecision `json:"approvals,omitempty"`
	// CompletedAt is the real terminal transition time, set exactly once on the
	// ordinary terminal path and replaced only by the narrow approval-pause
	// supersede. It is a pointer so an older record (or an interrupted record
	// that has no terminal event) reports "unavailable" instead of a fabricated
	// zero timestamp.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// ResearchEvidence is the optional, validated web.search source snapshot.
	// It is written atomically with the terminal transition. Presence means the
	// snapshot was complete when captured; absence never implies the search
	// returned nothing.
	ResearchEvidence *ResearchEvidence `json:"research_evidence,omitempty"`
}

// InvocationRecord is the durable dispatch-intent/outcome evidence for one
// logical consequential tool call.
type InvocationRecord struct {
	Version         int       `json:"version"`
	ID              string    `json:"id"`
	ExecutionID     string    `json:"execution_id"`
	CallID          string    `json:"call_id"`
	ToolID          string    `json:"tool_id"`
	Operation       string    `json:"operation,omitempty"`
	SideEffectClass string    `json:"side_effect_class,omitempty"`
	Status          string    `json:"status"`
	ErrorClass      string    `json:"error_class,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Report summarizes a recovery pass (bounded and deterministic).
type Report struct {
	Scanned            int
	Terminal           int
	Recovered          int
	UnknownInvocations int
}

// Journal is the durable execution journal over a store.Store. A nil *Journal
// is a safe no-op (dev/in-memory installs).
type Journal struct {
	st  store.Store
	now func() time.Time
	mu  sync.Mutex
	seq int64
}

// New builds a journal over the given store. A nil store yields a no-op
// journal (nil receiver methods guard).
func New(st store.Store, now func() time.Time) *Journal {
	if st == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	return &Journal{st: st, now: now}
}

// ErrAlreadyRecorded reports an execution id that already carries settled
// durable evidence: a terminal state, or recovery's unknown verdict. Callers
// match with errors.Is.
var ErrAlreadyRecorded = errors.New("execjournal: execution already recorded")

// Accepted durably records a newly accepted execution. It MUST succeed before
// the caller reports durable acceptance.
//
// Terminal is final in BOTH directions, mirroring Terminal(). A record that is
// already terminal, or that recovery classified unknown, is settled evidence
// and is never re-armed: rewriting it as accepted would erase the only durable
// record distinguishing completed from failed from unknown, and would let an
// execution that already finished — or whose side effect may already have
// occurred — be admitted and run a second time. This is the same guarantee
// Terminal() provides, and it is what EXECUTION_JOURNAL_RETENTION_CONTRACTS
// §"Journal disjointness" relies on when it states that a concurrent write to
// an eligible terminal record cannot occur.
//
// The guard is durable, not process-local, on purpose: core's duplicate
// detection reads the in-memory results and inflight maps, which a restart
// empties, and the execution id originates from the caller's X-Correlation-ID.
// Without this check the durable record would be re-armed across the boundary.
func (j *Journal) Accepted(id, businessID, divisionID, actorID string) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	existing, version, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	if ok && (existing.State.Terminal() || existing.State == StateUnknown) {
		return fmt.Errorf("%w: %s is %s", ErrAlreadyRecorded, id, existing.State)
	}
	now := j.now().UTC()
	rec := ExecutionRecord{
		Version: SchemaVersion, ID: id, BusinessID: businessID, DivisionID: divisionID,
		ActorID: actorID, State: StateAccepted, CreatedAt: now, UpdatedAt: now,
	}
	return j.putExecution(rec, version)
}

// Terminal durably records a terminal lifecycle state. `errClass` is a coarse
// classification only (e.g. an error code), never a raw message.
func (j *Journal) Terminal(id string, state State, errClass string) error {
	return j.TerminalWithEvidence(id, state, errClass, nil)
}

// TerminalWithEvidence is Terminal with an optional, validated research source
// snapshot written in the SAME atomic record update. Callers must not write
// evidence in a separate later step: if the terminal write fails, the record
// remains nonterminal and no evidence is exposed, so the process-local result
// can never claim durable evidence that was not stored.
func (j *Journal) TerminalWithEvidence(id string, state State, errClass string, evidence *ResearchEvidence) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	if !state.Terminal() {
		return fmt.Errorf("execjournal: %q is not a terminal state", state)
	}
	if err := ValidateResearchEvidenceForExecution(id, evidence); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	existing, version, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	now := j.now().UTC()
	if !ok {
		// A terminal transition without a prior accepted record still records
		// the fact; the id is authoritative.
		existing = ExecutionRecord{Version: SchemaVersion, ID: id, CreatedAt: now}
		version = 0
	}
	if existing.State.Terminal() {
		return nil // already terminal; terminal is final
	}
	existing.State = state
	existing.ErrorClass = errClass // coarse class only; never a raw message
	existing.CompletedAt = &now
	existing.UpdatedAt = now
	if evidence != nil {
		existing.ResearchEvidence = evidence
	}
	return j.putExecution(existing, version)
}

// ErrNotApprovalPause reports that an execution carries no provisional approval
// pause to supersede. It is a normal, expected outcome on the ordinary terminal
// path, not a failure: callers fall back to Terminal.
var ErrNotApprovalPause = errors.New("execjournal: execution carries no approval pause")

// RecordApproval durably appends one approval fact to an execution's history.
// History is append-only and survives every later state change, including the
// final terminal outcome, so an approval remains auditable after the execution
// that requested it has finished. It is associated with the execution by id and
// ordered by decision time.
func (j *Journal) RecordApproval(id, approvalID, outcome, decidedBy string) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	if _, ok := validApprovalOutcomes[outcome]; !ok {
		return fmt.Errorf("execjournal: invalid approval outcome %q", outcome)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	existing, version, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	if !ok {
		// Approval history is evidence about a known execution. Never invent a
		// record for an execution that was never durably accepted.
		return fmt.Errorf("%w: %s has no durable record", ErrNotFound, id)
	}
	existing.Approvals = append(existing.Approvals, ApprovalDecision{
		ApprovalID: approvalID, Outcome: outcome, DecidedBy: decidedBy, DecidedAt: j.now().UTC(),
	})
	existing.UpdatedAt = j.now().UTC()
	return j.putExecution(existing, version)
}

// SupersedeApprovedPause replaces a provisional approval pause with the real
// terminal outcome of the resumed execution. It is the one, and only, way a
// terminal execution record ever changes, and it is deliberately narrow:
//
//   - the record must be exactly failed + APPROVAL_REQUIRED (the pause the
//     engine writes when it parks on an approval gate);
//   - it must carry no recovery metadata, so a record recovery has already
//     classified can never be resurrected;
//   - it moves only to another terminal state, never to a nonterminal or
//     unknown one.
//
// Terminal() keeps first-wins semantics and is otherwise untouched, so every
// ordinary terminal write is still final. ErrNotApprovalPause is returned when
// there is simply no pause to supersede; the caller then uses Terminal().
func (j *Journal) SupersedeApprovedPause(id string, state State, errClass string) error {
	return j.SupersedeApprovedPauseWithEvidence(id, state, errClass, nil)
}

// SupersedeApprovedPauseWithEvidence is SupersedeApprovedPause with an
// optional final research source snapshot. A nil snapshot preserves the
// provisional pause's evidence (it is still evidence of a real search that
// occurred before the pause). A non-nil snapshot replaces it with the resumed
// execution's complete capture. The final terminal time replaces the pause
// time because the execution has now reached its real outcome.
func (j *Journal) SupersedeApprovedPauseWithEvidence(id string, state State, errClass string, evidence *ResearchEvidence) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	if !state.Terminal() {
		return fmt.Errorf("execjournal: %q is not a terminal state", state)
	}
	if err := ValidateResearchEvidenceForExecution(id, evidence); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	existing, version, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	if !ok || existing.State != StateFailed || existing.ErrorClass != ApprovalRequiredClass ||
		(existing.Recovery != nil && existing.Recovery.Recovered) {
		return fmt.Errorf("%w: %s", ErrNotApprovalPause, id)
	}
	now := j.now().UTC()
	existing.State = state
	existing.ErrorClass = errClass // coarse class only; never a raw message
	existing.CompletedAt = &now
	existing.UpdatedAt = now
	if evidence != nil {
		existing.ResearchEvidence = evidence
	}
	return j.putExecution(existing, version)
}

// RecordDispatchIntent durably records that a consequential invocation is about
// to be dispatched, BEFORE the effect can occur. It returns the invocation
// record id used to later record the outcome.
func (j *Journal) RecordDispatchIntent(executionID, callID, toolID, operation, sideEffectClass string) (string, error) {
	if j == nil {
		return "", nil
	}
	if executionID == "" {
		return "", fmt.Errorf("execjournal: execution id required")
	}
	j.mu.Lock()
	j.seq++
	id := fmt.Sprintf("inv-%s-%d", executionID, j.seq)
	j.mu.Unlock()

	now := j.now().UTC()
	rec := InvocationRecord{
		Version: SchemaVersion, ID: id, ExecutionID: executionID, CallID: callID,
		ToolID: toolID, Operation: operation, SideEffectClass: sideEffectClass,
		Status: InvDispatched, CreatedAt: now, UpdatedAt: now,
	}
	if err := j.putInvocation(rec, 0); err != nil {
		return "", err
	}
	return id, nil
}

// RecordOutcome durably records the terminal status of a dispatched invocation.
// status is one of InvCompleted / InvFailed / InvUnknown.
func (j *Journal) RecordOutcome(invocationID, status, errorClass string) error {
	if j == nil || invocationID == "" {
		return nil
	}
	switch status {
	case InvCompleted, InvFailed, InvUnknown:
	default:
		return fmt.Errorf("execjournal: invalid invocation status %q", status)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	existing, version, ok, err := j.getInvocation(invocationID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // intent record gone; nothing to update
	}
	existing.Status = status
	existing.ErrorClass = errorClass
	existing.UpdatedAt = j.now().UTC()
	return j.putInvocation(existing, version)
}

// Get returns a durable execution record (for operators/tests).
func (j *Journal) Get(id string) (ExecutionRecord, bool, error) {
	if j == nil {
		return ExecutionRecord{}, false, nil
	}
	er, _, ok, err := j.getExecution(id)
	return er, ok, err
}

// ListByBusiness returns up to maxRecords durable execution records for
// businessID, newest first (by created_at, tie-broken by execution id). A
// maxRecords < 1, or one above MaxListScan, is clamped to MaxListScan. Every
// record is validated and the call fails closed on malformed or unsupported
// data. It is a bounded read model: at most MaxListScan records are considered.
func (j *Journal) ListByBusiness(businessID string, maxRecords int) ([]ExecutionRecord, error) {
	if j == nil {
		return nil, nil
	}
	if businessID == "" {
		return nil, fmt.Errorf("execjournal: business id required")
	}
	if maxRecords < 1 || maxRecords > MaxListScan {
		maxRecords = MaxListScan
	}
	recs, err := j.st.List(store.Filter{Type: recordTypeExecution, BusinessID: businessID})
	if err != nil {
		return nil, fmt.Errorf("execjournal: list executions: %w", err)
	}
	out := make([]ExecutionRecord, 0, len(recs))
	for _, r := range recs {
		er, err := unmarshalExecution(r)
		if err != nil {
			return nil, err
		}
		out = append(out, er)
	}
	sort.Slice(out, func(i, k int) bool {
		if !out[i].CreatedAt.Equal(out[k].CreatedAt) {
			return out[i].CreatedAt.After(out[k].CreatedAt) // newest first
		}
		return out[i].ID < out[k].ID
	})
	if len(out) > maxRecords {
		out = out[:maxRecords]
	}
	return out, nil
}

// Recover loads and validates durable records and classifies interrupted
// executions. It is deterministic and bounded. It fails closed (returns an
// error) on a corrupt, unsupported or contradictory record; the caller must
// abort startup rather than serve with partial state.
func (j *Journal) Recover() (Report, error) {
	var rep Report
	if j == nil {
		return rep, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	recs, err := j.st.List(store.Filter{Type: recordTypeExecution})
	if err != nil {
		return rep, fmt.Errorf("execjournal: list executions: %w", err)
	}
	nonterminal := map[string]bool{}
	for _, r := range recs {
		er, err := unmarshalExecution(r)
		if err != nil {
			return rep, err
		}
		rep.Scanned++
		if er.State.Terminal() {
			rep.Terminal++
			continue
		}
		// Nonterminal at startup: classify conservatively as unknown. Never
		// fabricate a result and never mark it runnable.
		if er.State == StateUnknown {
			nonterminal[er.ID] = true
			continue // already classified by a prior recovery
		}
		prior := er.State
		er.State = StateUnknown
		er.Recovery = &RecoveryMeta{Recovered: true, PriorState: prior, RecoveredAt: j.now().UTC(),
			Reason: "interrupted before a durable terminal state; outcome may be unknown"}
		er.UpdatedAt = j.now().UTC()
		if err := j.putExecution(er, r.Version); err != nil {
			return rep, fmt.Errorf("execjournal: classify %s: %w", er.ID, err)
		}
		nonterminal[er.ID] = true
		rep.Recovered++
	}

	invs, err := j.st.List(store.Filter{Type: recordTypeInvocation})
	if err != nil {
		return rep, fmt.Errorf("execjournal: list invocations: %w", err)
	}
	for _, r := range invs {
		iv, err := unmarshalInvocation(r)
		if err != nil {
			return rep, err
		}
		if iv.Status != InvDispatched {
			continue
		}
		if !nonterminal[iv.ExecutionID] {
			continue
		}
		iv.Status = InvUnknown
		iv.ErrorClass = "interrupted"
		iv.UpdatedAt = j.now().UTC()
		if err := j.putInvocation(iv, r.Version); err != nil {
			return rep, fmt.Errorf("execjournal: classify invocation %s: %w", iv.ID, err)
		}
		rep.UnknownInvocations++
	}
	return rep, nil
}

// --- persistence helpers ---
//
// Store keys are namespaced: the FileStore index is keyed by record ID alone,
// so a journal record must never share an ID with an org/agent record. The
// logical execution id is carried in the record's `id` field.
func execKey(id string) string { return "nexus.execution:" + id }
func invKey(id string) string  { return "nexus.invocation:" + id }

func (j *Journal) putExecution(rec ExecutionRecord, version int) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("execjournal: marshal execution: %w", err)
	}
	sr := &store.Record{
		ID: execKey(rec.ID), Type: recordTypeExecution, Status: store.RecordStatusActive,
		BusinessID: rec.BusinessID, DivisionID: rec.DivisionID, Data: data,
	}
	// Carry the version from the getExecution read forward. Do NOT re-read the
	// store version here: re-reading would launder a stale snapshot's version
	// and let a writer overwrite a newer record it never saw. The store's
	// optimistic concurrency check rejects any version mismatch as
	// VERSION_CONFLICT, which callers can handle as an explicit conflict.
	sr.Version = version
	return j.st.Put(sr)
}

func (j *Journal) putInvocation(rec InvocationRecord, version int) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("execjournal: marshal invocation: %w", err)
	}
	sr := &store.Record{
		ID: invKey(rec.ID), Type: recordTypeInvocation, Status: store.RecordStatusActive,
		BusinessID: "",
		Data:       data,
	}
	sr.Version = version
	return j.st.Put(sr)
}

// getExecution reads one durable execution record, returning the store record's
// current version so callers can carry it forward into a subsequent Put instead
// of re-reading it (which would defeat optimistic concurrency by laundering a
// stale snapshot's version).
//
// A backend signals absence in one of two ways: FileStore follows the Store
// contract ("returns nil if not found") and yields (nil, nil), while MemStore
// yields (nil, store.ErrNotFound). Both mean exactly the same thing — there is
// no record — and neither is a failure, so absence is normalized to ok=false
// with a nil error here. Normalizing at this single internal boundary keeps
// every journal operation backend-independent without changing either store's
// public semantics, and it keeps the ok flag meaningful. A genuine I/O or
// corruption failure still propagates and is never mistaken for absence.
func (j *Journal) getExecution(id string) (ExecutionRecord, int, bool, error) {
	r, err := j.st.Get(execKey(id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ExecutionRecord{}, 0, false, nil
		}
		return ExecutionRecord{}, 0, false, err
	}
	if r == nil || r.Type != recordTypeExecution {
		return ExecutionRecord{}, 0, false, nil
	}
	er, err := unmarshalExecution(r)
	if err != nil {
		return ExecutionRecord{}, 0, false, err
	}
	return er, r.Version, true, nil
}

// getInvocation reads one durable invocation record, with the same absence
// normalization and version return as getExecution.
func (j *Journal) getInvocation(id string) (InvocationRecord, int, bool, error) {
	r, err := j.st.Get(invKey(id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return InvocationRecord{}, 0, false, nil
		}
		return InvocationRecord{}, 0, false, err
	}
	if r == nil || r.Type != recordTypeInvocation {
		return InvocationRecord{}, 0, false, nil
	}
	iv, err := unmarshalInvocation(r)
	if err != nil {
		return InvocationRecord{}, 0, false, err
	}
	return iv, r.Version, true, nil
}

// validateApprovals checks a durable approval history at decode time.
//
// Only the OUTCOME is validated: it is the one field with an established closed
// vocabulary, already enforced on every write by RecordApproval. Validating it
// on read therefore makes a reader agree with what the write path guarantees,
// and mirrors exactly how unmarshalExecution validates the execution STATE.
//
// Deliberately NOT validated:
//
//   - ApprovalID — may legitimately be empty. When createApproval fails the
//     engine surfaces APPROVAL_REQUIRED without an approval_id (graceful
//     degradation), so a real pause entry can carry no id.
//   - DecidedBy — omitempty, and a pause entry is recorded with no decider.
//
// An unknown outcome fails closed for the same reason an unknown state does:
// SchemaVersion is the documented forward-compatibility mechanism, so a version
// that extends this vocabulary must bump it, and a record of another version
// already fails closed. Within one version the vocabulary is closed, and
// returning the surrounding valid entries as though the record were intact
// would be a silently partial history.
func validateApprovals(rec ExecutionRecord) error {
	for i, a := range rec.Approvals {
		if _, ok := validApprovalOutcomes[a.Outcome]; !ok {
			return fmt.Errorf("execjournal: execution record %s has invalid approval outcome %q at index %d",
				rec.ID, a.Outcome, i)
		}
	}
	return nil
}

func unmarshalExecution(r *store.Record) (ExecutionRecord, error) {
	var er ExecutionRecord
	if err := json.Unmarshal(r.Data, &er); err != nil {
		return er, fmt.Errorf("execjournal: corrupt execution record %s: %w", r.ID, err)
	}
	if er.Version != SchemaVersion {
		return er, fmt.Errorf("execjournal: unsupported execution record version %d for %s", er.Version, r.ID)
	}
	if er.ID == "" {
		return er, fmt.Errorf("execjournal: execution record %s has no id", r.ID)
	}
	if _, ok := validStates[er.State]; !ok {
		return er, fmt.Errorf("execjournal: execution record %s has invalid state %q", er.ID, er.State)
	}
	if err := validateApprovals(er); err != nil {
		return er, err
	}
	if err := ValidateResearchEvidenceForExecution(er.ID, er.ResearchEvidence); err != nil {
		return er, err
	}
	return er, nil
}

func unmarshalInvocation(r *store.Record) (InvocationRecord, error) {
	var iv InvocationRecord
	if err := json.Unmarshal(r.Data, &iv); err != nil {
		return iv, fmt.Errorf("execjournal: corrupt invocation record %s: %w", r.ID, err)
	}
	if iv.Version != SchemaVersion {
		return iv, fmt.Errorf("execjournal: unsupported invocation record version %d for %s", iv.Version, r.ID)
	}
	if iv.ID == "" || iv.ExecutionID == "" {
		return iv, fmt.Errorf("execjournal: invocation record %s is incomplete", r.ID)
	}
	switch iv.Status {
	case InvDispatched, InvCompleted, InvFailed, InvUnknown:
	default:
		return iv, fmt.Errorf("execjournal: invocation record %s has invalid status %q", iv.ID, iv.Status)
	}
	return iv, nil
}
