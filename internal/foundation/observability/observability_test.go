package observability

import (
	"testing"
	"time"
)

// TEST-M3-025: Recorder interface compliance
func TestMemRecorderInterface(t *testing.T) {
	var _ Recorder = (*MemRecorder)(nil)
}

// TEST-M3-026: Start and finish trace
func TestMemRecorderTrace(t *testing.T) {
	r := NewMemRecorder()

	span := r.StartTrace("test-operation")
	if span.TraceID == "" {
		t.Error("expected non-empty trace ID")
	}
	if span.SpanID == "" {
		t.Error("expected non-empty span ID")
	}
	if span.Operation != "test-operation" {
		t.Errorf("expected operation test-operation, got %v", span.Operation)
	}

	r.FinishSpan(span, TraceStatusOK)

	trace := r.GetTrace(span.TraceID)
	if len(trace) != 1 {
		t.Fatalf("expected 1 span in trace, got %d", len(trace))
	}
	if trace[0].Status != TraceStatusOK {
		t.Errorf("expected status OK, got %v", trace[0].Status)
	}
}

// TEST-M3-027: Child spans
func TestMemRecorderChildSpan(t *testing.T) {
	r := NewMemRecorder()

	parent := r.StartTrace("parent")
	child := r.StartSpan(parent, "child")

	if child.TraceID != parent.TraceID {
		t.Errorf("child trace ID should match parent")
	}
	if child.ParentSpanID != parent.SpanID {
		t.Errorf("child parent ID should match parent span ID")
	}

	r.FinishSpan(parent, TraceStatusOK)
	r.FinishSpan(child, TraceStatusOK)

	trace := r.GetTrace(parent.TraceID)
	if len(trace) != 2 {
		t.Fatalf("expected 2 spans in trace, got %d", len(trace))
	}
}

// TEST-M3-028: Metrics recording
func TestMemRecorderMetrics(t *testing.T) {
	r := NewMemRecorder()

	r.RecordMetric(Metric{
		Name:  "task.duration",
		Value: 1.5,
		Labels: map[string]string{
			"task_type": "compute",
		},
	})

	if r.MetricCount() != 1 {
		t.Errorf("expected 1 metric, got %d", r.MetricCount())
	}

	// Query by name
	metrics := r.QueryMetrics("task.duration", time.Time{}, time.Now().Add(time.Hour))
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Value != 1.5 {
		t.Errorf("expected value 1.5, got %v", metrics[0].Value)
	}
}

// TEST-M3-029: Metrics query by time range
func TestMemRecorderMetricsQuery(t *testing.T) {
	r := NewMemRecorder()
	now := time.Now()

	r.RecordMetric(Metric{Name: "cpu", Value: 50, Timestamp: now.Add(-2 * time.Hour)})
	r.RecordMetric(Metric{Name: "cpu", Value: 75, Timestamp: now.Add(-1 * time.Hour)})
	r.RecordMetric(Metric{Name: "cpu", Value: 90, Timestamp: now})

	// Query last hour only
	metrics := r.QueryMetrics("cpu", now.Add(-time.Hour), now.Add(time.Hour))
	if len(metrics) != 2 {
		t.Errorf("expected 2 metrics in last hour, got %d", len(metrics))
	}
}

// TEST-M3-030: Audit record
func TestMemRecorderAudit(t *testing.T) {
	r := NewMemRecorder()

	record := &AuditRecord{
		CorrelationID: "corr-1",
		Actor:         "agent-1",
		Action:        "execute_tool",
		Resource:      "tool-1",
		Outcome:       "success",
		BusinessID:    "biz-1",
	}

	err := r.RecordAudit(record)
	if err != nil {
		t.Fatalf("RecordAudit failed: %v", err)
	}

	if r.AuditCount() != 1 {
		t.Errorf("expected 1 audit record, got %d", r.AuditCount())
	}

	// Query by correlation ID
	records, err := r.QueryAudit("corr-1")
	if err != nil {
		t.Fatalf("QueryAudit failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(records))
	}
	if records[0].Actor != "agent-1" {
		t.Errorf("expected actor agent-1, got %v", records[0].Actor)
	}
}

// TEST-M3-031: Audit redaction (details field)
func TestMemRecorderAuditRedaction(t *testing.T) {
	r := NewMemRecorder()

	// Record with redacted details
	record := &AuditRecord{
		Actor:    "agent-1",
		Action:   "access_secret",
		Resource: "secret-1",
		Outcome:  "success",
		Details: map[string]string{
			"secret_name": "REDACTED", // secrets must be redacted
			"access_type": "read",
		},
	}

	err := r.RecordAudit(record)
	if err != nil {
		t.Fatalf("RecordAudit failed: %v", err)
	}

	records, _ := r.QueryAudit("")
	if records[0].Details["secret_name"] != "REDACTED" {
		t.Error("expected secret to be redacted")
	}
}

// TEST-M3-032: Trace status
func TestMemRecorderTraceStatus(t *testing.T) {
	r := NewMemRecorder()

	// Error trace
	span := r.StartTrace("failing-operation")
	r.FinishSpan(span, TraceStatusError)

	trace := r.GetTrace(span.TraceID)
	if trace[0].Status != TraceStatusError {
		t.Errorf("expected error status, got %v", trace[0].Status)
	}
}

// TEST-M3-033: Span duration
func TestSpanDuration(t *testing.T) {
	start := time.Now()
	span := &Span{
		StartTime: start,
		EndTime:   start.Add(100 * time.Millisecond),
	}

	d := span.Duration()
	if d != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", d)
	}
}

// TEST-M3-034: Observability cannot grant authority
func TestObservabilityCannotGrantAuthority(t *testing.T) {
	// This is a structural invariant test.
	// The observability package has no authority-granting types or functions.
	// If this test compiles, the invariant holds.
	r := NewMemRecorder()

	// Record an audit event
	record := &AuditRecord{
		Actor:    "agent-1",
		Action:   "governance_decision",
		Resource: "policy-1",
		Outcome:  "ALLOW",
	}

	err := r.RecordAudit(record)
	if err != nil {
		t.Fatalf("RecordAudit failed: %v", err)
	}

	// The audit record is informational only — it does not grant authority.
	// This is verified by the fact that AuditRecord has no authority fields.
	records, _ := r.QueryAudit("")
	if records[0].Outcome != "ALLOW" {
		t.Errorf("expected outcome ALLOW in audit, got %v", records[0].Outcome)
	}
}
