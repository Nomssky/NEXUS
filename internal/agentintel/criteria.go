package agentintel

// Success-criteria evaluation (contracts/AGENT_INTELLIGENCE_CONTRACTS.md §7).
//
// Criteria are machine-checkable references to structured runtime evidence, not
// prose. The runtime parses them once, evaluates them against the bounded
// observation window of the CURRENT execution plus the completed-effect count,
// and refuses to complete while any criterion is unmet. Nothing here interprets
// natural language and nothing here calls a model to judge a string: an
// unsupported field, an unsupported operator, an empty value or a malformed
// integer is a deterministic rejection, never an implicit success.

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Criterion operators (§7).
const (
	opEqual    = "=="
	opNotEqual = "!="
	opPresent  = "present"
	opAbsent   = "absent"
	opAtLeast  = ">="
	opAtMost   = "<="
)

// effectsField is the only criterion field that is not an observation field: it
// counts the completed consequential effects this execution actually recorded.
const effectsField = "effects_completed"

// observationFields is the closed set of directly addressable observation
// fields. `result.<key>` is the one indirection into an observation's Result map.
var observationFields = map[string]bool{
	"source":      true,
	"action_type": true,
	"status":      true,
	"outcome":     true,
}

// criterion is one parsed success criterion. Parsing happens once per
// execution; evaluation is pure.
type criterion struct {
	raw   string
	field string // observation field, `result.<key>`, or effectsField
	op    string
	value string // == / !=
	count int    // >= / <=
	isCnt bool   // field == effectsField
}

// criterionKey is a criterion's MEANING: everything except the spelling it
// arrived in. Two criteria with equal keys are semantically identical however
// they were written, so ` source == model ` and `source==model` share one key.
// It is deliberately a comparable value type holding only immutable data.
type criterionKey struct {
	field string
	op    string
	value string
	count int
}

// key renders the criterion's meaning. `raw` is excluded on purpose — it is
// provenance for the terminal message, not part of what the criterion asks for.
func (c criterion) key() criterionKey {
	return criterionKey{field: c.field, op: c.op, value: c.value, count: c.count}
}

// compareCriterionKey is a total order over criterion meanings. A total order
// is what makes an order-independent comparison possible.
func compareCriterionKey(a, b criterionKey) int {
	if c := strings.Compare(a.field, b.field); c != 0 {
		return c
	}
	if c := strings.Compare(a.op, b.op); c != 0 {
		return c
	}
	if c := strings.Compare(a.value, b.value); c != 0 {
		return c
	}
	return cmp.Compare(a.count, b.count)
}

// criteriaIdentity renders a criterion set as a canonical, order-independent
// value.
//
// Criteria are ANDed, so the identity of a set is its MEMBERSHIP, not its
// spelling or its shape: order carries no meaning, and ANDing the same
// criterion twice says exactly what saying it once says. Sorting establishes a
// total order; collapsing adjacent equals drops the repeats. Every remaining
// difference is a real difference in what the objective demands. An empty set is
// the empty slice, which is distinct from "unparsed".
func criteriaIdentity(crits []criterion) []criterionKey {
	keys := make([]criterionKey, len(crits))
	for i, c := range crits {
		keys[i] = c.key()
	}
	slices.SortFunc(keys, compareCriterionKey)
	return slices.Compact(keys)
}

// parseCriteria parses every criterion of an objective, failing closed on the
// first malformed one. An empty list is valid and means "no criteria".
func parseCriteria(raws []string) ([]criterion, error) {
	out := make([]criterion, 0, len(raws))
	for _, raw := range raws {
		c, err := parseCriterion(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// parseCriterion parses one criterion. Whitespace around the field, the
// operator and the value is trimmed; anything else is rejected.
func parseCriterion(raw string) (criterion, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return criterion{}, fmt.Errorf("criterion %q is empty", raw)
	}

	// effects_completed is matched before the field forms so `effects_completed`
	// can never be read as a prefix of some other field.
	if s == effectsField || strings.HasPrefix(s, effectsField) {
		rest := strings.TrimSpace(s[len(effectsField):])
		op := ""
		switch {
		case strings.HasPrefix(rest, opAtLeast):
			op = opAtLeast
		case strings.HasPrefix(rest, opAtMost):
			op = opAtMost
		case rest == "":
			return criterion{}, fmt.Errorf("criterion %q needs %s or %s", raw, opAtLeast, opAtMost)
		default:
			return criterion{}, fmt.Errorf("criterion %q uses an unsupported operator for %s", raw, effectsField)
		}
		n, err := parseCount(strings.TrimSpace(rest[2:]))
		if err != nil {
			return criterion{}, fmt.Errorf("criterion %q has %v", raw, err)
		}
		return criterion{raw: s, field: effectsField, op: op, count: n, isCnt: true}, nil
	}

	// <field>==<value> / <field>!=<value>
	for _, op := range []string{opEqual, opNotEqual} {
		i := strings.Index(s, op)
		if i < 0 {
			continue
		}
		// A doubled or trailing operator character is malformed syntax, not a
		// value: `status===ok` and `status!==ok` are rejected, not coerced.
		if i == 0 {
			return criterion{}, fmt.Errorf("criterion %q has no field", raw)
		}
		if i+2 < len(s) && (s[i+2] == '=' || s[i+2] == '!') {
			return criterion{}, fmt.Errorf("criterion %q is malformed", raw)
		}
		field := strings.TrimSpace(s[:i])
		value := strings.TrimSpace(s[i+len(op):])
		if err := checkField(field, raw); err != nil {
			return criterion{}, err
		}
		if value == "" {
			return criterion{}, fmt.Errorf("criterion %q has an empty value", raw)
		}
		return criterion{raw: s, field: field, op: op, value: value}, nil
	}

	// <field> present / <field> absent
	for _, op := range []string{opPresent, opAbsent} {
		suffix := " " + op
		if !strings.HasSuffix(s, suffix) {
			continue
		}
		field := strings.TrimSpace(s[:len(s)-len(suffix)])
		if err := checkField(field, raw); err != nil {
			return criterion{}, err
		}
		return criterion{raw: s, field: field, op: op}, nil
	}

	return criterion{}, fmt.Errorf("criterion %q is malformed", raw)
}

// checkField rejects anything outside the closed field set.
func checkField(field, raw string) error {
	if field == "" {
		return fmt.Errorf("criterion %q has no field", raw)
	}
	if observationFields[field] {
		return nil
	}
	if key, ok := strings.CutPrefix(field, "result."); ok && key != "" {
		return nil
	}
	return fmt.Errorf("criterion %q uses unsupported field %q", raw, field)
}

// parseCount accepts a bare non-negative decimal integer. Signs, fractions,
// exponents and empty values are malformed: the count is compared, never
// evaluated, so there is no reason to accept a second spelling.
func parseCount(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("a bound is missing")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q is not a non-negative integer bound", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a non-negative integer bound", s)
	}
	return n, nil
}

// fieldValue reads one observation field. An empty string means "not set":
// a missing Result key and an empty value are indistinguishable to a criterion,
// which is exactly why neither can stand in for evidence.
func fieldValue(o Observation, field string) string {
	if key, ok := strings.CutPrefix(field, "result."); ok {
		return o.Result[key]
	}
	switch field {
	case "source":
		return o.Source
	case "action_type":
		return string(o.ActionType)
	case "status":
		return o.Status
	case "outcome":
		return o.Outcome
	}
	return ""
}

// unknownOutcome is the platform's "the outcome could not be determined"
// classification. Such an observation is never proof that anything succeeded.
const unknownOutcome = "unknown"

// satisfied reports whether the criterion holds against the bounded window of
// this execution's observations and this execution's completed-effect count.
//
// All criteria use existential semantics (§7): contradictory observations are
// resolved by the presence of one satisfying observation, repeated observations
// are evaluated independently, and an unknown outcome never satisfies anything.
func (c criterion) satisfied(obs []Observation, effects int) bool {
	if c.isCnt {
		if c.op == opAtLeast {
			return effects >= c.count
		}
		return effects <= c.count
	}
	for _, o := range obs {
		if o.Outcome == unknownOutcome {
			continue
		}
		v := fieldValue(o, c.field)
		switch c.op {
		case opEqual:
			// Absence never equals a value.
			if v != "" && v == c.value {
				return true
			}
		case opNotEqual:
			// `!=` needs a KNOWN value that differs; an absent field is not
			// evidence of difference.
			if v != "" && v != c.value {
				return true
			}
		case opPresent:
			if v != "" {
				return true
			}
		case opAbsent:
			if v == "" {
				return true
			}
		}
	}
	return false
}

// criteriaStatus is the evaluation of a whole criterion list.
type criteriaStatus struct {
	all bool
	// unmet lists the raw text of the criteria that are not satisfied, so the
	// terminal message names what was missing instead of a bare "criteria".
	unmet []string
}

// evaluateCriteria evaluates every criterion against the evidence window.
// Criteria are ANDed; duplicates are evaluated independently but land in the
// same state either way.
func evaluateCriteria(crits []criterion, obs []Observation, effects int) criteriaStatus {
	st := criteriaStatus{all: true}
	for _, c := range crits {
		if !c.satisfied(obs, effects) {
			st.all = false
			st.unmet = append(st.unmet, c.raw)
		}
	}
	return st
}

// criteriaTracker carries criterion satisfaction across iterations so the loop
// can tell NEW evidence from evidence it has already seen. It holds one bool
// per objective criterion — bounded by the objective, never a second evidence
// store — and no new public API.
type criteriaTracker struct {
	crits []criterion
	last  []bool
	seen  bool
}

func newCriteriaTracker(crits []criterion) *criteriaTracker {
	return &criteriaTracker{crits: crits, last: make([]bool, len(crits))}
}

// advance re-evaluates the criteria against the current window and reports
// whether this iteration produced evidence satisfying at least one criterion
// that was still unmet before it. That is the only thing the no-progress
// counter resets on (§7).
//
// The first call establishes the baseline and reports false: a criterion that
// is already satisfied before any iteration ran is not progress. The loop
// seeds the tracker with the execution's starting window for exactly this
// reason, so real evidence from the first iteration is not swallowed.
func (t *criteriaTracker) advance(obs []Observation, effects int) bool {
	progressed := false
	for i, c := range t.crits {
		now := c.satisfied(obs, effects)
		// The first evaluation establishes the baseline; it cannot itself be
		// progress, because no iteration has happened yet.
		if t.seen && now && !t.last[i] {
			progressed = true
		}
		t.last[i] = now
	}
	t.seen = true
	return progressed
}
