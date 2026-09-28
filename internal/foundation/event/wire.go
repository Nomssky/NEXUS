package event

import (
	"encoding/json"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// WireProducer is the EventProducer (SCHEMA_EVENTS_TRIGGERS §2.3) on the
// external projection.
type WireProducer struct {
	ProducerID   string `json:"producer_id"`
	ProducerType string `json:"producer_type"`
	ModuleName   string `json:"module_name"`
}

// WireRecord projects an in-process Event onto the contract Event Record
// (SCHEMA_EVENTS_TRIGGERS §2.2) for external consumers (the SSE stream today;
// webhook delivery §10 later).
//
// Honest projection — every populated field has a real source:
//   - nexus_id is stamped by the caller from the installation identity
//     (SCHEMA_COMMON §3.2 Universal Required; §2.2 required on events).
//   - schema_version and entity_type come from the common envelope.
//   - occurred_at and emitted_at share the transport timestamp: the
//     in-process bus records one instant per event and there is no separate
//     fact-time to report.
//   - producer maps the transport Source: every emitter on the in-process bus
//     is a NEXUS module (producer_type "module", module_name = Source).
//   - correlation_id is emitted even when empty — §2.2 requires the field;
//     presence is honest, an invented trace id would not be.
//
// §2.2 fields with no source on the transport (event_version, payload_schema,
// provenance, actor, workflow_id, objective_id, parent_event_id,
// classification, deduplication_key, ordering, ttl_seconds, expires_at) are
// intentionally OMITTED rather than fabricated. Consumers must ignore unknown
// fields (SCHEMA_COMMON §9.3) and this projection may grow as sources appear.
type WireRecord struct {
	SchemaVersion  string          `json:"schema_version"`
	EntityType     string          `json:"entity_type"`
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	NexusID        string          `json:"nexus_id"`
	BusinessID     string          `json:"business_id"`
	DivisionID     string          `json:"division_id,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	EmittedAt      time.Time       `json:"emitted_at"`
	Producer       WireProducer    `json:"producer"`
	TaskID         string          `json:"task_id,omitempty"`
	CorrelationID  string          `json:"correlation_id"`
	CausationID    string          `json:"causation_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Priority       string          `json:"priority,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// wirePriority maps the internal numeric priority onto the §2.2 enum
// (critical, high, medium, low, background). PriorityNormal has no contract
// twin — "medium" is its contract equivalent. An out-of-table internal value
// maps to "" (omitted) rather than a wrong enum member.
func wirePriority(p Priority) string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityNormal:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return ""
	}
}

// Wire projects e onto the external Event Record shape, stamping nexusID.
func (e *Event) Wire(nexusID string) WireRecord {
	return WireRecord{
		SchemaVersion: schema.Version,
		EntityType:    "event",
		EventID:       e.ID,
		EventType:     string(e.Type),
		NexusID:       nexusID,
		BusinessID:    e.BusinessID,
		DivisionID:    e.DivisionID,
		OccurredAt:    e.Timestamp,
		EmittedAt:     e.Timestamp,
		Producer: WireProducer{
			ProducerID:   e.Source,
			ProducerType: "module",
			ModuleName:   e.Source,
		},
		TaskID:         e.TaskID,
		CorrelationID:  e.CorrelationID,
		CausationID:    e.CausationID,
		Payload:        json.RawMessage(e.Data),
		Priority:       wirePriority(e.Priority),
		IdempotencyKey: e.IdempotencyKey,
	}
}
