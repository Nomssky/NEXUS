package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// publishEscalated publishes one governance.escalated event with the given
// raw data payload and dispatches it through the intake consumer.
func publishEscalated(t *testing.T, e *Engine, eventID, businessID string, data []byte) {
	t.Helper()
	if err := e.EventBus().Publish(&event.Event{
		ID:         eventID,
		Type:       event.EventTypeGovernanceEscalated,
		Source:     "test",
		Timestamp:  time.Now(),
		BusinessID: businessID,
		Data:       data,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

// escalationJSON builds a CTR-GOV-002 input payload.
func escalationJSON(t *testing.T, fields map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// TEST-CORE-061 (C): chain-gate ESCALATE lands in the escalation queue with
// the contract input fields, deadline, and a linked attention item —
// completing the D3 handoff that previously went unconsumed.
func TestEscalationQueueChainGate(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "escalate-all",
			Name:       "Escalate All",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ESCALATE,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-esc-queue",
		Context: NewRequestContext("corr-esc-queue", "biz-1", "user-1"),
		Intent:  "work that must escalate",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	if result.Error == nil || result.Error.Details["escalation_ref"] == "" {
		t.Fatalf("expected escalation_ref in envelope, got %+v", result.Error)
	}
	ref := result.Error.Details["escalation_ref"]

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	esc, ok := e.GetEscalation(ref)
	if !ok {
		t.Fatalf("escalation %q not queued (queue=%d)", ref, len(e.Escalations()))
	}
	if esc.Status != EscalationPending {
		t.Errorf("status: want pending, got %s", esc.Status)
	}
	if esc.RequestID != req.ID {
		t.Errorf("request_id: want %s, got %s", req.ID, esc.RequestID)
	}
	if esc.Gate != "chain" {
		t.Errorf("gate: want chain, got %q", esc.Gate)
	}
	if esc.Reason == "" {
		t.Error("expected non-empty reason")
	}
	if esc.Context["business_id"] != "biz-1" || esc.Context["correlation_id"] != "corr-esc-queue" {
		t.Errorf("context: got %v", esc.Context)
	}
	// Priority unset → schema §4.4 default "high" (7); deadline defaults to
	// the TTL because the request carried none.
	if esc.Urgency != defaultEscalationUrgency {
		t.Errorf("urgency: want %d, got %d", defaultEscalationUrgency, esc.Urgency)
	}
	if !esc.Deadline.Equal(now.Add(defaultEscalationTTL)) {
		t.Errorf("deadline: want %v, got %v", now.Add(defaultEscalationTTL), esc.Deadline)
	}
	if esc.AttentionID == "" {
		t.Fatal("expected linked attention item")
	}
	attItem, ok := e.attentionEng.GetItem(esc.AttentionID)
	if !ok {
		t.Fatalf("attention item %q not found", esc.AttentionID)
	}
	if attItem.Source != "governance" {
		t.Errorf("attention source: want governance, got %q", attItem.Source)
	}
	if attItem.ExpiresAt == nil || !attItem.ExpiresAt.Equal(esc.Deadline) {
		t.Errorf("attention expires_at: want %v, got %v", esc.Deadline, attItem.ExpiresAt)
	}
	if !attItem.IsPolicyViolation {
		t.Error("governance escalation must carry the policy-violation guardrail")
	}
}

// TEST-CORE-062 (C): a redelivered governance.escalated event is acked
// idempotently — one queue record, one attention item.
func TestEscalationQueueIdempotentRedelivery(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(2 * time.Hour).UTC().Format(time.RFC3339)
	data := escalationJSON(t, map[string]string{
		"escalation_ref": "esc-redeliver-1",
		"request_id":     "req-r1",
		"requester_id":   "user-1",
		"reason":         "policy says escalate",
		"gate":           "chain",
		"urgency":        "9",
		"deadline":       deadline,
	})
	publishEscalated(t, e, "ev-esc-1", "biz-1", data)
	publishEscalated(t, e, "ev-esc-2", "biz-1", data) // redelivery

	list := e.Escalations()
	if len(list) != 1 {
		t.Fatalf("want exactly 1 escalation, got %d", len(list))
	}
	esc := list[0]
	if esc.Urgency != 9 {
		t.Errorf("urgency: want 9 from contract input, got %d", esc.Urgency)
	}
	if !esc.Deadline.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("deadline: want %v, got %v", now.Add(2*time.Hour), esc.Deadline)
	}
	if got := e.attentionEng.ItemCount(); got != 1 {
		t.Errorf("attention items: want 1 (no duplicate on redelivery), got %d", got)
	}
}

// TEST-CORE-063 (C): escalation lifecycle pending → acknowledged → resolved.
func TestEscalationAcknowledgeResolve(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	publishEscalated(t, e, "ev-esc-ack", "biz-1", escalationJSON(t, map[string]string{
		"escalation_ref": "esc-ack-1",
		"reason":         "needs a human",
		"gate":           "executor",
	}))

	if err := e.AcknowledgeEscalation("esc-ack-1"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	esc, _ := e.GetEscalation("esc-ack-1")
	if esc.Status != EscalationAcknowledged || esc.AcknowledgedAt == nil {
		t.Errorf("want acknowledged with timestamp, got %s %v", esc.Status, esc.AcknowledgedAt)
	}
	if err := e.AcknowledgeEscalation("esc-ack-1"); err == nil {
		t.Error("second acknowledge must fail (not pending)")
	}
	if err := e.ResolveEscalation("esc-ack-1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	esc, _ = e.GetEscalation("esc-ack-1")
	if esc.Status != EscalationResolved || esc.ResolvedAt == nil {
		t.Errorf("want resolved with timestamp, got %s %v", esc.Status, esc.ResolvedAt)
	}
	if err := e.ResolveEscalation("esc-ack-1"); err == nil {
		t.Error("resolve on resolved must fail")
	}
	if err := e.AcknowledgeEscalation("esc-nope"); err == nil {
		t.Error("unknown id must fail")
	}
}

// TEST-CORE-064 (C): a past-deadline escalation expires (§4.6: expiration =
// silence = no action) and is terminal — no late acknowledges or resolves.
func TestEscalationExpiresAfterDeadline(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(time.Hour)
	publishEscalated(t, e, "ev-esc-exp", "biz-1", escalationJSON(t, map[string]string{
		"escalation_ref": "esc-exp-1",
		"reason":         "quiet escalation",
		"gate":           "chain",
		"deadline":       deadline.UTC().Format(time.RFC3339),
	}))

	esc, _ := e.GetEscalation("esc-exp-1")
	if esc.Status != EscalationPending {
		t.Fatalf("before deadline: want pending, got %s", esc.Status)
	}

	now = deadline.Add(time.Minute) // move the shared clock past the deadline

	esc, ok := e.GetEscalation("esc-exp-1")
	if !ok || esc.Status != EscalationExpired {
		t.Fatalf("after deadline: want expired, got %v (ok=%v)", esc, ok)
	}
	if err := e.AcknowledgeEscalation("esc-exp-1"); err == nil {
		t.Error("acknowledge after expiry must fail")
	}
	if err := e.ResolveEscalation("esc-exp-1"); err == nil {
		t.Error("resolve after expiry must fail")
	}
}

// TEST-CORE-065 (C): contract failure rule — "Logged, retry once". An
// intake that cannot submit the attention item still acks the escalation
// (the queue record is the escalation_id), and malformed events are logged
// and dropped instead of requeued forever.
func TestEscalationIntakeFailureLoggedRetryOnce(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	// Empty reason → attention SubmitItem rejects the empty title on both
	// the first attempt and the single contract retry; the escalation must
	// still be queued (ack) with no attention link.
	publishEscalated(t, e, "ev-esc-bad-title", "biz-1", escalationJSON(t, map[string]string{
		"escalation_ref": "esc-bad-title",
		"reason":         "",
		"gate":           "chain",
	}))
	esc, ok := e.GetEscalation("esc-bad-title")
	if !ok {
		t.Fatal("escalation must be queued even when attention submit fails")
	}
	if esc.AttentionID != "" {
		t.Errorf("want no attention link after failed submit, got %q", esc.AttentionID)
	}

	// Malformed payload: logged and dropped, no queue entry.
	publishEscalated(t, e, "ev-esc-malformed", "biz-1", []byte("{not json"))
	publishEscalated(t, e, "ev-esc-no-ref", "biz-1", escalationJSON(t, map[string]string{
		"reason": "missing ref",
	}))
	for _, esc := range e.Escalations() {
		if strings.Contains(esc.Reason, "missing ref") {
			t.Errorf("event without escalation_ref must be dropped, got %+v", esc)
		}
	}
	if len(e.Escalations()) != 1 {
		t.Errorf("want only the bad-title escalation queued, got %d", len(e.Escalations()))
	}
}
