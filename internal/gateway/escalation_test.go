package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// escalationGateway builds a started engine with one queued escalation per
// business scope (via the governance.escalated intake) plus a gateway.
func escalationGateway(t *testing.T, opts ...ServerOption) (*core.Engine, *Server) {
	t.Helper()
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })
	queueEscalation(t, engine, "esc-gw-1", "biz-1")
	queueEscalation(t, engine, "esc-gw-2", "biz-2")
	return engine, NewServer(engine, ":0", opts...)
}

// queueEscalation publishes one governance.escalated intake event for the
// given business scope and dispatches it.
func queueEscalation(t *testing.T, engine *core.Engine, id, businessID string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"escalation_ref": id,
		"reason":         "needs a human",
		"gate":           "chain",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := engine.EventBus().Publish(&event.Event{
		ID:         "ev-" + id,
		Type:       event.EventTypeGovernanceEscalated,
		Source:     "test",
		Timestamp:  time.Now(),
		BusinessID: businessID,
		Data:       data,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := engine.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

// getEscalations issues GET /api/v1/escalations.
func getEscalations(srv *Server, businessID string) *httptest.ResponseRecorder {
	url := "/api/v1/escalations"
	if businessID != "" {
		url += "?business_id=" + businessID
	}
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// postEscalationDecision issues POST /api/v1/escalations/{id}/{ack|resolve}.
func postEscalationDecision(srv *Server, id, businessID, decision, reasoning string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"reasoning": reasoning})
	req := httptest.NewRequest("POST",
		"/api/v1/escalations/"+id+"/"+decision+"?business_id="+businessID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// TEST-GW-ESC-01 (CTR-ATT-001): list is fail-closed on scope and serves only
// the caller's business records.
func TestEscalationListScoped(t *testing.T) {
	_, srv := escalationGateway(t)

	if w := getEscalations(srv, ""); w.Code != http.StatusBadRequest {
		t.Errorf("missing business_id: expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	w := getEscalations(srv, "biz-1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Escalations []core.Escalation `json:"escalations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Escalations) != 1 {
		t.Fatalf("biz-1 escalations: want 1, got %d", len(resp.Escalations))
	}
	esc := resp.Escalations[0]
	if esc.EscalationID != "esc-gw-1" || esc.BusinessID != "biz-1" {
		t.Errorf("want esc-gw-1/biz-1, got %s/%s", esc.EscalationID, esc.BusinessID)
	}
	if esc.Reason == "" || esc.Deadline.IsZero() || esc.Status != core.EscalationPending {
		t.Errorf("alert shape: reason=%q deadline=%v status=%s", esc.Reason, esc.Deadline, esc.Status)
	}
}

// TEST-GW-ESC-02 (CTR-ATT-002): acknowledge — reasoning required, unknown id
// 404, success answers {accepted} and attributes the response on the record.
func TestEscalationAcknowledgeEndpoint(t *testing.T) {
	engine, srv := escalationGateway(t)

	if w := postEscalationDecision(srv, "esc-gw-1", "biz-1", "ack", ""); w.Code != http.StatusBadRequest {
		t.Errorf("missing reasoning: expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if w := postEscalationDecision(srv, "esc-missing", "biz-1", "ack", "seen"); w.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d body=%s", w.Code, w.Body.String())
	}

	w := postEscalationDecision(srv, "esc-gw-1", "biz-1", "ack", "on it")
	if w.Code != http.StatusOK {
		t.Fatalf("ack: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		EscalationID string `json:"escalation_id"`
		Status       string `json:"status"`
		Accepted     bool   `json:"accepted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Accepted || resp.Status != "acknowledged" || resp.EscalationID != "esc-gw-1" {
		t.Errorf("CTR-ATT-002 output: got %+v", resp)
	}
	esc, ok := engine.GetEscalation("esc-gw-1")
	if !ok || esc.AckReason != "on it" || esc.AcknowledgedBy == "" {
		t.Errorf("record attribution: ack_reason=%q ack_by=%q", esc.AckReason, esc.AcknowledgedBy)
	}

	// Non-decidable state (already acknowledged) → 409.
	if w := postEscalationDecision(srv, "esc-gw-1", "biz-1", "ack", "again"); w.Code != http.StatusConflict {
		t.Errorf("double ack: expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// TEST-GW-ESC-03 (CTR-ATT-002): resolve records decision + reasoning and is
// terminal — a second resolve is a conflict.
func TestEscalationResolveEndpoint(t *testing.T) {
	engine, srv := escalationGateway(t)

	w := postEscalationDecision(srv, "esc-gw-1", "biz-1", "resolve", "policy approved for this run")
	if w.Code != http.StatusOK {
		t.Fatalf("resolve: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status   string `json:"status"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Accepted || resp.Status != "resolved" {
		t.Errorf("CTR-ATT-002 output: got %+v", resp)
	}
	esc, ok := engine.GetEscalation("esc-gw-1")
	if !ok || esc.Status != core.EscalationResolved {
		t.Fatalf("record: want resolved, got %+v", esc)
	}
	if esc.Resolution != "policy approved for this run" || esc.ResolvedBy == "" {
		t.Errorf("human response: resolution=%q resolved_by=%q", esc.Resolution, esc.ResolvedBy)
	}

	if w := postEscalationDecision(srv, "esc-gw-1", "biz-1", "resolve", "again"); w.Code != http.StatusConflict {
		t.Errorf("second resolve: expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// TEST-GW-ESC-04 (G-002 posture): a foreign business scope cannot see or
// answer another tenant's alert.
func TestEscalationScopeMismatch(t *testing.T) {
	_, srv := escalationGateway(t)

	if w := getEscalations(srv, "biz-9"); w.Code != http.StatusOK {
		t.Fatalf("foreign list: expected 200, got %d", w.Code)
	} else {
		var resp struct {
			Escalations []core.Escalation `json:"escalations"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Escalations) != 0 {
			t.Errorf("foreign list must be empty, got %d", len(resp.Escalations))
		}
	}

	w := postEscalationDecision(srv, "esc-gw-1", "biz-2", "ack", "cross tenant")
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-tenant ack: expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if errBody := decodeErrorBody(t, w); errBody["category"] != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION, got %v", errBody["category"])
	}
}

// TEST-GW-ESC-05: identity enforcement on the escalation surface — missing
// credentials → 401, non-member → 403, member reaches core (404 for the
// unknown record proves the full auth path landed there).
func TestEscalationIdentityEnforcement(t *testing.T) {
	auth := identity.NewLocalAuthenticator()
	if err := auth.Register("alice", security.HashCredential([]byte("alice-secret")), identity.AuthMethodToken); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if err := auth.Register("mallory", security.HashCredential([]byte("mallory-secret")), identity.AuthMethodToken); err != nil {
		t.Fatalf("register mallory: %v", err)
	}
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{
		IdentityID: "alice", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive,
	}); err != nil {
		t.Fatalf("add alice membership: %v", err)
	}
	if err := members.Add(identity.Membership{
		IdentityID: "mallory", BusinessID: "biz-9",
		Role: identity.RoleMember, Status: identity.StatusActive,
	}); err != nil {
		t.Fatalf("add mallory membership: %v", err)
	}
	_, srv := escalationGateway(t,
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
	)

	issue := func(actor, credential string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"reasoning": "because"})
		req := httptest.NewRequest("POST",
			"/api/v1/escalations/esc-x/ack?business_id=biz-1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if actor != "" {
			a6AuthHeaders(req, actor, credential)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	if w := issue("", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d body=%s", w.Code, w.Body.String())
	}
	if w := issue("mallory", "mallory-secret"); w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-member, got %d body=%s", w.Code, w.Body.String())
	}
	if w := issue("alice", "alice-secret"); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for member with unknown record, got %d body=%s", w.Code, w.Body.String())
	}
}
