// Package observability implements the NEXUS observability and audit subsystem (C07).
//
// C07 owns structured logs, metrics, traces, audit, decision traces,
// correlation and causation IDs, health, alerts, and incident timeline.
//
// C07 is cross-cutting: callable from every layer; depends on C01, C05.
// C07 is forbidden from granting authority, leaking secrets, or dropping
// critical events. Observability cannot grant authority.
//
// Correlation chain:
//
//	Event -> Workflow -> Task -> Agent -> Model -> Tool ->
//	Result -> Verification -> State Change
//
// Must reconstruct:
//
//	REQUEST -> DECISION -> WORKFLOW -> TASK -> AGENT -> MODEL -> TOOL ->
//	EXTERNAL OPERATION -> VERIFICATION -> OUTCOME
//
// Audit is append-only/tamper-resistant; secrets redacted before storage;
// critical events never dropped by sampling; replay never causes side effects.
package observability

import (
	"fmt"
	"time"
)

// Level represents the severity level of an observation.
type Level string

const (
	LevelDebug    Level = "debug"
	LevelInfo     Level = "info"
	LevelWarn     Level = "warn"
	LevelError    Level = "error"
	LevelCritical Level = "critical"
)

// TraceStatus represents the outcome of a traced operation.
type TraceStatus string

const (
	TraceStatusOK       TraceStatus = "ok"
	TraceStatusError    TraceStatus = "error"
	TraceStatusCanceled TraceStatus = "canceled"
)

// Span represents a single unit of work in a distributed trace.
type Span struct {
	// TraceID is the unique identifier for the entire trace.
	TraceID string `json:"trace_id"`
	// SpanID is the unique identifier for this span.
	SpanID string `json:"span_id"`
	// ParentSpanID is the parent span (empty for root span).
	ParentSpanID string `json:"parent_span_id,omitempty"`
	// Operation is the name of the operation being traced.
	Operation string `json:"operation"`
	// Service identifies which component generated this span.
	Service string `json:"service"`
	// StartTime is when the span began.
	StartTime time.Time `json:"start_time"`
	// EndTime is when the span ended (zero if still active).
	EndTime time.Time `json:"end_time,omitempty"`
	// Status is the outcome of the traced operation.
	Status TraceStatus `json:"status"`
	// Attributes are key-value pairs providing additional context.
	Attributes map[string]string `json:"attributes,omitempty"`
	// Events are notable occurrences within the span.
	Events []SpanEvent `json:"events,omitempty"`
}

// SpanEvent represents a notable occurrence within a span.
type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Duration returns the span duration (EndTime - StartTime).
func (s *Span) Duration() time.Duration {
	if s.EndTime.IsZero() {
		return 0
	}
	return s.EndTime.Sub(s.StartTime)
}

// Finish marks the span as complete.
func (s *Span) Finish(status TraceStatus) {
	s.EndTime = time.Now()
	s.Status = status
}

// Metric represents a numerical measurement.
type Metric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// AuditRecord represents an immutable audit entry.
// Audit is append-only and tamper-resistant. Secrets are redacted.
type AuditRecord struct {
	// ID is the unique audit record identifier.
	ID string `json:"id"`
	// Timestamp is when the audit event occurred.
	Timestamp time.Time `json:"timestamp"`
	// CorrelationID links to the correlation chain.
	CorrelationID string `json:"correlation_id,omitempty"`
	// Actor is who performed the action.
	Actor string `json:"actor"`
	// Action is what was performed.
	Action string `json:"action"`
	// Resource is what was acted upon.
	Resource string `json:"resource"`
	// Outcome is the result of the action.
	Outcome string `json:"outcome"`
	// BusinessID scopes the audit record.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID scopes the audit record.
	DivisionID string `json:"division_id,omitempty"`
	// Details are additional context (secrets must be redacted).
	Details map[string]string `json:"details,omitempty"`
	// Classification is the sensitivity level.
	Classification string `json:"classification,omitempty"`
}

// Tracer manages distributed tracing.
type Tracer interface {
	// StartTrace creates a new root span.
	StartTrace(operation string) *Span
	// StartSpan creates a child span under the given parent.
	StartSpan(parent *Span, operation string) *Span
	// FinishSpan marks a span as complete and records it.
	FinishSpan(span *Span, status TraceStatus)
	// GetTrace returns all spans for a given trace ID.
	GetTrace(traceID string) []*Span
}

// MetricsCollector collects and stores metrics.
type MetricsCollector interface {
	// RecordMetric stores a metric data point.
	RecordMetric(metric Metric)
	// QueryMetrics returns metrics matching the given name and time range.
	QueryMetrics(name string, from, to time.Time) []Metric
}

// AuditLog records immutable audit entries.
type AuditLog interface {
	// RecordAudit appends an audit entry.
	RecordAudit(record *AuditRecord) error
	// QueryAudit returns audit records matching the given criteria.
	QueryAudit(correlationID string) ([]*AuditRecord, error)
}

// Recorder is the top-level observability interface.
// It combines tracing, metrics, and audit into a single entry point.
type Recorder interface {
	Tracer
	MetricsCollector
	AuditLog
}

// ObservabilityError represents an observability-specific error.
type ObservabilityError struct {
	Code    string
	Message string
}

func (e *ObservabilityError) Error() string {
	return fmt.Sprintf("observability error [%s]: %s", e.Code, e.Message)
}
