package memory

// Durable memory errors. They are distinguishable and map onto the existing
// NEXUS error envelope at the gateway edge (contract §15).
import "errors"

var (
	// ErrValidation is a malformed candidate, an out-of-bounds value, or an
	// unknown enum.
	ErrValidation = errors.New("validation")
	// ErrPermission is a write whose scope the caller or the acting agent may
	// not create.
	ErrPermission = errors.New("permission denied")
	// ErrScope is a cross-business or cross-division attempt, or a missing
	// membership.
	ErrScope = errors.New("scope denied")
	// ErrNotFound is used for records that do not exist AND for records the
	// caller may not see: existence never leaks (contract §4, G5).
	ErrNotFound = errors.New("memory record not found")
	// ErrConflict is a stale optimistic-concurrency update or a limit that
	// would be exceeded by the write.
	ErrConflict = errors.New("conflict")
	// ErrExpired is a write against a record that has expired.
	ErrExpired = errors.New("expired")
	// ErrTargetDrift is a bound write whose effective target no longer equals
	// the target that was admitted for it. Nothing was written: the platform
	// refuses before the mutation (contract §7).
	ErrTargetDrift = errors.New("memory target drift")
)

// ScopeOf is the canonical scope authority used by the platform. It is the
// single injected MembershipSet.AllowsScope (contract §4).
type membershipScopes struct {
	allows func(identityID, businessID, divisionID string) bool
}

func (m membershipScopes) AllowsScope(identityID, businessID, divisionID string) bool {
	if m.allows == nil {
		return false
	}
	return m.allows(identityID, businessID, divisionID)
}

// NewMembershipScopes adapts the canonical membership authority.
func NewMembershipScopes(allows func(identityID, businessID, divisionID string) bool) ScopeChecker {
	return membershipScopes{allows: allows}
}
