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
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// approvalGateway builds a started engine whose governance requires approval
// on every action (the chain gate gates each submitted request) plus a
// gateway with the given options — identity fixtures for enforcement tests.
func approvalGateway(t *testing.T, opts ...ServerOption) (*core.Engine, *Server) {
	t.Helper()
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	registerSimulatedProvider(t, engine)
	engine.Governance().SetPolicies([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
			ApprovalConfig: &governance.ApprovalConfig{
				TimeoutSeconds:    3600,
				AutoDenyOnTimeout: true,
			},
		},
	})
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })
	return engine, NewServer(engine, ":0", opts...)
}

// submitGated submits a request that stops at the approval gate and returns
// (requestID, approvalID) once the APPROVAL_REQUIRED result is stored.
func submitGated(t *testing.T, srv *Server, engine *core.Engine, intent, businessID string) (string, string) {
	t.Helper()
	id := submitAndWait(t, srv, engine, intent, businessID, "user-1", true)
	result, ok := engine.GetResult(id)
	if !ok || result.Error == nil {
		t.Fatalf("expected an approval-required result for %s, got %+v", id, result)
	}
	if result.Error.Category != "APPROVAL_REQUIRED" {
		t.Fatalf("expected APPROVAL_REQUIRED, got %+v", result.Error)
	}
	approvalID := result.Error.Details["approval_id"]
	if approvalID == "" {
		t.Fatalf("expected error.details.approval_id, got %+v", result.Error)
	}
	return id, approvalID
}

// postApprovalDecision issues a POST approve/deny decision.
func postApprovalDecision(srv *Server, approvalID, businessID, decide, reason string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	req := httptest.NewRequest("POST",
		"/api/v1/approvals/"+approvalID+"/"+decide+"?business_id="+businessID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// TEST-GW-APR-01: list without business_id → 400 VALIDATION (fail closed).
func TestApprovalListMissingBusinessID(t *testing.T) {
	_, srv := approvalGateway(t)

	req := httptest.NewRequest("GET", "/api/v1/approvals", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if errBody := decodeErrorBody(t, w); errBody["category"] != "VALIDATION" {
		t.Errorf("expected VALIDATION, got %v", errBody["category"])
	}
}

// TEST-GW-APR-02: unknown approval → 404 VALIDATION "approval not found".
func TestApprovalApproveUnknown(t *testing.T) {
	_, srv := approvalGateway(t)

	w := postApprovalDecision(srv, "apr-does-not-exist", "biz-1", "approve", "because")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	if errBody := decodeErrorBody(t, w); errBody["category"] != "VALIDATION" {
		t.Errorf("expected VALIDATION, got %v", errBody["category"])
	}
}

// TEST-GW-APR-03: missing/blank reason → 400 VALIDATION (SCHEMA_WORK §6.2
// decision_rationale is required once a decision is made).
func TestApprovalDecisionMissingReason(t *testing.T) {
	engine, srv := approvalGateway(t)
	_, approvalID := submitGated(t, srv, engine, "gated", "biz-1")

	for _, reason := range []string{"", "   "} {
		w := postApprovalDecision(srv, approvalID, "biz-1", "approve", reason)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("reason %q: expected 400, got %d body=%s", reason, w.Code, w.Body.String())
		}
		if errBody := decodeErrorBody(t, w); errBody["category"] != "VALIDATION" {
			t.Errorf("expected VALIDATION, got %v", errBody["category"])
		}
	}
	// Record untouched by the rejected decisions.
	if got := engine.ListApprovals("biz-1"); len(got) != 1 {
		t.Errorf("expected the record to stay pending, got %d", len(got))
	}
}

// TEST-GW-APR-04: foreign business scope → 404 (G5: the record is outside
// the caller's scope, indistinguishable from unknown).
func TestApprovalScopeMismatch(t *testing.T) {
	engine, srv := approvalGateway(t)
	_, approvalID := submitGated(t, srv, engine, "gated", "biz-1")

	w := postApprovalDecision(srv, approvalID, "biz-2", "approve", "because")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "VALIDATION" {
		t.Errorf("expected VALIDATION, got %v", errBody["category"])
	}
}

// TEST-GW-APR-05: list returns the contract-shaped pending record
// (SCHEMA_WORK §6.2 fields the gateway projection carries).
func TestApprovalListShape(t *testing.T) {
	engine, srv := approvalGateway(t)
	_, approvalID := submitGated(t, srv, engine, "gated", "biz-1")

	req := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Approvals []core.ApprovalRecord `json:"approvals"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(body.Approvals))
	}
	rec := body.Approvals[0]
	if rec.EntityID != approvalID || rec.Status != "PENDING" || rec.EntityType != "approval" {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.BusinessID != "biz-1" || rec.PolicyRef != "require-approval" || rec.Scope != "core" {
		t.Errorf("unexpected scope/policy wiring: %+v", rec)
	}
	// G-009: enforcement is off, so the submit actor was not trusted — the
	// fixed marker is bound as the requester.
	if rec.RequesterID != "unauthenticated" {
		t.Errorf("expected unauthenticated requester (G-009), got %q", rec.RequesterID)
	}
	if rec.ExpiresAt == nil {
		t.Error("expected expires_at from the approval timeout")
	}
}

// TEST-GW-APR-06: approve → 202 {approval_id, status:approved}; the resumed
// request re-executes to completed, observable via GET (same posture as
// cancel: decide asynchronously, observe the final state).
func TestApprovalApproveResumesRequest(t *testing.T) {
	engine, srv := approvalGateway(t)
	reqID, approvalID := submitGated(t, srv, engine, "gated", "biz-1")

	w := postApprovalDecision(srv, approvalID, "biz-1", "approve", "safe to run")
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode 202 body: %v", err)
	}
	if body["approval_id"] != approvalID || body["status"] != "approved" {
		t.Errorf("unexpected 202 body: %v", body)
	}
	if got := w.Header().Get("X-Correlation-ID"); got != approvalID {
		t.Errorf("expected X-Correlation-ID %q, got %q", approvalID, got)
	}

	waitUntil(t, "resumed completed result", func() bool {
		result, ok := engine.GetResult(reqID)
		return ok && result.Status == "completed"
	})
	getReq := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-1", nil)
	getW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET result: expected 200, got %d", getW.Code)
	}
	var result core.Response
	if err := json.NewDecoder(getW.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed after resume, got %q", result.Status)
	}
}

// TEST-GW-APR-07: deny → 200 {status:denied}, no resume — the stored result
// stays failed/APPROVAL_REQUIRED (D2 pin) and the record leaves the list.
func TestApprovalDenyKeepsGatedResult(t *testing.T) {
	engine, srv := approvalGateway(t)
	reqID, approvalID := submitGated(t, srv, engine, "gated", "biz-1")

	w := postApprovalDecision(srv, approvalID, "biz-1", "deny", "too risky")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["approval_id"] != approvalID || body["status"] != "denied" {
		t.Errorf("unexpected body: %v", body)
	}

	// Stored result untouched by the denial.
	result, ok := engine.GetResult(reqID)
	if !ok || result.Status != "failed" {
		t.Fatalf("expected stored failed result to remain, got ok=%v %+v", ok, result)
	}
	if result.Error == nil || result.Error.Category != "APPROVAL_REQUIRED" {
		t.Fatalf("expected APPROVAL_REQUIRED to remain, got %+v", result.Error)
	}
	// Record resolved → gone from the pending list.
	if got := engine.ListApprovals("biz-1"); len(got) != 0 {
		t.Errorf("expected empty list after deny, got %d", len(got))
	}
	// Second decision → 404.
	if w2 := postApprovalDecision(srv, approvalID, "biz-1", "deny", "again"); w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 on repeat deny, got %d", w2.Code)
	}
}

// TEST-GW-APR-08: identity enforcement on scoped approval paths — missing
// credentials → 401, authenticated non-member → 403, member reaches core
// (404 for the unknown record proves the full auth path landed there).
func TestApprovalIdentityEnforcement(t *testing.T) {
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
	_, srv := approvalGateway(t,
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
	)

	issue := func(actor, credential string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"reason": "because"})
		req := httptest.NewRequest("POST",
			"/api/v1/approvals/apr-x/approve?business_id=biz-1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if actor != "" {
			a6AuthHeaders(req, actor, credential)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	// Missing credentials → 401 (fail closed).
	if w := issue("", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d body=%s", w.Code, w.Body.String())
	}
	// Authenticated but not a member of biz-1 → 403.
	if w := issue("mallory", "mallory-secret"); w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-member, got %d body=%s", w.Code, w.Body.String())
	}
	// Member passes auth+membership and reaches core → 404 for the unknown
	// record (proves the decision path is reachable behind enforcement).
	w := issue("alice", "alice-secret")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for member with unknown record, got %d body=%s", w.Code, w.Body.String())
	}
}
