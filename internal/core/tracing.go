package core

// Execution trace capture and read model (EXECUTION_TRACE_CONTRACTS.md v7).
//
// The engine's bus is built with a synchronous publish-time tap
// (contract §3): every accepted event carrying a correlation identity is
// redacted at capture time (§9) and appended, in publish order, to a
// bounded per-execution buffer (§11). Nothing here publishes, schedules,
// persists or mutates runtime state; the trace is a view, never a source
// of authority (§12). Durability is process-local by construction — the
// buffers live exactly as long as the engine that created the bus.

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

const (
	// MaxTraceEntries bounds the entries retained per execution. At capacity
	// the oldest entry is dropped and counted (EXECUTION_TRACE_CONTRACTS §11).
	MaxTraceEntries = 256
	// MaxTracedExecutions bounds how many executions keep a trace buffer;
	// eviction is oldest-first and drops the whole buffer (contract §11).
	MaxTracedExecutions = 512
	// maxTraceValueLen bounds every payload value, in runes (contract §9).
	maxTraceValueLen = 200
	// maxTraceDataKeys bounds payload keys retained per entry (contract §9).
	maxTraceDataKeys = 32
)

// TraceEntry is one captured event, redacted at capture time (contract §4,
// §9). Seq is the per-execution capture position — the publish order at the
// single bus funnel — not a source-assigned sequence number.
type TraceEntry struct {
	Seq             uint64            `json:"seq"`
	EventID         string            `json:"event_id"`
	Type            string            `json:"type"`
	Source          string            `json:"source"`
	Timestamp       time.Time         `json:"timestamp"`
	TaskID          string            `json:"task_id,omitempty"`
	CausationID     string            `json:"causation_id,omitempty"`
	Data            map[string]string `json:"data,omitempty"`
	RedactedKeys    []string          `json:"redacted_keys,omitempty"`
	DataUnparseable bool              `json:"data_unparseable,omitempty"`
}

// TraceCapture reports the buffer-wide capture facts for one execution
// (contract §11): how much was kept, what capacity or parsing dropped, and
// where the first terminal marker fell (§8). Counts are buffer-wide even
// when the entries in the response are a paged window.
type TraceCapture struct {
	Available    bool   `json:"available"`
	Entries      int    `json:"entries"`
	Dropped      int    `json:"dropped"`
	Limit        int    `json:"limit"`
	Truncated    bool   `json:"truncated"`
	FirstSeq     uint64 `json:"first_seq,omitempty"`
	LastSeq      uint64 `json:"last_seq,omitempty"`
	TerminalSeq  uint64 `json:"terminal_seq,omitempty"`
	PostTerminal int    `json:"post_terminal,omitempty"`
	HasMore      bool   `json:"has_more,omitempty"`
}

// TraceView is the read model for one window of one execution's trace
// (contract §10).
type TraceView struct {
	Capture TraceCapture  `json:"capture"`
	Entries []*TraceEntry `json:"entries"`
}

// traceBuffer is one execution's bounded capture buffer. Entries are
// append-only and immutable once recorded; only the slice head moves at
// capacity. Guarded by the registry lock.
type traceBuffer struct {
	entries     []*TraceEntry
	dropped     int
	nextSeq     uint64
	terminalSeq uint64
}

// traceRegistry is the bounded in-process trace store (contract §11): at
// most MaxTracedExecutions buffers, evicted oldest-first.
type traceRegistry struct {
	mu     sync.Mutex
	traces map[string]*traceBuffer
	order  []string
}

func newTraceRegistry() *traceRegistry {
	return &traceRegistry{traces: make(map[string]*traceBuffer)}
}

// record is the bus tap. Redaction happens before the registry lock is
// taken (contract §3, §9) so no unredacted payload ever reaches a buffer
// and the critical section stays O(1).
func (r *traceRegistry) record(ev *event.Event) {
	if ev == nil || ev.CorrelationID == "" {
		return
	}
	data, redacted, unparseable := sanitizeTraceData(ev.Type, ev.Data)
	entry := &TraceEntry{
		EventID:         ev.ID,
		Type:            string(ev.Type),
		Source:          ev.Source,
		Timestamp:       ev.Timestamp,
		TaskID:          ev.TaskID,
		CausationID:     ev.CausationID,
		Data:            data,
		RedactedKeys:    redacted,
		DataUnparseable: unparseable,
	}
	marker := traceTerminalMarker(ev)

	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.traces[ev.CorrelationID]
	if !ok {
		buf = &traceBuffer{nextSeq: 1}
		r.traces[ev.CorrelationID] = buf
		r.order = append(r.order, ev.CorrelationID)
		for len(r.order) > MaxTracedExecutions {
			delete(r.traces, r.order[0])
			r.order = r.order[1:]
		}
	}
	entry.Seq = buf.nextSeq
	buf.nextSeq++
	buf.entries = append(buf.entries, entry)
	if len(buf.entries) > MaxTraceEntries {
		buf.entries = buf.entries[1:]
		buf.dropped++
	}
	// First terminal marker wins and is never rewritten; later markers ride
	// in the entries and are counted, never folded (contract §8).
	if marker && buf.terminalSeq == 0 {
		buf.terminalSeq = entry.Seq
	}
}

// view returns the bounded window of one execution's trace. ok is false
// when no buffer exists: the execution was never captured or its trace was
// evicted at capacity (contract §10, §11) — both are reported honestly by
// the caller, never fabricated here.
func (r *traceRegistry) view(correlationID string, afterSeq uint64, limit int) (*TraceView, bool) {
	if correlationID == "" {
		return nil, false
	}
	if limit <= 0 || limit > MaxTraceEntries {
		limit = MaxTraceEntries
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.traces[correlationID]
	if !ok {
		return nil, false
	}

	capture := TraceCapture{
		Available:   true,
		Entries:     len(buf.entries),
		Dropped:     buf.dropped,
		Limit:       MaxTraceEntries,
		Truncated:   buf.dropped > 0,
		TerminalSeq: buf.terminalSeq,
	}
	if len(buf.entries) > 0 {
		capture.FirstSeq = buf.entries[0].Seq
		capture.LastSeq = buf.entries[len(buf.entries)-1].Seq
	}
	if buf.terminalSeq != 0 {
		for _, e := range buf.entries {
			if e.Seq > buf.terminalSeq {
				capture.PostTerminal++
			}
		}
	}

	entries := make([]*TraceEntry, 0, limit)
	for _, e := range buf.entries {
		if e.Seq <= afterSeq {
			continue
		}
		if len(entries) == limit {
			capture.HasMore = true
			break
		}
		// Entries are immutable after record; sharing the pointer is safe.
		entries = append(entries, e)
	}
	return &TraceView{Capture: capture, Entries: entries}, true
}

// traceDenyKeys are dropped even when otherwise allowed: caller- or
// approver-supplied free text (EXECUTION_TRACE_CONTRACTS §9).
var traceDenyKeys = map[string]bool{
	"decision_rationale": true, // approver prose (approval.* extras)
	"objective":          true, // request objective text (objective.started)
	"intent":             true, // model-authored step/delegate intent text
}

// traceTypeSkipKeys drops specific keys for specific event types: on
// chain.started the outcome field carries the request intent — free text
// the trace must not hold (contract §9).
var traceTypeSkipKeys = map[event.EventType]map[string]bool{
	event.EventType("chain.started"): {"outcome": true},
}

// traceWaitStates are objective.completed states that end an episode
// without ending the execution: an approval wait is not terminal
// (EXECUTION_TRACE_CONTRACTS §8).
var traceWaitStates = map[string]bool{"pending_approval": true}

// traceAllowedKeys is the explicit payload allowlist
// (EXECUTION_TRACE_CONTRACTS §9): runtime-established identifiers, enums,
// counts, durations, error classes and bounded runtime reasons only.
// Anything else — and therefore anything a future leaky payload might
// carry, including prompts, model output, tool arguments/results and
// credentials — is dropped and named in redacted_keys, never retained.
var traceAllowedKeys = map[string]bool{
	// Envelope and lifecycle
	"event": true, "step": true, "step_id": true, "actor": true, "state": true,
	"status": true, "outcome": true, "phase": true, "action": true,
	"action_type": true, "reason": true, "reason_category": true,
	"iteration": true, "iterations": true, "execution_kind": true,
	// Identity and correlation (runtime-established only)
	"agent_id": true, "objective_id": true, "execution_id": true,
	"request_id": true, "requester_id": true, "requester": true,
	"approver_id": true, "actor_id": true, "division_id": true,
	"business_id": true, "correlation_id": true, "proposal_id": true,
	"approval_id": true, "escalation_id": true, "escalation_ref": true,
	"observation_id": true, "memory_id": true, "plan_id": true,
	// Delegation and coordination linkage
	"parent_agent_id": true, "parent_task_id": true, "parent_version": true,
	"delegation_depth": true, "delegation_action_id": true, "delegate_id": true,
	"child_agent_id": true, "child_task_id": true, "child_status": true,
	"child_tools": true, "depth": true, "group_id": true, "members": true,
	"group_size": true, "preferred_agent": true, "refused_agent": true,
	"refused_index": true,
	// Governance
	"decision": true, "governance_outcome": true, "policy_id": true,
	"policy_version": true, "risk_level": true, "gate": true, "urgency": true,
	"resource": true, "resource_type": true, "deadline": true, "options": true,
	"constraints": true, "constraint_id": true, "constraint_type": true,
	"constraint_failure": true, "pending_approval": true, "denied": true,
	"escalated": true, "completed": true, "failed": true, "ok": true,
	"refused": true, "cancelled": true,
	// Tools, dispatch and reliability
	"tool_id": true, "operation": true, "call_id": true, "attempt": true,
	"attempts": true, "max_attempts": true, "error_class": true,
	"error_kind": true, "duration_ms": true, "retry_in_ms": true,
	"retry_after_ms": true, "retry_recommended": true, "side_effect_class": true,
	"credentialed_external_mutation": true, "adapter": true, "truncated": true,
	"redacted": true, "unknown": true, "timed_out": true, "bytes": true,
	"result_bytes": true,
	// Model boundary
	"provider": true, "model": true, "finish_reason": true, "matched": true,
	"valid": true, "rejections": true,
	// Plans, steps, budget, context
	"plan_version": true, "version": true, "steps": true, "budget": true,
	"used": true, "memory_records": true, "observations": true,
	"memory_chars": true, "obs_chars": true, "kind": true, "dropped": true,
	"observation": true, "source": true,
	// Memory metadata (AGENT_MEMORY_CONTEXT_CONTRACTS §16)
	"key": true, "scope": true, "trust": true, "found": true,
	"memory_type": true, "memory_scope": true, "record_count": true,
	"records": true, "returned": true, "conflict": true, "expired": true,
	"tool_calls": true, "model_calls": true, "delegations": true,
}

// sanitizeTraceData projects one event payload through the capture-time
// allowlist (EXECUTION_TRACE_CONTRACTS §9). It returns the retained data,
// the sorted names of dropped keys (names only, never values), and whether
// the source payload could not be parsed at all — in which case the entry
// keeps its envelope with empty data rather than a guess.
func sanitizeTraceData(typ event.EventType, raw []byte) (map[string]string, []string, bool) {
	if len(raw) == 0 {
		return nil, nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, true
	}
	skip := traceTypeSkipKeys[typ]
	redacted := make([]string, 0)
	keep := make([]string, 0, len(fields))
	for k := range fields {
		if traceDenyKeys[k] || skip[k] || !traceAllowedKeys[k] {
			redacted = append(redacted, k)
			continue
		}
		keep = append(keep, k)
	}
	sort.Strings(redacted)
	sort.Strings(keep)
	if len(keep) > maxTraceDataKeys {
		redacted = append(redacted, keep[maxTraceDataKeys:]...)
		sort.Strings(redacted)
		keep = keep[:maxTraceDataKeys]
	}
	if len(keep) == 0 && len(redacted) == 0 {
		return nil, nil, false
	}
	data := make(map[string]string, len(keep))
	for _, k := range keep {
		data[k] = traceValue(fields[k])
	}
	return data, redacted, false
}

// traceValue renders one payload value: strings are truncated as-is;
// numbers, booleans and structures are re-encoded compactly under the same
// bound (governance.escalated carries a small options array, for example).
func traceValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return truncateTraceValue(s)
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return truncateTraceValue(string(b))
}

func truncateTraceValue(s string) string {
	if utf8.RuneCountInString(s) <= maxTraceValueLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxTraceValueLen]) + "..."
}

// traceTerminalMarker reports whether an event ends an execution episode
// (EXECUTION_TRACE_CONTRACTS §8). An approval wait (objective.completed
// with state=pending_approval) is a wait state, not a terminal marker.
func traceTerminalMarker(ev *event.Event) bool {
	switch ev.Type {
	case event.EventTypeObjectiveCompleted:
		var payload map[string]string
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			return true
		}
		return !traceWaitStates[payload["state"]]
	case event.EventType("chain.completed"),
		event.EventType("chain.cancelled"),
		event.EventType("chain.failed"):
		return true
	}
	return false
}

// ExecutionTrace returns the captured process-local trace window for one
// execution, keyed by its runtime correlation identity — which on the
// agent surfaces equals the execution id (EXECUTION_TRACE_CONTRACTS §2,
// §10). ok is false when no trace buffer exists (never captured, or evicted
// at capacity); callers must report that honestly instead of fabricating a
// trace. Retrieval is read-only: it never mutates execution, approval,
// governance, memory or tool state.
func (e *Engine) ExecutionTrace(correlationID string, afterSeq uint64, limit int) (*TraceView, bool) {
	if e.traces == nil {
		return nil, false
	}
	return e.traces.view(correlationID, afterSeq, limit)
}
