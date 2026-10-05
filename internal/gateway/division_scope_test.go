package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// divControlKey gates the control surface for the division fixture.
const divControlKey = "div-fixture-control-key"

// divFixture wires identity enforcement plus a registry and seeds two
// businesses: biz-1 owns div-1/div-2, biz-2 owns div-3. wideActor holds a
// business-wide membership of biz-1, narrowActor a membership narrowed to
// div-1.
type divFixture struct {
	srv        *Server
	engine     *core.Engine
	biz1, biz2 string
	div1, div2 string
	div3       string
	wideID     string
	wideCred   string
	narrowID   string
	narrowCred string
}

func divisionFixture(t *testing.T) *divFixture {
	t.Helper()

	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	// Mirror the launcher posture: core receives the membership set with both
	// security flags on, so chainIdentity/chainAuthorization and the
	// division narrowing in CancelRequest all run as they do in production.
	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	const actor = "system"
	mustIdentity := func(id, businessID, divisionID string) {
		t.Helper()
		if _, err := reg.CreateIdentity(identity.Identity{
			ID:          id,
			Type:        identity.TypeHuman,
			DisplayName: id,
			BusinessID:  businessID,
			DivisionID:  divisionID,
		}, actor); err != nil {
			t.Fatalf("create identity %s: %v", id, err)
		}
		cred := id + "-secret"
		if err := auth.Register(id, security.HashCredential([]byte(cred)), identity.AuthMethodToken); err != nil {
			t.Fatalf("register credential %s: %v", id, err)
		}
	}
	mustBusiness := func(id string) {
		t.Helper()
		if _, err := reg.CreateBusiness(identity.Business{
			EntityID:        id,
			BusinessID:      id,
			Name:            id,
			OwnerIdentityID: "nx:human:owner",
			Status:          identity.BusinessActive,
		}, actor); err != nil {
			t.Fatalf("create business %s: %v", id, err)
		}
	}
	mustDivision := func(id, businessID string) {
		t.Helper()
		if _, err := reg.CreateDivision(identity.Division{
			EntityID:         id,
			BusinessID:       businessID,
			ParentBusinessID: businessID,
			Name:             id,
			OwnerIdentityID:  "nx:human:owner",
			Status:           identity.DivisionActive,
		}, actor); err != nil {
			t.Fatalf("create division %s: %v", id, err)
		}
	}
	mustMember := func(identityID, businessID, divisionID string) {
		t.Helper()
		if err := members.Add(identity.Membership{
			IdentityID: identityID,
			BusinessID: businessID,
			DivisionID: divisionID,
			Role:       identity.RoleMember,
			Status:     identity.StatusActive,
		}); err != nil {
			t.Fatalf("add membership %s: %v", identityID, err)
		}
	}

	mustIdentity("nx:human:owner", "", "")
	mustBusiness("biz-1")
	mustBusiness("biz-2")
	mustDivision("div-1", "biz-1")
	mustDivision("div-2", "biz-1")
	mustDivision("div-3", "biz-2")

	mustIdentity("nx:human:wide", "biz-1", "")
	mustIdentity("nx:human:narrow", "biz-1", "div-1")
	mustMember("nx:human:wide", "biz-1", "")
	mustMember("nx:human:narrow", "biz-1", "div-1")

	srv := NewServer(engine, ":0",
		WithRegistry(reg),
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
		WithControlAPIKey(divControlKey),
	)

	return &divFixture{
		srv:        srv,
		engine:     engine,
		biz1:       "biz-1",
		biz2:       "biz-2",
		div1:       "div-1",
		div2:       "div-2",
		div3:       "div-3",
		wideID:     "nx:human:wide",
		wideCred:   "nx:human:wide-secret",
		narrowID:   "nx:human:narrow",
		narrowCred: "nx:human:narrow-secret",
	}
}

// doAuth issues an authenticated request against the fixture gateway.
func (f *divFixture) doAuth(t *testing.T, method, path, body, actorID, credential string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Actor-ID", actorID)
	r.Header.Set("X-Actor-Credential", credential)
	r.Header.Set("X-API-Key", divControlKey)
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, r)
	return w
}

// submitDivision posts a submit body carrying the given division (empty omits it).
func (f *divFixture) submitDivision(t *testing.T, actorID, credential, divisionID string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]string{
		"intent":      "division scope probe",
		"business_id": f.biz1,
		"actor_id":    actorID,
	}
	if divisionID != "" {
		body["division_id"] = divisionID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return f.doAuth(t, http.MethodPost, "/api/v1/requests", string(raw), actorID, credential)
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code     string `json:"code"`
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, w.Body.String())
	}
	return envelope.Error.Code, envelope.Error.Message
}

func decodeAccepted(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode accepted: %v body=%s", err, w.Body.String())
	}
	return resp["request_id"]
}

// waitResult polls GET until the request is terminal and returns the body.
func (f *divFixture) waitResult(t *testing.T, actorID, credential, requestID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := f.doAuth(t, http.MethodGet,
			"/api/v1/requests/"+requestID+"?business_id="+f.biz1, "", actorID, credential)
		if w.Code == http.StatusOK {
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode result: %v body=%s", err, w.Body.String())
			}
			return body
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("request %s never reached a terminal result", requestID)
	return nil
}

// CTR-AUTH-001: division_id is accepted on submit and reaches the request
// context, so the recorded scope survives admission, execution and retrieval.
func TestDivisionScopeOnSubmitPropagates(t *testing.T) {
	f := divisionFixture(t)

	w := f.submitDivision(t, f.wideID, f.wideCred, f.div1)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit with division: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	id := decodeAccepted(t, w)

	if pending, ok := f.engine.Pending(id); !ok {
		t.Fatalf("request %s not admitted", id)
	} else if pending.DivisionID != f.div1 {
		t.Errorf("pending division: got %q want %q", pending.DivisionID, f.div1)
	}

	result := f.waitResult(t, f.wideID, f.wideCred, id)
	if got, _ := result["division_id"].(string); got != f.div1 {
		t.Errorf("result division_id: got %q want %q", got, f.div1)
	}
}

// §8: an unknown division is a validation failure, a division owned by another
// business is a validation failure, and an unverifiable division fails closed.
func TestDivisionScopeOnSubmitValidation(t *testing.T) {
	f := divisionFixture(t)

	t.Run("unknown division", func(t *testing.T) {
		w := f.submitDivision(t, f.wideID, f.wideCred, "div-nope")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
		if _, msg := decodeError(t, w); msg != "division not found" {
			t.Errorf("message: got %q want %q", msg, "division not found")
		}
	})

	t.Run("division of another business", func(t *testing.T) {
		w := f.submitDivision(t, f.wideID, f.wideCred, f.div3)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
		if _, msg := decodeError(t, w); msg != "division does not belong to the requested business" {
			t.Errorf("message: got %q", msg)
		}
	})

	t.Run("registry unavailable", func(t *testing.T) {
		// No registry wired: an unverifiable scope must fail closed (RT-02).
		srv := a6Fixture(t, f.wideID, f.wideCred, f.biz1)
		raw, _ := json.Marshal(map[string]string{
			"intent": "probe", "business_id": f.biz1,
			"actor_id": f.wideID, "division_id": f.div1,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		a6AuthHeaders(req, f.wideID, f.wideCred)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
		}
		if code, _ := decodeError(t, w); code != "DEPENDENCY_FAILURE" {
			t.Errorf("code: got %q want DEPENDENCY_FAILURE", code)
		}
	})
}

// §4.3: a division-scoped membership may act only inside its own division;
// a business-wide membership covers every division of its business.
func TestDivisionScopeOnSubmitMembership(t *testing.T) {
	f := divisionFixture(t)

	t.Run("narrow actor into its own division", func(t *testing.T) {
		w := f.submitDivision(t, f.narrowID, f.narrowCred, f.div1)
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("narrow actor into a sibling division", func(t *testing.T) {
		w := f.submitDivision(t, f.narrowID, f.narrowCred, f.div2)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
		code, msg := decodeError(t, w)
		if code != "AUTHORIZATION" {
			t.Errorf("code: got %q want AUTHORIZATION", code)
		}
		if msg != "access denied: actor is not a member of the requested division" {
			t.Errorf("message: got %q", msg)
		}
	})

	t.Run("wide actor into any division of its business", func(t *testing.T) {
		w := f.submitDivision(t, f.wideID, f.wideCred, f.div2)
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("narrow actor submits business-wide work", func(t *testing.T) {
		// A divisionless request is business-scope work; §G3 records that the
		// contract is silent on whether a division-scoped member may raise it.
		w := f.submitDivision(t, f.narrowID, f.narrowCred, "")
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// §4.3 read narrowing: a recorded division is enforced on retrieval; a
// divisionless request is not narrowed.
func TestDivisionScopeOnResultRead(t *testing.T) {
	f := divisionFixture(t)

	submit := func(division string) string {
		w := f.submitDivision(t, f.wideID, f.wideCred, division)
		if w.Code != http.StatusAccepted {
			t.Fatalf("submit %q: expected 202, got %d body=%s", division, w.Code, w.Body.String())
		}
		return decodeAccepted(t, w)
	}

	div1ID := submit(f.div1)
	div2ID := submit(f.div2)
	plainID := submit("")

	// Both records must be terminal before the narrowing assertions, so the
	// pending branch is not what answers.
	f.waitResult(t, f.wideID, f.wideCred, div1ID)
	f.waitResult(t, f.wideID, f.wideCred, div2ID)
	f.waitResult(t, f.wideID, f.wideCred, plainID)

	get := func(actorID, credential, requestID string) *httptest.ResponseRecorder {
		return f.doAuth(t, http.MethodGet,
			"/api/v1/requests/"+requestID+"?business_id="+f.biz1, "", actorID, credential)
	}

	t.Run("own division", func(t *testing.T) {
		if w := get(f.narrowID, f.narrowCred, div1ID); w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("sibling division", func(t *testing.T) {
		w := get(f.narrowID, f.narrowCred, div2ID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
		code, msg := decodeError(t, w)
		if code != "AUTHORIZATION" || msg != "access denied: division scope mismatch" {
			t.Errorf("got %q/%q", code, msg)
		}
	})

	t.Run("divisionless request is not narrowed", func(t *testing.T) {
		if w := get(f.narrowID, f.narrowCred, plainID); w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("business-wide member reads every division", func(t *testing.T) {
		if w := get(f.wideID, f.wideCred, div2ID); w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// §4.3 cancel narrowing: a recorded division is enforced at the cancel
// boundary, before any terminal-state answer reveals the record's status.
func TestDivisionScopeOnCancel(t *testing.T) {
	f := divisionFixture(t)

	submit := func(division string) string {
		w := f.submitDivision(t, f.wideID, f.wideCred, division)
		if w.Code != http.StatusAccepted {
			t.Fatalf("submit %q: expected 202, got %d body=%s", division, w.Code, w.Body.String())
		}
		return decodeAccepted(t, w)
	}

	div2ID := submit(f.div2)
	f.waitResult(t, f.wideID, f.wideCred, div2ID)

	t.Run("narrow actor cancels a sibling division", func(t *testing.T) {
		w := f.doAuth(t, http.MethodPost,
			"/api/v1/requests/"+div2ID+"/cancel?business_id="+f.biz1, "{}",
			f.narrowID, f.narrowCred)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
		code, msg := decodeError(t, w)
		if code != "AUTHORIZATION" || msg != "access denied: division scope mismatch" {
			t.Errorf("got %q/%q", code, msg)
		}
	})

	t.Run("business-wide member reaches the terminal answer", func(t *testing.T) {
		w := f.doAuth(t, http.MethodPost,
			"/api/v1/requests/"+div2ID+"/cancel?business_id="+f.biz1, "{}",
			f.wideID, f.wideCred)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 for an already-terminal request, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// Core-side division narrowing applies to an admitted-but-not-terminal record
// too — the pending path the HTTP handler cannot reach deterministically.
func TestCancelNarrowsPendingDivisionScope(t *testing.T) {
	f := divisionFixture(t)

	// wideActor raises work inside div-2; narrowActor's membership is pinned
	// to div-1, so the recorded division must block the cancel outright.
	w := f.submitDivision(t, f.wideID, f.wideCred, f.div2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	id := decodeAccepted(t, w)
	pending, ok := f.engine.Pending(id)
	if !ok {
		t.Skipf("request %s already terminal; nothing to assert on the pending path", id)
	}
	if pending.DivisionID != f.div2 {
		t.Fatalf("pending division: got %q want %q", pending.DivisionID, f.div2)
	}

	err := f.engine.CancelRequest(id, f.biz1, f.narrowID)
	if err != core.ErrDivisionScopeMismatch {
		t.Fatalf("narrow actor cancel: got %v want ErrDivisionScopeMismatch", err)
	}
	if err := f.engine.CancelRequest(id, f.biz1, f.wideID); err != nil {
		t.Fatalf("wide actor cancel: %v", err)
	}
}

// A divisionless record is never narrowed — the business-only check governs.
func TestCancelUnscopedRecordIsNotNarrowed(t *testing.T) {
	f := divisionFixture(t)

	w := f.submitDivision(t, f.wideID, f.wideCred, "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	id := decodeAccepted(t, w)
	if pending, ok := f.engine.Pending(id); ok && pending.DivisionID != "" {
		t.Fatalf("divisionless submit recorded division %q", pending.DivisionID)
	}
	if err := f.engine.CancelRequest(id, f.biz1, f.narrowID); err != nil &&
		!errors.Is(err, core.ErrAlreadyCompleted) {
		t.Fatalf("narrow actor cancel of a divisionless request: %v", err)
	}
}
