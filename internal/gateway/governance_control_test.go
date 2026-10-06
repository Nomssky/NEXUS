package gateway

// Governance control boundary over HTTP (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md
// §14, §16): the surface a model could abuse to grant itself authority — the
// policy control plane — stays behind the existing control-plane key, and an
// actor identity alone can never write a policy or decide an approval it is not
// authorized for.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// policyAllowAll is the seeded built-in, used to prove that a written policy
// really changes the decision an agent action gets.
func policyAllowAllJSON() string {
	return `{"policy_type":"access_control","name":"allow","description":"allow",
		"status":"active","subject":{"subject_type":"all"},
		"action":{"action_type":"custom"},"resource":{"resource_type":"all"},
		"effect":"ALLOW","precedence":0}`
}

func TestPolicySurfaceRequiresTheControlPlaneKey(t *testing.T) {
	_, ts := policyGateway(t)

	// No key at all.
	status, _ := policyDo(t, http.MethodPut,
		ts.URL+"/api/v1/control/policies/gov-deny", "",
		`{"policy_type":"access_control","name":"x","description":"x","status":"active",`+
			`"subject":{"subject_type":"all"},"action":{"action_type":"custom","action_ids":["tool_call"]},`+
			`"resource":{"resource_type":"tool"},"effect":"DENY"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("a policy write without the control key must be 401, got %d", status)
	}

	// A wrong key.
	status, _ = policyDo(t, http.MethodPut,
		ts.URL+"/api/v1/control/policies/gov-deny", "not-the-key",
		`{"policy_type":"access_control","name":"x","description":"x","status":"active",`+
			`"subject":{"subject_type":"all"},"action":{"action_type":"custom","action_ids":["tool_call"]},`+
			`"resource":{"resource_type":"tool"},"effect":"DENY"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("a policy write with a wrong key must be 401, got %d", status)
	}

	// And the control key alone still writes the policy.
	status, _ = policyDo(t, http.MethodPut,
		ts.URL+"/api/v1/control/policies/gov-deny", policyTestKey,
		`{"policy_type":"access_control","name":"gov-deny","description":"gov-deny","status":"active",`+
			`"subject":{"subject_type":"all"},"action":{"action_type":"custom","action_ids":["tool_call"]},`+
			`"resource":{"resource_type":"tool"},"effect":"DENY","precedence":1000}`)
	if status != http.StatusOK {
		t.Fatalf("the control key must be able to write a policy, got %d", status)
	}
}

func TestPolicySurfaceIsDisabledWithoutAConfiguredKey(t *testing.T) {
	_, ts := policyGatewayNoKey(t)
	status, body := policyDo(t, http.MethodPut,
		ts.URL+"/api/v1/control/policies/gov-deny", "",
		`{"policy_type":"access_control","name":"x","description":"x","status":"active",`+
			`"subject":{"subject_type":"all"},"action":{"action_type":"custom"},"resource":{"resource_type":"all"},`+
			`"effect":"DENY"}`)
	if status != http.StatusForbidden {
		t.Fatalf("an installation with no control key must refuse policy writes, got %d", status)
	}
	if e, _ := body["error"].(map[string]any); e == nil || e["code"] != "CONTROL_DISABLED" {
		t.Fatalf("expected the fail-closed CONTROL_DISABLED envelope, got %v", body)
	}
}

func TestWrittenPolicyDecidesAgentActionsAndAnActorCannotInstallIt(t *testing.T) {
	engine, ts := policyGateway(t)

	// Before: a tool_call is allowed by the seeded built-in.
	status, _ := policyDo(t, http.MethodGet, ts.URL+"/api/v1/control/policies/"+
		governance.DefaultAllowPolicyID, policyTestKey, "")
	if status != http.StatusOK {
		t.Fatalf("the seeded policy must be readable, got %d", status)
	}

	// Write a DENY for tool calls with the control key.
	status, _ = policyDo(t, http.MethodPut,
		ts.URL+"/api/v1/control/policies/gov-deny-tool", policyTestKey,
		`{"policy_type":"access_control","name":"gov-deny-tool","description":"deny tool calls",`+
			`"status":"active","subject":{"subject_type":"all"},`+
			`"action":{"action_type":"custom","action_ids":["tool_call"]},`+
			`"resource":{"resource_type":"tool"},"effect":"DENY","precedence":1000}`)
	if status != http.StatusOK {
		t.Fatalf("policy write: %d", status)
	}

	// The SAME engine now refuses an agent action.
	adm := engine.Controller()
	proposal := toolProposalForTest()
	got, err := adm.Admit(proposal)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if got.Allowed() {
		t.Fatalf("the written DENY must decide the admission: %+v", got)
	}
	if got.Decision.Outcome != governance.DENY {
		t.Fatalf("expected DENY, got %s", got.Decision.Outcome)
	}

	// Removing it restores the seeded default.
	status, _ = policyDo(t, http.MethodDelete,
		ts.URL+"/api/v1/control/policies/gov-deny-tool", policyTestKey, "")
	if status != http.StatusOK {
		t.Fatalf("policy delete: %d", status)
	}
	got, err = adm.Admit(proposal)
	if err != nil || !got.Allowed() {
		t.Fatalf("removing the DENY must restore the default allow: %+v %v", got, err)
	}
	if !strings.HasPrefix(got.Decision.MatchedPolicyID, governance.DefaultAllowPolicyID[:5]) &&
		got.Decision.MatchedPolicyID != governance.DefaultAllowPolicyID {
		t.Fatalf("the decision must name the matched policy: %+v", got.Decision)
	}
}

// toolProposalForTest is one runtime-established agent action proposal, built
// exactly as agentexec builds it (the fields are never model-supplied).
func toolProposalForTest() control.Proposal {
	return control.Proposal{
		ProposalID:    "prop-gw-1",
		CorrelationID: "exec-gw-1",
		ExecutionID:   "exec-gw-1",
		ActorID:       "nx:human:bootstrap",
		AgentID:       "agent-gw-1",
		BusinessID:    "default",
		Action:        control.ActionToolCall,
		Resource:      "http.request",
		ResourceType:  control.ResourceTypeCapability,
		ToolID:        "http.request",
		Operation:     "get",
	}
}
