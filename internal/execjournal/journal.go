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

// Accepted durably records a newly accepted execution. It MUST succeed before
// the caller reports durable acceptance.
func (j *Journal) Accepted(id, businessID, divisionID, actorID string) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	now := j.now().UTC()
	rec := ExecutionRecord{
		Version: SchemaVersion, ID: id, BusinessID: businessID, DivisionID: divisionID,
		ActorID: actorID, State: StateAccepted, CreatedAt: now, UpdatedAt: now,
	}
	return j.putExecution(rec)
}

// Terminal durably records a terminal lifecycle state. `errClass` is a coarse
// classification only (e.g. an error code), never a raw message.
func (j *Journal) Terminal(id string, state State, errClass string) error {
	if j == nil {
		return nil
	}
	if id == "" {
		return fmt.Errorf("execjournal: execution id required")
	}
	if !state.Terminal() {
		return fmt.Errorf("execjournal: %q is not a terminal state", state)
	}
	existing, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	if !ok {
		// A terminal transition without a prior accepted record still records
		// the fact; the id is authoritative.
		existing = ExecutionRecord{Version: SchemaVersion, ID: id, CreatedAt: j.now().UTC()}
	}
	if existing.State.Terminal() {
		return nil // already terminal; terminal is final
	}
	existing.State = state
	existing.ErrorClass = errClass // coarse class only; never a raw message
	existing.UpdatedAt = j.now().UTC()
	return j.putExecution(existing)
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
	if err := j.putInvocation(rec); err != nil {
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
	existing, ok, err := j.getInvocation(invocationID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // intent record gone; nothing to update
	}
	existing.Status = status
	existing.ErrorClass = errorClass
	existing.UpdatedAt = j.now().UTC()
	return j.putInvocation(existing)
}

// Get returns a durable execution record (for operators/tests).
func (j *Journal) Get(id string) (ExecutionRecord, bool, error) {
	if j == nil {
		return ExecutionRecord{}, false, nil
	}
	return j.getExecution(id)
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
		if err := j.putExecution(er); err != nil {
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
		if err := j.putInvocation(iv); err != nil {
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

func (j *Journal) putExecution(rec ExecutionRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("execjournal: marshal execution: %w", err)
	}
	sr := &store.Record{
		ID: execKey(rec.ID), Type: recordTypeExecution, Status: store.RecordStatusActive,
		BusinessID: rec.BusinessID, DivisionID: rec.DivisionID, Data: data,
	}
	// Carry the current store version so both MemStore (strict equality) and
	// FileStore (0 = no check) accept the update consistently.
	if existing, gerr := j.st.Get(sr.ID); gerr == nil && existing != nil {
		sr.Version = existing.Version
	}
	return j.st.Put(sr)
}

func (j *Journal) putInvocation(rec InvocationRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("execjournal: marshal invocation: %w", err)
	}
	sr := &store.Record{
		ID: invKey(rec.ID), Type: recordTypeInvocation, Status: store.RecordStatusActive,
		BusinessID: "",
		Data:       data,
	}
	if existing, gerr := j.st.Get(sr.ID); gerr == nil && existing != nil {
		sr.Version = existing.Version
	}
	return j.st.Put(sr)
}

func (j *Journal) getExecution(id string) (ExecutionRecord, bool, error) {
	r, err := j.st.Get(execKey(id))
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	if r == nil || r.Type != recordTypeExecution {
		return ExecutionRecord{}, false, nil
	}
	er, err := unmarshalExecution(r)
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	return er, true, nil
}

func (j *Journal) getInvocation(id string) (InvocationRecord, bool, error) {
	r, err := j.st.Get(invKey(id))
	if err != nil {
		return InvocationRecord{}, false, err
	}
	if r == nil || r.Type != recordTypeInvocation {
		return InvocationRecord{}, false, nil
	}
	iv, err := unmarshalInvocation(r)
	if err != nil {
		return InvocationRecord{}, false, err
	}
	return iv, true, nil
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
