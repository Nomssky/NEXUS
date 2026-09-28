package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// wireJSON projects and serializes an event the way the SSE boundary does.
func wireJSON(t *testing.T, e *Event, nexusID string) (string, map[string]any) {
	t.Helper()
	data, err := json.Marshal(e.Wire(nexusID))
	if err != nil {
		t.Fatalf("marshal wire record: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal wire record: %v", err)
	}
	return string(data), m
}

// TEST-EVT-WIRE-01: the projection carries every sourced §2.2 field —
// envelope identity, nexus_id, scopes, both timestamps from the transport
// instant, the §2.3 producer, payload as raw JSON, and the priority enum.
func TestWireRecordSourcedFields(t *testing.T) {
	ts := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	e := &Event{
		ID:             "nx:event:abc",
		Type:           EventTypeApprovalRequested,
		Source:         "approval.engine",
		Timestamp:      ts,
		BusinessID:     "nx:business:1",
		DivisionID:     "nx:division:1",
		TaskID:         "nx:task:1",
		CorrelationID:  "corr-1",
		CausationID:    "cause-1",
		IdempotencyKey: "idem-1",
		Priority:       PriorityHigh,
		Data:           []byte(`{"decision":"approve"}`),
	}
	raw, m := wireJSON(t, e, "nx:nexus:wire")

	if m["schema_version"] != schema.Version {
		t.Errorf("schema_version: %v", m["schema_version"])
	}
	if m["entity_type"] != "event" {
		t.Errorf("entity_type: %v", m["entity_type"])
	}
	if m["event_id"] != "nx:event:abc" {
		t.Errorf("event_id: %v", m["event_id"])
	}
	if m["event_type"] != string(EventTypeApprovalRequested) {
		t.Errorf("event_type: %v", m["event_type"])
	}
	if m["nexus_id"] != "nx:nexus:wire" {
		t.Errorf("nexus_id: %v", m["nexus_id"])
	}
	if m["business_id"] != "nx:business:1" || m["division_id"] != "nx:division:1" {
		t.Errorf("scopes: %v %v", m["business_id"], m["division_id"])
	}
	if m["occurred_at"] != "2026-09-28T10:00:00Z" || m["emitted_at"] != "2026-09-28T10:00:00Z" {
		t.Errorf("timestamps must share the transport instant: %v %v", m["occurred_at"], m["emitted_at"])
	}
	prod, ok := m["producer"].(map[string]any)
	if !ok {
		t.Fatalf("producer missing: %v", m)
	}
	if prod["producer_id"] != "approval.engine" || prod["producer_type"] != "module" ||
		prod["module_name"] != "approval.engine" {
		t.Errorf("producer: %v", prod)
	}
	if m["task_id"] != "nx:task:1" || m["correlation_id"] != "corr-1" || m["causation_id"] != "cause-1" {
		t.Errorf("trace fields: %v %v %v", m["task_id"], m["correlation_id"], m["causation_id"])
	}
	if m["idempotency_key"] != "idem-1" {
		t.Errorf("idempotency_key: %v", m["idempotency_key"])
	}
	if m["priority"] != "high" {
		t.Errorf("priority enum: %v", m["priority"])
	}
	// Payload is embedded as a JSON object, never base64 ([]byte would
	// serialize as a base64 string).
	if _, ok := m["payload"].(map[string]any); !ok {
		t.Errorf("payload must be a JSON object, got %T: %v", m["payload"], m["payload"])
	}
	if want := `"payload":{"decision":"approve"}`; !strings.Contains(raw, want) {
		t.Errorf("raw payload missing %s in %s", want, raw)
	}
}

// TEST-EVT-WIRE-02: honest projection — §2.2 fields with no source are
// omitted (never fabricated), correlation_id is present even when empty
// (required field), and unsourced priority (out-of-table) is omitted.
func TestWireRecordOmitsUnsourcedFields(t *testing.T) {
	e := &Event{
		ID:        "nx:event:x",
		Type:      EventTypeCustom,
		Source:    "test.module",
		Timestamp: time.Now(),
	}
	_, m := wireJSON(t, e, "nx:nexus:wire")

	for _, key := range []string{
		"event_version", "payload_schema", "provenance", "actor",
		"workflow_id", "objective_id", "parent_event_id", "classification",
		"deduplication_key", "ordering", "ttl_seconds", "expires_at",
	} {
		if _, present := m[key]; present {
			t.Errorf("%s has no source on the transport and must be omitted", key)
		}
	}
	// Required-by-§2.2 correlation_id is emitted even when empty.
	if v, present := m["correlation_id"]; !present {
		t.Error("correlation_id is required by §2.2 and must be present")
	} else if v != "" {
		t.Errorf("correlation_id: %v", v)
	}
	// Zero-value priority is PriorityLow (0) → contract "low", present.
	if m["priority"] != "low" {
		t.Errorf("zero priority must map to low, got %v", m["priority"])
	}
	// Out-of-table priority → omitted, not a wrong enum member.
	e.Priority = Priority(3)
	_, m2 := wireJSON(t, e, "nx:nexus:wire")
	if _, present := m2["priority"]; present {
		t.Errorf("unknown priority must be omitted, got %v", m2["priority"])
	}
	// Empty payload serializes as JSON null (still present — required field).
	if v, present := m["payload"]; !present || v != nil {
		t.Errorf("empty payload must be null, got present=%v v=%v", present, v)
	}
}

// TEST-EVT-WIRE-03: priority mapping is deterministic across the internal
// scale (contract enum has no "normal" — medium is its equivalent).
func TestWirePriorityMapping(t *testing.T) {
	cases := map[Priority]string{
		PriorityLow:      "low",
		PriorityNormal:   "medium",
		PriorityHigh:     "high",
		PriorityCritical: "critical",
		Priority(99):     "",
	}
	for p, want := range cases {
		if got := wirePriority(p); got != want {
			t.Errorf("wirePriority(%d) = %q, want %q", p, got, want)
		}
	}
}
