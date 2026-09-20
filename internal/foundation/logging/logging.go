// Package logging implements the M0 structured logging foundation (contributes
// to component C07 Observability, layer L2, but is bootstrapped at L0 because
// every component needs it from the first run).
//
// Scope note: this is a deliberately minimal foundation, NOT the full
// Observability / Audit / Telemetry system (Core/NEXUS_OBSERVABILITY_AUDIT_TELEMETRY.md,
// contracts/SCHEMA_OBSERVABILITY_CONFIG.md §2). It emits structured records whose
// field names match the contract Log Record so the foundation can evolve without
// a wire change.
//
// Invariants preserved:
//   - Observability grants no authority (INV: Observability ≠ Authority).
//   - Logs must never contain secrets or sensitive data.
//   - Workflow/task/agent IDs are NOT fabricated; they are only emitted when
//     supplied by a caller operating in a milestone where they exist.
//
// Deferred to later milestones (recorded, not implemented): metrics, traces
// (trace_id/span_id), audit store, tamper resistance, alerting, tiered retention.
package logging

import (
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level is the canonical log level (contract: debug/info/warn/error/fatal).
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelFatal Level = "fatal"
)

var levelOrder = map[Level]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
	LevelFatal: 4,
}

// ParseLevel converts a string to a Level, defaulting to info for unknown input
// (the caller is responsible for config validation of the configured level).
func ParseLevel(s string) Level {
	l := Level(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := levelOrder[l]; ok {
		return l
	}
	return LevelInfo
}

// redactedValue is the fixed marker used when a value is redacted.
const redactedValue = "[REDACTED]"

// secretKeyFragments are case-insensitive substrings that mark a field as
// secret-bearing. Any record field whose key contains one of these is redacted.
var secretKeyFragments = []string{
	"secret", "password", "passwd", "token", "apikey", "api_key",
	"private_key", "privatekey", "credential", "authorization",
	"bearer", "cookie", "session", "client_secret",
}

// Logger emits structured records.
type Logger struct {
	mu         sync.Mutex
	out        io.Writer
	minLevel   Level
	format     string
	service    string
	nexusID    string
	businessID string
	now        func() time.Time
}

// Options configures a Logger.
type Options struct {
	Out        io.Writer
	MinLevel   Level
	Format     string // "json" or "text"
	Service    string
	NexusID    string
	BusinessID string
}

// New constructs a Logger.
func New(opts Options) *Logger {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	format := opts.Format
	if format != "text" {
		format = "json"
	}
	return &Logger{
		out:        out,
		minLevel:   opts.MinLevel,
		format:     format,
		service:    opts.Service,
		nexusID:    opts.NexusID,
		businessID: opts.BusinessID,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// record is the structured log line. Field names match the contract Log Record.
// lifecycle/objective/workflow/task/agent IDs are populated only when set.
type record struct {
	LogID         string         `json:"log_id,omitempty"`
	Timestamp     string         `json:"timestamp"`
	Level         Level          `json:"level"`
	ServiceName   string         `json:"service_name"`
	Message       string         `json:"message"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	TraceID       string         `json:"trace_id,omitempty"`
	NexusID       string         `json:"nexus_id,omitempty"`
	BusinessID    string         `json:"business_id,omitempty"`
	ObjectiveID   string         `json:"objective_id,omitempty"`
	WorkflowID    string         `json:"workflow_id,omitempty"`
	TaskID        string         `json:"task_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	Context       map[string]any `json:"context,omitempty"`
}

// Fields carries optional structured context for a single event.
type Fields struct {
	CorrelationID string
	TraceID       string
	ObjectiveID   string
	WorkflowID    string
	TaskID        string
	AgentID       string
	Context       map[string]any
}

// log emits a record if it meets the minimum level.
func (l *Logger) log(level Level, msg string, f Fields) {
	if levelOrder[level] < levelOrder[l.minLevel] {
		return
	}
	rec := record{
		Timestamp:     l.now().Format(time.RFC3339Nano),
		Level:         level,
		ServiceName:   l.service,
		Message:       msg,
		CorrelationID: f.CorrelationID,
		TraceID:       f.TraceID,
		NexusID:       l.nexusID,
		BusinessID:    l.businessID,
		ObjectiveID:   f.ObjectiveID,
		WorkflowID:    f.WorkflowID,
		TaskID:        f.TaskID,
		AgentID:       f.AgentID,
	}
	if len(f.Context) > 0 {
		rec.Context = redactMap(f.Context)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.format == "text" {
		l.writeText(rec)
		return
	}
	l.writeJSON(rec)
}

func (l *Logger) writeJSON(rec record) {
	enc := json.NewEncoder(l.out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rec)
}

func (l *Logger) writeText(rec record) {
	var b strings.Builder
	b.WriteString(rec.Timestamp)
	b.WriteString(" ")
	b.WriteString(strings.ToUpper(string(rec.Level)))
	b.WriteString(" ")
	b.WriteString(rec.ServiceName)
	b.WriteString(" ")
	b.WriteString(rec.Message)
	if rec.CorrelationID != "" {
		b.WriteString(" correlation_id=")
		b.WriteString(rec.CorrelationID)
	}
	if len(rec.Context) > 0 {
		ks := make([]string, 0, len(rec.Context))
		for k := range rec.Context {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			b.WriteString(" ")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmtAny(rec.Context[k]))
		}
	}
	b.WriteString("\n")
	_, _ = io.WriteString(l.out, b.String())
}

func fmtAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return "[unencodable]"
		}
		return string(data)
	}
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string, f Fields) { l.log(LevelDebug, msg, f) }

// Info logs at info level.
func (l *Logger) Info(msg string, f Fields) { l.log(LevelInfo, msg, f) }

// Warn logs at warn level.
func (l *Logger) Warn(msg string, f Fields) { l.log(LevelWarn, msg, f) }

// Error logs at error level.
func (l *Logger) Error(msg string, f Fields) { l.log(LevelError, msg, f) }

// Fatal logs at fatal level. It does not exit the process; the lifecycle owner
// decides termination so that shutdown bookkeeping stays in one place.
func (l *Logger) Fatal(msg string, f Fields) { l.log(LevelFatal, msg, f) }

// redactMap returns a copy of m with secret-looking keys redacted, recursing
// into nested maps. Values are never inspected for content; redaction is by key.
func redactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSecretKey(k) {
			out[k] = redactedValue
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return redactMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactValue(e)
		}
		return out
	default:
		return v
	}
}

func isSecretKey(k string) bool {
	lower := strings.ToLower(k)
	for _, frag := range secretKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// RedactedValue exposes the redaction marker for tests and callers.
func RedactedValue() string { return redactedValue }
