package observability

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// MemRecorder is an in-memory implementation of the Recorder interface.
// It is suitable for testing and development.
type MemRecorder struct {
	mu       sync.RWMutex
	spans    map[string][]*Span // traceID -> spans
	metrics  []Metric
	auditLog []*AuditRecord
	now      func() time.Time
}

// NewMemRecorder creates a new in-memory observability recorder.
func NewMemRecorder() *MemRecorder {
	return &MemRecorder{
		spans:    make(map[string][]*Span),
		metrics:  make([]Metric, 0),
		auditLog: make([]*AuditRecord, 0),
		now:      time.Now,
	}
}

// NewMemRecorderWithClock creates a new in-memory recorder with an injectable clock.
func NewMemRecorderWithClock(now func() time.Time) *MemRecorder {
	return &MemRecorder{
		spans:    make(map[string][]*Span),
		metrics:  make([]Metric, 0),
		auditLog: make([]*AuditRecord, 0),
		now:      now,
	}
}

// StartTrace creates a new root span.
func (r *MemRecorder) StartTrace(operation string) *Span {
	traceID := generateID()
	spanID := generateID()

	span := &Span{
		TraceID:   traceID,
		SpanID:    spanID,
		Operation: operation,
		Service:   "nexus",
		StartTime: r.now(),
		Status:    TraceStatusOK,
	}

	r.mu.Lock()
	r.spans[traceID] = append(r.spans[traceID], span)
	r.mu.Unlock()

	return span
}

// StartSpan creates a child span under the given parent.
func (r *MemRecorder) StartSpan(parent *Span, operation string) *Span {
	spanID := generateID()

	span := &Span{
		TraceID:      parent.TraceID,
		SpanID:       spanID,
		ParentSpanID: parent.SpanID,
		Operation:    operation,
		Service:      "nexus",
		StartTime:    r.now(),
		Status:       TraceStatusOK,
	}

	r.mu.Lock()
	r.spans[parent.TraceID] = append(r.spans[parent.TraceID], span)
	r.mu.Unlock()

	return span
}

// FinishSpan marks a span as complete and records it.
func (r *MemRecorder) FinishSpan(span *Span, status TraceStatus) {
	span.EndTime = r.now()
	span.Status = status
}

// GetTrace returns all spans for a given trace ID.
func (r *MemRecorder) GetTrace(traceID string) []*Span {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spans := r.spans[traceID]
	result := make([]*Span, len(spans))
	copy(result, spans)
	return result
}

// RecordMetric stores a metric data point.
func (r *MemRecorder) RecordMetric(metric Metric) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if metric.Timestamp.IsZero() {
		metric.Timestamp = r.now()
	}
	r.metrics = append(r.metrics, metric)
}

// QueryMetrics returns metrics matching the given name and time range.
func (r *MemRecorder) QueryMetrics(name string, from, to time.Time) []Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Metric
	for _, m := range r.metrics {
		if m.Name != name {
			continue
		}
		if !from.IsZero() && m.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && m.Timestamp.After(to) {
			continue
		}
		result = append(result, m)
	}
	return result
}

// RecordAudit appends an audit entry.
func (r *MemRecorder) RecordAudit(record *AuditRecord) error {
	if record == nil {
		return &ObservabilityError{Code: "INVALID_RECORD", Message: "audit record is nil"}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if record.ID == "" {
		record.ID = generateID()
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = r.now()
	}

	r.auditLog = append(r.auditLog, record)
	return nil
}

// QueryAudit returns audit records matching the given correlation ID.
func (r *MemRecorder) QueryAudit(correlationID string) ([]*AuditRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*AuditRecord
	for _, record := range r.auditLog {
		if correlationID != "" && record.CorrelationID != correlationID {
			continue
		}
		result = append(result, record)
	}
	return result, nil
}

// AuditCount returns the number of audit records.
func (r *MemRecorder) AuditCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.auditLog)
}

// MetricCount returns the number of metrics.
func (r *MemRecorder) MetricCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.metrics)
}

// generateID creates a random 16-byte hex string.
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
