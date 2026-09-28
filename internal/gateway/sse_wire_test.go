package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// TEST-GW-SSE-WIRE-01: SSE frames are the external event boundary — they carry
// the contract Event Record projection (SCHEMA_EVENTS_TRIGGERS §2.2, honest
// projection): nexus_id stamped from the installation identity, schema_version
// + entity_type envelope, event_id, producer, and the payload as raw JSON;
// unsourced §2.2 fields (payload_schema) must be absent, never fabricated.
func TestSSEWireEnvelopeProjection(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0", WithNexusID("nx:nexus:wire-test"))
	sse := startSSE(t, srv)

	publishDispatch(t, engine, &event.Event{
		ID:            "wire-1",
		Type:          event.EventType("custom"),
		Priority:      event.PriorityHigh,
		CorrelationID: "corr-wire",
		Data:          []byte(`{"x":1}`),
	})
	time.Sleep(100 * time.Millisecond)
	body := sse.finish(t)

	for _, want := range []string{
		`"nexus_id":"nx:nexus:wire-test"`,
		`"schema_version":"1.0.0"`,
		`"entity_type":"event"`,
		`"event_id":"wire-1"`,
		`"event_type":"custom"`,
		`"business_id":"biz-1"`,
		`"producer":{"producer_id":"g010-test","producer_type":"module","module_name":"g010-test"}`,
		`"correlation_id":"corr-wire"`,
		`"payload":{"x":1}`,
		`"priority":"high"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE wire frame missing %s; body=%s", want, body)
		}
	}
	// Unsourced §2.2 fields are omitted, not fabricated.
	for _, absent := range []string{"payload_schema", "provenance", "event_version"} {
		if strings.Contains(body, `"`+absent+`"`) {
			t.Errorf("SSE wire frame must omit unsourced field %s", absent)
		}
	}
	// The internal transport shape (raw event.Event) must not leak: no
	// internal "id"/"data" keys on the wire.
	if strings.Contains(body, `"data":`) {
		t.Errorf("internal transport field data must not appear on the wire; body=%s", body)
	}
}
