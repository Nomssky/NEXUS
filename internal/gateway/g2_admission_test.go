package gateway

// G2/G3 admission regression tests: lifecycle status gates submit only,
// and division-scoped membership is a strict sub-scope of business
// authority (AllowsScope). Contract: SCHEMA_IDENTITIES_ORG §12.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

func TestG3AllowsScopeSemantics(t *testing.T) {
	m := identity.NewMembershipSet()
	_ = m.Add(identity.Membership{IdentityID: "wide", BusinessID: "biz", Role: identity.RoleMember, Status: identity.StatusActive})
	_ = m.Add(identity.Membership{IdentityID: "narrow", BusinessID: "biz", DivisionID: "div-1", Role: identity.RoleMember, Status: identity.StatusActive})
	_ = m.Add(identity.Membership{IdentityID: "foreign", BusinessID: "other", Role: identity.RoleMember, Status: identity.StatusActive})

	cases := []struct {
		id, biz, div string
		want         bool
	}{
		{"wide", "biz", "", true},
		{"wide", "biz", "div-1", true},
		{"wide", "biz", "div-2", true},
		{"wide", "other", "", false},
		{"narrow", "biz", "", false}, // division membership never covers business level
		{"narrow", "biz", "div-1", true},
		{"narrow", "biz", "div-2", false},
		{"foreign", "biz", "", false},
	}
	for _, c := range cases {
		if got := m.AllowsScope(c.id, c.biz, c.div); got != c.want {
			t.Errorf("AllowsScope(%s,%s,%s)=%v want %v", c.id, c.biz, c.div, got, c.want)
		}
	}
}

// Enforcement-off variant: the status gate is independent of identity
// flags and rejects with 409 CONFLICT. Existing routes (business-level
// request during a suspended division) remain admittable.
func TestG2AdmissionRejectedForSuspendedOrg(t *testing.T) {
	reg := identity.NewRegistry("nx:nexus:test")
	now := time.Now().UTC()
	if _, err := reg.CreateIdentity(identity.Identity{ID: "o", Type: identity.TypeHuman, DisplayName: "owner", Provenance: schema.ProvenanceRef{ProducedAt: now}}, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateBusiness(identity.Business{EntityID: "biz-1", Name: "b", OwnerIdentityID: "o", Provenance: schema.ProvenanceRef{ProducedAt: now}}, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateDivision(identity.Division{EntityID: "div-1", BusinessID: "biz-1", Name: "d", OwnerIdentityID: "o", ParentBusinessID: "biz-1", Provenance: schema.ProvenanceRef{ProducedAt: now}}, "t"); err != nil {
		t.Fatal(err)
	}
	engine, _ := core.NewEngine(nil)
	ctx := t.Context()
	engine.Start(ctx)
	defer engine.Stop(ctx)
	srv := NewServer(engine, ":0", WithRegistry(reg))

	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/requests", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(w, r)
		return w
	}

	if _, err := reg.SetBusinessStatus("biz-1", identity.BusinessSuspended, "o"); err != nil {
		t.Fatal(err)
	}
	if w := post(`{"intent":"x","business_id":"biz-1","actor_id":"o"}`); w.Code != http.StatusConflict {
		t.Fatalf("suspended business: expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := reg.SetBusinessStatus("biz-1", identity.BusinessActive, "o"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetDivisionStatus("div-1", identity.DivisionSuspended, "o"); err != nil {
		t.Fatal(err)
	}
	if w := post(`{"intent":"x","business_id":"biz-1","division_id":"div-1","actor_id":"o"}`); w.Code != http.StatusConflict {
		t.Fatalf("suspended division: expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	// A business-level request during a suspended division still admits:
	// the suspended division blocks only its own admissions.
	if w := post(`{"intent":"x","business_id":"biz-1","actor_id":"o"}`); w.Code != http.StatusAccepted {
		t.Fatalf("business-level submit during suspended division: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
}
