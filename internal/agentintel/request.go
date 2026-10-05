package agentintel

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// objectiveInput is the wire shape the gateway places into
// executor.WorkRequest.Input["agent_objective"]. It carries only the
// objective: business/division/actor come from the request itself, so an
// objective can never widen scope (§2).
type objectiveInput struct {
	Objective Objective `json:"objective"`
}

// parseObjective decodes and validates the objective carried by a request,
// clamping its budget to the runtime caps.
func parseObjective(raw string, caps Caps) (Objective, Budget, error) {
	if raw == "" {
		return Objective{}, Budget{}, fmt.Errorf("missing objective")
	}
	var in objectiveInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return Objective{}, Budget{}, fmt.Errorf("malformed objective payload: %w", err)
	}
	if err := in.Objective.Validate(); err != nil {
		return Objective{}, Budget{}, err
	}
	return in.Objective, in.Objective.Budget.Clamp(caps), nil
}

// EncodeObjective renders an objective for the request input seam.
func EncodeObjective(o Objective) string {
	b, _ := json.Marshal(objectiveInput{Objective: o})
	return string(b)
}

// idSeq gives observation/child ids a stable, process-local, monotonic
// component. Durability is intentionally absent (§14): ids exist for tracing
// inside one process.
var idSeq atomic.Int64

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, idSeq.Add(1))
}

// jsonString renders an event payload deterministically enough for tests.
func jsonString(v any) ([]byte, error) { return json.Marshal(v) }
