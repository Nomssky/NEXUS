// Package event implements the NEXUS event and trigger substrate (C06).
//
// C06 owns event ingestion, append-only persistence, priority queues,
// worker pool, dedup/idempotency, DLQ/quarantine, safe replay, and
// declarative triggers.
//
// Events are immutable facts, never commands. The event substrate is
// forbidden from treating events as commands or generating external
// side effects on replay.
//
// Delivery semantics: at_most_once, at_least_once, effectively_once
// (requires idempotency key). No unsupported exactly-once claims.
package event

import (
	"fmt"
	"time"
)

// EventType classifies what kind of event occurred.
type EventType string

const (
	EventTypeWorkflowStarted   EventType = "workflow.started"
	EventTypeWorkflowCompleted EventType = "workflow.completed"
	EventTypeWorkflowFailed    EventType = "workflow.failed"
	EventTypeTaskCreated       EventType = "task.created"
	EventTypeTaskAssigned      EventType = "task.assigned"
	EventTypeTaskCompleted     EventType = "task.completed"
	EventTypeTaskFailed        EventType = "task.failed"
	EventTypeTaskCancelled     EventType = "task.cancelled"
	EventTypeAgentSpawned      EventType = "agent.spawned"
	EventTypeAgentHeartbeat    EventType = "agent.heartbeat"
	EventTypeAgentStopped      EventType = "agent.stopped"
	EventTypeModelInvoked      EventType = "model.invoked"
	EventTypeModelCompleted    EventType = "model.completed"
	EventTypeToolInvoked       EventType = "tool.invoked"
	EventTypeToolCompleted     EventType = "tool.completed"
	EventTypeGovernanceDecided EventType = "governance.decided"
	EventTypeApprovalRequested EventType = "approval.requested"
	EventTypeApprovalResolved  EventType = "approval.resolved"
	EventTypeSecurityViolation EventType = "security.violation"
	EventTypeCustom            EventType = "custom"
)

// Priority represents the priority of an event in the queue.
type Priority int

const (
	PriorityLow      Priority = 0
	PriorityNormal   Priority = 5
	PriorityHigh     Priority = 10
	PriorityCritical Priority = 15
)

// DeliveryMode specifies how events are delivered to consumers.
type DeliveryMode string

const (
	DeliveryAtMostOnce      DeliveryMode = "at_most_once"
	DeliveryAtLeastOnce     DeliveryMode = "at_least_once"
	DeliveryEffectivelyOnce DeliveryMode = "effectively_once"
)

// Event is an immutable fact that occurred in the system.
// Events are never commands — they record what happened, not what should happen.
type Event struct {
	// ID is the unique event identifier.
	ID string `json:"id"`
	// Type classifies the event.
	Type EventType `json:"type"`
	// Source identifies what generated the event.
	Source string `json:"source"`
	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`
	// BusinessID scopes the event to a business.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID scopes the event to a division.
	DivisionID string `json:"division_id,omitempty"`
	// CorrelationID links events across a correlation chain.
	CorrelationID string `json:"correlation_id,omitempty"`
	// CausationID links to the event/action that caused this event.
	CausationID string `json:"causation_id,omitempty"`
	// IdempotencyKey prevents duplicate processing for at_least_once delivery.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// Priority determines queue ordering.
	Priority Priority `json:"priority"`
	// Data is the event payload (immutable after creation).
	Data []byte `json:"data,omitempty"`
}

// Consumer processes events from the event bus.
type Consumer interface {
	// Handle processes a single event. Returns an error if processing fails.
	Handle(event *Event) error
}

// ConsumerFunc adapts a function to the Consumer interface.
type ConsumerFunc func(event *Event) error

func (f ConsumerFunc) Handle(event *Event) error {
	return f(event)
}

// Bus is the event bus interface.
// It provides event ingestion, subscription, and delivery.
type Bus interface {
	// Publish publishes an event to the bus.
	Publish(event *Event) error
	// Subscribe registers a consumer for events matching the given types.
	// Returns a subscription ID that can be used to unsubscribe.
	Subscribe(consumer Consumer, eventTypes ...EventType) (string, error)
	// Unsubscribe removes a consumer by subscription ID.
	Unsubscribe(subscriptionID string) error
}

// Queue is a priority queue for events.
type Queue interface {
	// Enqueue adds an event to the queue.
	Enqueue(event *Event) error
	// Dequeue removes and returns the highest-priority event.
	Dequeue() (*Event, error)
	// Size returns the number of events in the queue.
	Size() int
}

// DeliveryError represents an event delivery failure.
type DeliveryError struct {
	EventID string
	Code    string
	Message string
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("delivery error [%s] for event %s: %s", e.Code, e.EventID, e.Message)
}
