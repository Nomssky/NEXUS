package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TEST-M0-005: structured logging emits the canonical fields.
func TestStructuredLoggingJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{
		Out:      &buf,
		MinLevel: LevelDebug,
		Format:   "json",
		Service:  "nexus.test",
		NexusID:  "nx:nexus:test",
	})

	log.Info("hello", Fields{CorrelationID: "corr-123"})

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"timestamp", "level", "service_name", "message", "nexus_id", "correlation_id"} {
		if _, ok := rec[key]; !ok {
			t.Fatalf("expected field %q in log record: %s", key, buf.String())
		}
	}
	if rec["level"] != "info" {
		t.Fatalf("expected info level, got %v", rec["level"])
	}
	if rec["correlation_id"] != "corr-123" {
		t.Fatalf("expected correlation id to propagate, got %v", rec["correlation_id"])
	}
}

// TEST-M0-005 (level filter): debug suppressed at info level.
func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Out: &buf, MinLevel: LevelInfo, Format: "json"})
	log.Debug("should not appear", Fields{})
	if buf.Len() != 0 {
		t.Fatalf("debug should be filtered at info level, got: %s", buf.String())
	}
}

// TEST-M0-004: secrets are never exposed in logs.
func TestSecretRedaction(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Out: &buf, MinLevel: LevelDebug, Format: "json"})

	log.Info("config loaded", Fields{
		Context: map[string]any{
			"api_key":       "sk-live-supersecret",
			"password":      "hunter2",
			"authorization": "Bearer abc.def",
			"client_secret": "cs_123",
			"private_key":   "-----BEGIN KEY-----",
			"safe_setting":  "visible",
			"nested":        map[string]any{"token": "tok-xyz", "ok": true},
		},
	})

	out := buf.String()
	for _, leak := range []string{"sk-live-supersecret", "hunter2", "abc.def", "cs_123", "tok-xyz"} {
		if strings.Contains(out, leak) {
			t.Fatalf("secret %q leaked into logs: %s", leak, out)
		}
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("non-secret values must remain visible: %s", out)
	}
	if !strings.Contains(out, RedactedValue()) {
		t.Fatalf("expected redaction marker in output: %s", out)
	}
}

// TEST-M0-005 (text format): text format also emits structured markers.
func TestStructuredLoggingText(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Out: &buf, MinLevel: LevelDebug, Format: "text", Service: "nexus.test"})
	log.Warn("careful", Fields{CorrelationID: "c1"})
	out := buf.String()
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "careful") || !strings.Contains(out, "correlation_id=c1") {
		t.Fatalf("unexpected text log: %s", out)
	}
}
