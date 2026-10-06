package memory

// Retrieval filters and deterministic ranking (contract §11). No model is
// involved in either, and the ranking has a total order so the same query over
// the same memory state always returns the same sequence.

import (
	"sort"
	"strings"
	"time"
)

// Query is one authorized retrieval request. Every field is a caller-established
// fact; `Terms` is the only free-text input and it is matched as data.
type Query struct {
	// BusinessID must match the caller's business when set.
	BusinessID string
	DivisionID string
	// AgentID restricts the result to one agent's memory.
	AgentID string
	// Scope restricts to one visibility class (empty = every visible class).
	Scope  Scope
	Type   Type
	Source Source
	Trust  Trust
	// Key is an exact key lookup (already normalized on input).
	Key string
	// Subject restricts to one conflict namespace.
	Subject string
	// Terms is a case-insensitive substring filter over key and value.
	Terms        string
	CreatedAfter *time.Time
	// Limit bounds the result (0 = platform bound).
	Limit int
}

// Result is a bounded, ranked retrieval result. It never carries content the
// caller may not see.
type Result struct {
	Records   []Record
	Dropped   int
	Truncated bool
	Scope     Scope
}

// matchesQuery applies the deterministic filters. Visibility and expiry were
// already applied by the caller of Query.
func matchesQuery(r *Record, id Identity, q Query) bool {
	if q.BusinessID != "" && r.BusinessID != q.BusinessID {
		return false
	}
	if q.DivisionID != "" && r.DivisionID != q.DivisionID {
		return false
	}
	if q.AgentID != "" && r.AgentID != q.AgentID {
		return false
	}
	if q.Scope != "" && r.Scope != q.Scope {
		return false
	}
	if q.Type != "" && r.Type != q.Type {
		return false
	}
	if q.Source != "" && r.Source != q.Source {
		return false
	}
	if q.Trust != "" && r.Trust != q.Trust {
		return false
	}
	key := normalizeKey(q.Key)
	if key != "" && r.Key != key {
		return false
	}
	if subject := normalizeSubject(q.Subject, key); q.Subject != "" && r.Subject != subject {
		return false
	}
	if terms := strings.ToLower(strings.TrimSpace(q.Terms)); terms != "" {
		if !strings.Contains(strings.ToLower(r.Key), terms) &&
			!strings.Contains(strings.ToLower(r.Value), terms) {
			return false
		}
	}
	if q.CreatedAfter != nil && r.CreatedAt.Before(*q.CreatedAfter) {
		return false
	}
	// An explicit agent query never reaches another agent's private memory.
	if r.Scope == ScopeAgent && r.AgentID != id.AgentID {
		return false
	}
	return true
}

// rank sorts in place with the total order of contract §11:
//
//	narrower scope → exact match → non-conflicting → type match →
//	higher trust → newer updated_at → ascending id
func rank(recs []Record, q Query) {
	key := normalizeKey(q.Key)
	subject := normalizeSubject(q.Subject, key)
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := &recs[i], &recs[j]
		// 1. narrower scope first: agent, then division, then business.
		if ra, rb := scopeRank(a.Scope), scopeRank(b.Scope); ra != rb {
			return ra > rb
		}
		// 2. exact key/subject match first.
		if am, bm := exactMatch(a, key, subject), exactMatch(b, key, subject); am != bm {
			return am
		}
		// 3. non-conflicting before conflicting.
		if a.Conflict != b.Conflict {
			return !a.Conflict
		}
		// 4. requested type first.
		if q.Type != "" {
			if a.Type != q.Type {
				return false
			}
			if b.Type != q.Type {
				return true
			}
		}
		// 5. higher trust first.
		if ta, tb := trustRank(a.Trust), trustRank(b.Trust); ta != tb {
			return ta > tb
		}
		// 6. newer first.
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		// 7. stable tie-break.
		return a.ID < b.ID
	})
}

func exactMatch(r *Record, key, subject string) bool {
	if key != "" && r.Key == key {
		return true
	}
	if subject != "" && r.Subject == subject {
		return true
	}
	return false
}

func trustRank(t Trust) int {
	switch t {
	case TrustExplicit:
		return 4
	case TrustValidated:
		return 3
	case TrustObserved:
		return 2
	case TrustUnverified:
		return 1
	}
	return 0
}
