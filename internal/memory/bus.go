package memory

// Adapter from the memory platform onto the EXISTING event bus
// (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md §16). This package creates no bus
// of its own: the launcher injects the same `event.MemBus` every other component
// publishes to, and the payload is metadata only — ids, scope, type, provenance,
// counts and outcomes. Memory content and reasoning never appear in an event.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// BusSink publishes memory events onto the existing bus.
type BusSink struct {
	Bus *event.MemBus
}

// NewBusSink wraps the existing bus.
func NewBusSink(bus *event.MemBus) *BusSink { return &BusSink{Bus: bus} }

// Publish implements EventSink.
func (s *BusSink) Publish(e Event) error {
	if s == nil || s.Bus == nil {
		return nil
	}
	payload, err := json.Marshal(e.Fields)
	if err != nil {
		return err
	}
	correlation := e.CorrelationID
	if correlation == "" {
		correlation = e.BusinessID
	}
	return s.Bus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s-%d", e.BusinessID, e.Type, time.Now().UnixNano()),
		Type:          event.EventType(e.Type),
		Source:        "memory",
		Timestamp:     time.Now(),
		BusinessID:    e.BusinessID,
		DivisionID:    e.DivisionID,
		CorrelationID: correlation,
		Priority:      event.PriorityNormal,
		Data:          payload,
	})
}