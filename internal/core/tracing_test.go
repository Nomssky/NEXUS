package core

// Execution trace unit & integration tests
// (contracts/EXECUTION_TRACE_CONTRACTS.md): capture order, capture-time
// redaction, capacity behavior, terminal semantics, concurrency, and
// read-only retrieval through the engine.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

func newTraceTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })
	return e
}

func tracePublish(e *Engine, corr string, typ event.EventType, data string) {
	_ = e.EventBus().Publish(&event.Event{
		ID: fmt.Sprintf("%s-%s", corr, typ), Type: typ, Source: "test",
		Timestamp: e.now(), CorrelationID: corr, Priority: event.PriorityNormal,
		Data: []byte(data),
	})
}

func mustView(t *testing.T, e *Engine, corr string) *TraceView {
	t.Helper()
	v, ok := e.ExecutionTrace(corr, 0, MaxTraceEntries)
	if !ok {
		t.Fatalf("expected a trace for %s", corr)
	}
	return v
}

// A causally consistent capture: publish order is preserved, the envelope
// is kept verbatim, and the first terminal marker is located.
func TestTraceCaptureOrderAndTerminal(t *testing.T) {
	e := newTraceTestEngine(t)
	tracePublish(e, "exec-1", event.EventType("chain.started"),
		`{"step":"chain.started","actor":"core","outcome":"free text intent"}`)
	tracePublish(e, "exec-1", event.EventTypeGovernanceDecided,
		`{"proposal_id":"p-1","decision":"allow","action":"tool_call"}`)
	tracePublish(e, "exec-1", event.EventType("chain.completed"),
		`{"step":"chain.completed","actor":"core","outcome":"completed"}`)

	v := mustView(t, e, "exec-1")
	if len(v.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(v.Entries))
	}
	for i, en := range v.Entries {
		if en.Seq != uint64(i+1) {
			t.Fatalf("entry %d has seq %d, want %d", i, en.Seq, i+1)
		}
	}
	if v.Entries[0].Type != "chain.started" || v.Entries[2].Type != "chain.completed" {
		t.Fatalf("capture order broken: %s ... %s", v.Entries[0].Type, v.Entries[2].Type)
	}
	if v.Entries[0].Data["step"] != "chain.started" || v.Entries[0].Data["actor"] != "core" {
		t.Fatalf("envelope data lost: %+v", v.Entries[0].Data)
	}
	if v.Capture.TerminalSeq != 3 || v.Capture.PostTerminal != 0 {
		t.Fatalf("terminal marker: seq=%d post=%d, want seq=3 post=0",
			v.Capture.TerminalSeq, v.Capture.PostTerminal)
	}
	if !v.Capture.Available || v.Capture.FirstSeq != 1 || v.Capture.LastSeq != 3 {
		t.Fatalf("capture stats wrong: %+v", v.Capture)
	}
}

// Capture-time redaction (contract §9): free text and non-allowlisted keys
// are dropped and NAMED, values are bounded, unparseable payloads keep the
// envelope — and no sentinel survives serialization.
func TestTraceRedactionAtCapture(t *testing.T) {
	e := newTraceTestEngine(t)
	long := strings.Repeat("r", 500)
	sentinels := []string{"SENTINEL-OBJECTIVE", "SENTINEL-INTENT", "SENTINEL-REASON", "SENTINEL-UNKNOWN"}

	tracePublish(e, "exec-r", event.EventTypeObjectiveStarted,
		`{"state":"created","objective":"`+sentinels[0]+`"}`)
	tracePublish(e, "exec-r", event.EventTypeStepStarted,
		`{"step":"s1","intent":"`+sentinels[1]+`"}`)
	tracePublish(e, "exec-r", event.EventTypeApprovalApproved,
		`{"approval_id":"apr-1","approver_id":"owner","decision_rationale":"`+sentinels[2]+`"}`)
	tracePublish(e, "exec-r", event.EventTypeActionRejected,
		`{"action":"unknown","reason":"`+long+`","unexpected_key":"`+sentinels[3]+`"}`)
	tracePublish(e, "exec-r", event.EventType("chain.started"),
		`{"step":"chain.started","actor":"core","outcome":"unterminated`)

	v := mustView(t, e, "exec-r")
	byType := map[string]*TraceEntry{}
	for _, en := range v.Entries {
		byType[en.Type] = en
	}

	// Free-text keys are dropped and named; safe siblings are kept.
	obj := byType[string(event.EventTypeObjectiveStarted)]
	if _, kept := obj.Data["objective"]; kept {
		t.Fatal("objective text must never enter the trace")
	}
	if !contains(obj.RedactedKeys, "objective") || obj.Data["state"] != "created" {
		t.Fatalf("objective.started: data=%v redacted=%v", obj.Data, obj.RedactedKeys)
	}
	st := byType[string(event.EventTypeStepStarted)]
	if _, kept := st.Data["intent"]; kept || !contains(st.RedactedKeys, "intent") {
		t.Fatalf("step intent must be redacted: %v %v", st.Data, st.RedactedKeys)
	}
	ap := byType[string(event.EventTypeApprovalApproved)]
	if _, kept := ap.Data["decision_rationale"]; kept || !contains(ap.RedactedKeys, "decision_rationale") {
		t.Fatalf("approver prose must be redacted: %v %v", ap.Data, ap.RedactedKeys)
	}
	if ap.Data["approval_id"] != "apr-1" || ap.Data["approver_id"] != "owner" {
		t.Fatalf("approval identifiers must be kept: %v", ap.Data)
	}
	// Non-allowlisted keys are dropped and named; values are bounded.
	rj := byType[string(event.EventTypeActionRejected)]
	if _, kept := rj.Data["unexpected_key"]; kept || !contains(rj.RedactedKeys, "unexpected_key") {
		t.Fatalf("unknown keys must be redacted: %v %v", rj.Data, rj.RedactedKeys)
	}
	if got := rj.Data["reason"]; len([]rune(got)) != maxTraceValueLen+3 {
		t.Fatalf("value bound: got %d runes", len([]rune(got)))
	}
	// Unparseable source payload: envelope kept, data visibly absent.
	up := byType["chain.started"]
	if !up.DataUnparseable || len(up.Data) != 0 || len(up.RedactedKeys) != 0 {
		t.Fatalf("unparseable payload must keep envelope with empty data: %+v", up)
	}

	// Nothing redacted can re-enter through serialization.
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sentinels {
		if strings.Contains(string(blob), s) {
			t.Fatalf("sentinel %q leaked into the serialized trace", s)
		}
	}
}

// Capacity is explicit and deterministic (contract §11): per-execution
// drop-oldest with a visible counter and a sequence gap.
func TestTracePerExecutionCapacity(t *testing.T) {
	e := newTraceTestEngine(t)
	total := MaxTraceEntries + 44
	for i := 1; i <= total; i++ {
		tracePublish(e, "exec-cap", event.EventTypeCustom, fmt.Sprintf(`{"count":"%d"}`, i))
	}
	v := mustView(t, e, "exec-cap")
	c := v.Capture
	if c.Entries != MaxTraceEntries || c.Dropped != 44 || !c.Truncated {
		t.Fatalf("capacity stats: %+v", c)
	}
	if c.FirstSeq != 45 || c.LastSeq != uint64(total) {
		t.Fatalf("sequence gap must be visible: %+v", c)
	}
	if len(v.Entries) != MaxTraceEntries {
		t.Fatalf("expected %d entries, got %d", MaxTraceEntries, len(v.Entries))
	}
}

// The global trace registry is bounded FIFO; eviction is honest (contract §11).
func TestTraceGlobalEviction(t *testing.T) {
	e := newTraceTestEngine(t)
	for i := 0; i < MaxTracedExecutions+1; i++ {
		tracePublish(e, fmt.Sprintf("c-%d", i), event.EventTypeCustom, `{"count":"1"}`)
	}
	if _, ok := e.ExecutionTrace("c-0", 0, 1); ok {
		t.Fatal("oldest trace must be evicted at capacity")
	}
	if _, ok := e.ExecutionTrace("c-1", 0, 1); !ok {
		t.Fatal("recent traces must survive eviction")
	}
	if _, ok := e.ExecutionTrace("", 0, 1); ok {
		t.Fatal("an empty correlation must never resolve to a trace")
	}
}

// An approval wait is not terminal (contract §8): the first terminal marker
// is the resume episode's completed state, and later markers stay visible.
func TestTraceWaitStateIsNotTerminal(t *testing.T) {
	e := newTraceTestEngine(t)
	tracePublish(e, "exec-w", event.EventTypeObjectiveCompleted, `{"state":"pending_approval"}`)
	tracePublish(e, "exec-w", event.EventTypeApprovalRequested, `{"approval_id":"apr-1"}`)
	tracePublish(e, "exec-w", event.EventTypeApprovalApproved, `{"approval_id":"apr-1"}`)
	tracePublish(e, "exec-w", event.EventTypeObjectiveCompleted, `{"state":"completed"}`)
	tracePublish(e, "exec-w", event.EventType("chain.completed"), `{"outcome":"completed"}`)

	v := mustView(t, e, "exec-w")
	if v.Capture.TerminalSeq != 4 {
		t.Fatalf("first terminal marker must be seq 4, got %d", v.Capture.TerminalSeq)
	}
	if v.Capture.PostTerminal != 1 {
		t.Fatalf("entries after the marker must be counted, got %d", v.Capture.PostTerminal)
	}
	// Both episodes remain visible — nothing is folded.
	states := []string{}
	for _, en := range v.Entries {
		if en.Type == string(event.EventTypeObjectiveCompleted) {
			states = append(states, en.Data["state"])
		}
	}
	if len(states) != 2 || states[0] != "pending_approval" || states[1] != "completed" {
		t.Fatalf("wait and terminal episodes must both appear, got %v", states)
	}
}

// Concurrent producers and readers must never corrupt data or race
// (mission test 20).
func TestTraceConcurrentPublishAndRead(t *testing.T) {
	e := newTraceTestEngine(t)
	const publishers, perPublisher = 8, 20
	corrs := []string{"cc-0", "cc-1", "cc-2", "cc-3"}

	var wg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perPublisher; i++ {
				corr := corrs[p%len(corrs)]
				tracePublish(e, corr, event.EventTypeCustom, fmt.Sprintf(`{"count":"%d"}`, i))
			}
		}(p)
	}
	readers := 4
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				for _, c := range corrs {
					if v, ok := e.ExecutionTrace(c, 0, MaxTraceEntries); ok {
						var seq uint64
						for _, en := range v.Entries {
							if en.Seq < seq {
								t.Errorf("entries out of capture order: %d after %d", en.Seq, seq)
							}
							seq = en.Seq
						}
					}
				}
			}
		}()
	}
	wg.Wait()

	for _, c := range corrs {
		v := mustView(t, e, c)
		if v.Capture.Entries != publishers/len(corrs)*perPublisher {
			t.Fatalf("%s: expected %d entries, got %d", c, publishers/len(corrs)*perPublisher, v.Capture.Entries)
		}
	}
}

// Retrieval is read-only and process-local: a real chain execution is
// captured, reading changes nothing, and unknown ids never resolve.
func TestExecutionTraceEngineIntegration(t *testing.T) {
	e := newTraceTestEngine(t)
	req := &Request{
		ID: "exec-chain-1", Context: NewRequestContext("exec-chain-1", "biz-1", "user-1"),
		Intent: "trace integration",
		Handler: func(ctx context.Context, wr *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
			return &executor.Outcome{TaskID: wr.TaskID, Status: "completed", Output: "ok",
				CorrelationID: wr.CorrelationID, BusinessID: wr.BusinessID}, nil
		},
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.GetResult("exec-chain-1"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	result, ok := e.GetResult("exec-chain-1")
	if !ok || result.Status != "completed" {
		t.Fatalf("expected completed result, got %+v", result)
	}

	v := mustView(t, e, "exec-chain-1")
	var started, completed bool
	for _, en := range v.Entries {
		switch en.Type {
		case "chain.started":
			started = true
		case "chain.completed":
			completed = true
		}
	}
	if !started || !completed {
		t.Fatalf("chain lifecycle missing from trace: started=%v completed=%v", started, completed)
	}

	// Read-only: a second read is identical and the stored record is intact.
	v2 := mustView(t, e, "exec-chain-1")
	if len(v.Entries) != len(v2.Entries) || v.Capture != v2.Capture {
		t.Fatal("trace retrieval must be deterministic and side-effect free")
	}
	if r2, _ := e.GetResult("exec-chain-1"); r2 != result {
		t.Fatal("trace retrieval must not touch the execution record")
	}
	if _, ok := e.ExecutionTrace("exec-unknown", 0, 10); ok {
		t.Fatal("an unknown execution must have no trace")
	}
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
