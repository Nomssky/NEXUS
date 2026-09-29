package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// envelopeServer builds an engine + server and exercises the production
// handler chain (identity + auth middleware + mux).
func envelopeServer(t *testing.T) *Server {
	t.Helper()
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return NewServer(engine, ":0")
}

// TEST-GW-ENVELOPE-01: every gateway error body must satisfy
// contracts/CORE_INTERFACE_CONTRACTS.md §3 — machine-readable code, closed
// category enum, message, category-default retryable, correlation_id, and
// timestamp — with correlation echoed on the response header. Regression for
// the F1 defect where writeError emitted {code: HTTP-status-string,
// category: free-form token} without correlation_id/timestamp and derived
// retryable from the status code.
func TestErrorEnvelopeContractConformance(t *testing.T) {
	srv := envelopeServer(t)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing business_id", http.MethodPost, "/api/v1/requests", `{"intent":"x"}`, http.StatusBadRequest, "VALIDATION"},
		{"unknown request", http.MethodGet, "/api/v1/requests/req-nope?business_id=biz-1", "", http.StatusNotFound, "VALIDATION"},
		{"control without api key", http.MethodGet, "/api/v1/control/status", "", http.StatusForbidden, "CONTROL_DISABLED"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status: want %d, got %d body=%s", tc.wantStatus, w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("content type: %q", ct)
			}

			var payload struct {
				Error struct {
					Code          string         `json:"code"`
					Category      string         `json:"category"`
					Message       string         `json:"message"`
					Retryable     *bool          `json:"retryable"`
					CorrelationID string         `json:"correlation_id"`
					Timestamp     string         `json:"timestamp"`
					Details       map[string]any `json:"details"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
				t.Fatalf("decode: %v body=%s", err, w.Body.String())
			}
			e := payload.Error

			if e.Code == "" {
				t.Error("missing machine-readable code")
			}
			if e.Code != tc.wantCode {
				t.Errorf("code: want %q, got %q", tc.wantCode, e.Code)
			}
			if !nerrors.Category(nerrors.Category(e.Category)).IsValid() {
				t.Errorf("category %q not in the contract §3 closed enum", e.Category)
			}
			if e.Message == "" {
				t.Error("missing message")
			}
			if e.Retryable == nil {
				t.Error("missing retryable")
			}
			if e.CorrelationID == "" {
				t.Error("missing correlation_id")
			}
			if e.Timestamp == "" {
				t.Error("missing timestamp")
			} else if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
				t.Errorf("timestamp not RFC3339: %q", e.Timestamp)
			}
			if h := w.Header().Get("X-Correlation-ID"); h == "" {
				t.Error("missing X-Correlation-ID response header")
			}
		})
	}
}

// TEST-GW-ENVELOPE-02: retryable follows the contract's per-category table
// (VALIDATION → false), not the HTTP status code.
func TestErrorEnvelopeRetryableFollowsCategory(t *testing.T) {
	srv := envelopeServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", strings.NewReader(`{"intent":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var payload struct {
		Error struct {
			Category  string `json:"category"`
			Retryable *bool  `json:"retryable"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Error.Retryable == nil {
		t.Fatal("missing retryable")
	}
	if *payload.Error.Retryable {
		t.Errorf("VALIDATION must be non-retryable, got true")
	}
}

// TEST-GW-ENVELOPE-05 (F10): unknown routes must answer in the §3 envelope,
// not the ServeMux default text/plain "404 page not found" body.
func TestUnknownRouteErrorEnvelope(t *testing.T) {
	srv := envelopeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/definitely-not-a-route", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type: want application/json, got %q body=%s", ct, w.Body.String())
	}
	var payload struct {
		Error struct {
			Code          string `json:"code"`
			Category      string `json:"category"`
			Message       string `json:"message"`
			CorrelationID string `json:"correlation_id"`
			Timestamp     string `json:"timestamp"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not an error envelope: %v: %s", err, w.Body.String())
	}
	if payload.Error.Code == "" || payload.Error.Category == "" || payload.Error.Message == "" {
		t.Errorf("envelope incomplete: %+v", payload.Error)
	}
	if w.Header().Get("X-Correlation-ID") == "" {
		t.Error("missing X-Correlation-ID header")
	}
}

// TEST-GW-ENVELOPE-06 (F10): a method mismatch on a known route must also
// answer in the envelope (was the mux's plain-text "Method Not Allowed").
func TestMethodNotAllowedErrorEnvelope(t *testing.T) {
	srv := envelopeServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type: want application/json, got %q body=%s", ct, w.Body.String())
	}
	var payload struct {
		Error struct {
			Code     string `json:"code"`
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not an error envelope: %v: %s", err, w.Body.String())
	}
	if payload.Error.Code == "" {
		t.Errorf("missing code: %s", w.Body.String())
	}
	if allow := w.Header().Get("Allow"); allow == "" {
		t.Error("405 must keep the Allow header from the mux")
	}
}
