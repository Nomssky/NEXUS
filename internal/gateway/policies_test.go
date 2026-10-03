package gateway

// TEST-GW-POLICY-*: contract-defined policy control surface
// (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §9).
//
// These cover the boundary, not evaluation — governance decisions themselves
// are covered by the core/engine suites and by the black-box e2e run.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

const policyTestKey = "policy-test-key"

// policyGateway builds an engine plus a started-over-HTTP gateway carrying the
// control key and an installation id (both §2.2 required fields the gateway
// stamps).
func policyGateway(t *testing.T) (*core.Engine, *httptest.Server) {
	t.Helper()
	return policyGatewayKeyed(t, policyTestKey)
}

// policyGatewayNoKey builds the same server with no control key at all, so the
// fail-closed CONTROL_DISABLED path can be exercised.
func policyGatewayNoKey(t *testing.T) (*core.Engine, *httptest.Server) {
	t.Helper()
	return policyGatewayKeyed(t, "")
}

func policyGatewayKeyed(t *testing.T, key string) (*core.Engine, *httptest.Server) {
	t.Helper()
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv := NewServer(engine, "127.0.0.1:0",
		WithControlAPIKey(key),
		WithNexusID("nx:nexus:test"),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return engine, ts
}

// policyDo issues one control-plane call. An empty key omits the header so the
// auth failures can be exercised.
func policyDo(t *testing.T, method, url, key, body string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	return resp.StatusCode, payload
}

// policyBody is the §2.2 minimum the contract does not let the server derive.
func policyBody(effect string) string {
	return `{
		"policy_type": "access_control",
		"name": "deny billing writes",
		"description": "billing intent is blocked in business biz-a",
		"status": "active",
		"business_id": "biz-a",
		"subject": {"subject_type": "all"},
		"action": {"action_type": "custom"},
		"resource": {"resource_type": "all"},
		"effect": "` + effect + `",
		"precedence": 10
	}`
}

// TEST-GW-POLICY-001: the control prefix keeps the existing X-API-Key gate —
// no key configured → 403 CONTROL_DISABLED (fail closed), a missing or wrong
// header → 401 UNAUTHORIZED/AUTH. Both fail before any handler runs.
func TestPolicyControlRequiresAPIKey(t *testing.T) {
	// Fail-closed when no key is configured at all.
	_, unkeyed := policyGatewayNoKey(t)
	for _, p := range []string{
		unkeyed.URL + "/api/v1/control/policies",
		unkeyed.URL + "/api/v1/control/policies/x",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			code, payload := policyDo(t, method, p, policyTestKey, `{}`)
			if code != http.StatusForbidden {
				t.Fatalf("%s %s without configured key: got %d, want 403", method, p, code)
			}
			if got := errorCode(payload); got != "CONTROL_DISABLED" {
				t.Fatalf("%s %s without configured key: code = %v, want CONTROL_DISABLED", method, p, got)
			}
		}
	}

	// Key configured: an absent header and a wrong key both answer 401.
	_, ts := policyGateway(t)
	for _, p := range []string{
		ts.URL + "/api/v1/control/policies",
		ts.URL + "/api/v1/control/policies/x",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			code, payload := policyDo(t, method, p, "", `{}`)
			if code != http.StatusUnauthorized {
				t.Fatalf("%s %s without header: got %d, want 401", method, p, code)
			}
			if got, cat := errorCode(payload), errorCategory(payload); got != "UNAUTHORIZED" || cat != "AUTH" {
				t.Fatalf("%s %s without header: code = %v category = %v, want UNAUTHORIZED/AUTH", method, p, got, cat)
			}

			code, payload = policyDo(t, method, p, "wrong-key", `{}`)
			if code != http.StatusUnauthorized {
				t.Fatalf("%s %s with wrong key: got %d, want 401", method, p, code)
			}
			if got, cat := errorCode(payload), errorCategory(payload); got != "UNAUTHORIZED" || cat != "AUTH" {
				t.Fatalf("%s %s with wrong key: code = %v category = %v, want UNAUTHORIZED/AUTH", method, p, got, cat)
			}
		}
	}
}

// errorCode reads the machine-readable code out of a CORE §3 envelope body.
func errorCode(payload map[string]any) any {
	errObj, _ := payload["error"].(map[string]any)
	return errObj["code"]
}

// errorCategory reads the CORE §3 envelope category out of a response body.
func errorCategory(payload map[string]any) any {
	errObj, _ := payload["error"].(map[string]any)
	return errObj["category"]
}

// TEST-GW-POLICY-002: the list carries the seeded built-in, and `effect`
// arrives as the §2.1/§2.2 enum name — not the internal ordinal. This is the
// wire-encoding regression guard for Outcome.MarshalJSON.
func TestPolicyControlListIncludesBuiltIn(t *testing.T) {
	_, ts := policyGateway(t)

	code, payload := policyDo(t, http.MethodGet, ts.URL+"/api/v1/control/policies", policyTestKey, "")
	if code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", code)
	}
	raw, _ := json.Marshal(payload["policies"])
	if !strings.Contains(string(raw), governance.DefaultAllowPolicyID) {
		t.Fatalf("list omits the built-in policy: %s", raw)
	}

	policies, ok := payload["policies"].([]any)
	if !ok || len(policies) == 0 {
		t.Fatalf("policies is not a list: %T", payload["policies"])
	}
	builtin, _ := policies[0].(map[string]any)
	if builtin["policy_id"] != governance.DefaultAllowPolicyID {
		t.Fatalf("first record = %v, want %s (list is sorted by policy_id)",
			builtin["policy_id"], governance.DefaultAllowPolicyID)
	}
	if effect, ok := builtin["effect"].(string); !ok || effect != "ALLOW" {
		t.Fatalf("built-in effect = %v (%T), want the enum name \"ALLOW\"",
			builtin["effect"], builtin["effect"])
	}
	if builtin["entity_type"] != "policy" {
		t.Fatalf("built-in entity_type = %v, want policy", builtin["entity_type"])
	}
}

// TEST-GW-POLICY-003: create → read → replace → delete, with the §2.2
// server-derived fields filled in and an update that preserves creation
// provenance while stamping updated_at.
func TestPolicyControlCRUDRoundTrip(t *testing.T) {
	engine, ts := policyGateway(t)
	base := ts.URL + "/api/v1/control/policies/billing-deny"

	// Create.
	code, payload := policyDo(t, http.MethodPut, base, policyTestKey, policyBody("DENY"))
	if code != http.StatusOK {
		t.Fatalf("create: got %d, want 200 (%v)", code, payload)
	}
	if payload["policy_id"] != "billing-deny" || payload["status"] != "active" {
		t.Fatalf("create response = %v", payload)
	}

	// Read back: defaults applied, effect a string.
	code, got := policyDo(t, http.MethodGet, base, policyTestKey, "")
	if code != http.StatusOK {
		t.Fatalf("get: got %d, want 200", code)
	}
	for field, want := range map[string]string{
		"entity_type":    "policy",
		"schema_version": "1.0.0",
		"nexus_id":       "nx:nexus:test",
		"policy_version": "1",
		"created_by":     controlActorID,
		"policy_id":      "billing-deny",
		"name":           "deny billing writes",
	} {
		if got[field] != want {
			t.Fatalf("%s = %v, want %v", field, got[field], want)
		}
	}
	if effect, ok := got["effect"].(string); !ok || effect != "DENY" {
		t.Fatalf("effect = %v (%T), want \"DENY\"", got["effect"], got["effect"])
	}
	if got["business_id"] != "biz-a" {
		t.Fatalf("business_id = %v, want biz-a", got["business_id"])
	}
	prov, _ := got["provenance"].(map[string]any)
	if prov["origin"] != "human" || prov["producer"] != "gateway:http-api" {
		t.Fatalf("provenance = %v, want the gateway producer reference", prov)
	}
	createdAt, _ := got["created_at"].(string)
	if createdAt == "" {
		t.Fatalf("created_at missing: %v", got)
	}

	// Replace: created_at/created_by survive, updated_at appears.
	replaced := strings.Replace(policyBody("DENY"), "deny billing writes", "deny billing writes (v2)", 1)
	code, _ = policyDo(t, http.MethodPut, base, policyTestKey, replaced)
	if code != http.StatusOK {
		t.Fatalf("replace: got %d, want 200", code)
	}
	code, got = policyDo(t, http.MethodGet, base, policyTestKey, "")
	if code != http.StatusOK {
		t.Fatalf("get after replace: got %d", code)
	}
	if got["name"] != "deny billing writes (v2)" {
		t.Fatalf("name after replace = %v", got["name"])
	}
	if got["created_at"] != createdAt {
		t.Fatalf("created_at changed on replace: %v -> %v", createdAt, got["created_at"])
	}
	if got["updated_at"] == nil {
		t.Fatalf("updated_at not stamped on replace: %v", got)
	}

	// Visible in the list.
	code, payload = policyDo(t, http.MethodGet, ts.URL+"/api/v1/control/policies", policyTestKey, "")
	if code != http.StatusOK {
		t.Fatalf("list: got %d", code)
	}
	found := false
	for _, p := range payload["policies"].([]any) {
		if rec, _ := p.(map[string]any); rec["policy_id"] == "billing-deny" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created policy missing from the list: %v", payload["policies"])
	}

	// Delete → gone from the engine and from a subsequent read.
	code, payload = policyDo(t, http.MethodDelete, base, policyTestKey, "")
	if code != http.StatusOK || payload["deleted"] != true {
		t.Fatalf("delete: got %d %v, want 200 {deleted:true}", code, payload)
	}
	if code, _ = policyDo(t, http.MethodGet, base, policyTestKey, ""); code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", code)
	}
	if _, ok := findGovernancePolicy(engine, "billing-deny"); ok {
		t.Fatalf("policy still present in the engine after delete")
	}

	// Unknown id on delete → 404, not a silent success.
	if code, _ = policyDo(t, http.MethodDelete, base, policyTestKey, ""); code != http.StatusNotFound {
		t.Fatalf("delete unknown: got %d, want 404", code)
	}
}

// TEST-GW-POLICY-004: the seeded built-in cannot be overwritten or removed —
// the rule that keeps an unconfigured installation on ALLOW instead of
// falling through to the engine default-DENY (contracts §9.4).
func TestPolicyControlBuiltInIsReadOnly(t *testing.T) {
	engine, ts := policyGateway(t)
	base := ts.URL + "/api/v1/control/policies/" + governance.DefaultAllowPolicyID

	code, payload := policyDo(t, http.MethodPut, base, policyTestKey, policyBody("DENY"))
	if code != http.StatusConflict {
		t.Fatalf("overwrite built-in: got %d, want 409 (%v)", code, payload)
	}
	if cat := payload["error"].(map[string]any)["category"]; cat != "CONFLICT" {
		t.Fatalf("overwrite built-in: category = %v, want CONFLICT", cat)
	}

	code, _ = policyDo(t, http.MethodDelete, base, policyTestKey, "")
	if code != http.StatusConflict {
		t.Fatalf("delete built-in: got %d, want 409", code)
	}

	if _, ok := findGovernancePolicy(engine, governance.DefaultAllowPolicyID); !ok {
		t.Fatalf("built-in policy is gone — the installation would default to DENY")
	}
	if engine.Governance().Policies()[0].Effect != governance.ALLOW {
		t.Fatalf("built-in effect changed to %v", engine.Governance().Policies()[0].Effect)
	}
}

// TEST-GW-POLICY-005: §2.2 required fields the server cannot derive are
// rejected with 400 VALIDATION before anything reaches the engine, and a
// rejected write leaves the policy set untouched.
func TestPolicyControlValidation(t *testing.T) {
	engine, ts := policyGateway(t)
	base := ts.URL + "/api/v1/control/policies/bad-policy"

	cases := []struct {
		name string
		body string
	}{
		{"missing effect", strings.Replace(policyBody("DENY"), `"effect": "DENY",`, "", 1)},
		{"bad effect", policyBody("MAYBE")},
		{"missing policy_type", strings.Replace(policyBody("DENY"), `"policy_type": "access_control",`, "", 1)},
		{"missing name", strings.Replace(policyBody("DENY"), `"name": "deny billing writes",`, "", 1)},
		{"missing description", strings.Replace(policyBody("DENY"), `"description": "billing intent is blocked in business biz-a",`, "", 1)},
		{"missing status", strings.Replace(policyBody("DENY"), `"status": "active",`, "", 1)},
		{"bad subject type", strings.Replace(policyBody("DENY"), `"subject_type": "all"`, `"subject_type": "everyone"`, 1)},
		{"bad action type", strings.Replace(policyBody("DENY"), `"action_type": "custom"`, `"action_type": "do_anything"`, 1)},
		{"bad resource type", strings.Replace(policyBody("DENY"), `"resource_type": "all"`, `"resource_type": "everything"`, 1)},
		{"approval without config", strings.Replace(policyBody("DENY"), `"effect": "DENY"`, `"effect": "REQUIRE_APPROVAL"`, 1)},
		{"malformed json", `{not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, payload := policyDo(t, http.MethodPut, base, policyTestKey, tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 (%v)", code, payload)
			}
			if cat := payload["error"].(map[string]any)["category"]; cat != "VALIDATION" {
				t.Fatalf("category = %v, want VALIDATION", cat)
			}
		})
	}

	// A body that names a different policy than the path is refused.
	code, _ := policyDo(t, http.MethodPut, base, policyTestKey,
		strings.Replace(policyBody("DENY"), `"policy_type"`, `"policy_id": "other", "policy_type"`, 1))
	if code != http.StatusBadRequest {
		t.Fatalf("policy_id mismatch: got %d, want 400", code)
	}

	// Nothing above may have reached the engine.
	if _, ok := findGovernancePolicy(engine, "bad-policy"); ok {
		t.Fatalf("a rejected write reached the engine")
	}
}

// findGovernancePolicy reads a record straight out of the engine, bypassing
// the HTTP surface, so a test can tell "not visible" from "not there".
func findGovernancePolicy(engine *core.Engine, id string) (*governance.Policy, bool) {
	for _, p := range engine.Governance().Policies() {
		if p != nil && p.PolicyID == id {
			return p, true
		}
	}
	return nil, false
}
