package identity

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// --- test doubles for the driven ports -------------------------------------
//
// These deliberately know nothing about store.Store, Registry or the security
// package: the use-case core must be exercisable through the ports alone.

type fakeCredentialRepo struct {
	mu      sync.Mutex
	recs    map[string]Credential
	saveErr error
	saves   int
}

func newFakeCredentialRepo() *fakeCredentialRepo {
	return &fakeCredentialRepo{recs: map[string]Credential{}}
}

func (r *fakeCredentialRepo) Find(identityID string) (Credential, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.recs[identityID]
	return c, ok
}

func (r *fakeCredentialRepo) LoadAll() ([]Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Credential, 0, len(r.recs))
	for _, c := range r.recs {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeCredentialRepo) Save(c Credential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saves++
	r.recs[c.IdentityID] = c
	return nil
}

func (r *fakeCredentialRepo) snapshotCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recs)
}

type fakeDirectory struct {
	identities map[string]Identity
}

func (d fakeDirectory) Lookup(identityID string) (Identity, bool) {
	i, ok := d.identities[identityID]
	return i, ok
}

// fakeVerifier is deterministic and intentionally NOT cryptographic. It proves
// the core does not reach for a specific primitive.
type fakeVerifier struct{}

func (fakeVerifier) Hash(b []byte) string   { return "fake:" + string(b) }
func (fakeVerifier) Equal(a, b string) bool { return a == b }

func newServiceWithFakes() (*authenticationService, *fakeCredentialRepo) {
	repo := newFakeCredentialRepo()
	return newAuthenticationService(repo, fakeVerifier{}), repo
}

// --- tests ------------------------------------------------------------------

// The core works with ports only: no store, no registry, no cryptography.
func TestAuthenticationServiceWorksThroughPortsOnly(t *testing.T) {
	svc, repo := newServiceWithFakes()
	const id = "nx:human:ports"
	secret := []byte("port-secret")

	if err := svc.Register(id, fakeVerifier{}.Hash(secret), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if repo.snapshotCount() != 1 {
		t.Fatal("registration must go through the repository port")
	}

	res, err := svc.Authenticate(id, secret)
	if err != nil || !res.Authenticated {
		t.Fatalf("expected success through ports, got %+v / %v", res, err)
	}
	if res.Method != AuthMethodPassword {
		t.Fatalf("method: got %q", res.Method)
	}
}

// Every failure cause must be indistinguishable in the result and the error.
func TestAuthenticationServiceUniformFailure(t *testing.T) {
	svc, _ := newServiceWithFakes()
	const known = "nx:human:known"
	if err := svc.Register(known, fakeVerifier{}.Hash([]byte("right")), AuthMethodToken); err != nil {
		t.Fatalf("register: %v", err)
	}

	cases := []struct {
		name  string
		id    string
		creds []byte
	}{
		{"unknown identity", "nx:human:absent", []byte("right")},
		{"wrong credential", known, []byte("wrong")},
		{"empty credential", known, nil},
	}
	var first AuthResult
	var firstErr error
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.Authenticate(tc.id, tc.creds)
			if res.Authenticated {
				t.Fatal("must not authenticate")
			}
			if err == nil {
				t.Fatal("must return an error")
			}
			if nerrors.CategoryOf(err) != nerrors.CategoryAuth {
				t.Fatalf("category: got %s", nerrors.CategoryOf(err))
			}
			if res.Method != AuthMethodNone {
				t.Fatalf("failed result must carry method none, got %q", res.Method)
			}
			if res.Reason != "authentication failed" {
				t.Fatalf("reason must be uniform, got %q", res.Reason)
			}
			if i == 0 {
				first, firstErr = res, err
				return
			}
			// Same reason and error text regardless of the cause.
			if res.Reason != first.Reason || err.Error() != firstErr.Error() {
				t.Fatalf("failure is not uniform: %q/%q vs %q/%q",
					res.Reason, err, first.Reason, firstErr)
			}
		})
	}
}

// Status and expiry enforcement comes from the directory port and fails closed.
func TestAuthenticationServiceDirectoryEnforcement(t *testing.T) {
	const id = "nx:human:directory"
	secret := []byte("s")
	expired := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)

	tests := []struct {
		name string
		dir  fakeDirectory
		want bool
	}{
		{
			name: "active and unexpired",
			dir: fakeDirectory{map[string]Identity{
				id: {ID: id, Status: StatusActive, ExpiresAt: &future},
			}},
			want: true,
		},
		{
			name: "missing record",
			dir:  fakeDirectory{map[string]Identity{}},
			want: false,
		},
		{
			name: "suspended",
			dir: fakeDirectory{map[string]Identity{
				id: {ID: id, Status: StatusSuspended},
			}},
			want: false,
		},
		{
			name: "expired",
			dir: fakeDirectory{map[string]Identity{
				id: {ID: id, Status: StatusActive, ExpiresAt: &expired},
			}},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newServiceWithFakes()
			if err := svc.Register(id, fakeVerifier{}.Hash(secret), AuthMethodPassword); err != nil {
				t.Fatalf("register: %v", err)
			}
			svc.SetDirectory(tc.dir)
			res, err := svc.Authenticate(id, secret)
			if res.Authenticated != tc.want {
				t.Fatalf("authenticated=%v, want %v (err=%v)", res.Authenticated, tc.want, err)
			}
			if !tc.want && err == nil {
				t.Fatal("denial must return an error")
			}
		})
	}
}

// A nil directory preserves the credential-only posture.
func TestAuthenticationServiceNilDirectory(t *testing.T) {
	svc, _ := newServiceWithFakes()
	const id = "nx:human:nil-dir"
	if err := svc.Register(id, fakeVerifier{}.Hash([]byte("x")), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc.SetDirectory(nil)
	if res, err := svc.Authenticate(id, []byte("x")); err != nil || !res.Authenticated {
		t.Fatalf("credential-only posture must authenticate: %+v / %v", res, err)
	}
}

func TestAuthenticationServiceTTL(t *testing.T) {
	svc, _ := newServiceWithFakes()
	const id = "nx:human:ttl"
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	svc.SetTTL(30 * time.Minute)
	if err := svc.Register(id, fakeVerifier{}.Hash([]byte("x")), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	res, err := svc.Authenticate(id, []byte("x"))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !res.ExpiresAt.Equal(base.Add(30 * time.Minute)) {
		t.Fatalf("expires_at: got %v", res.ExpiresAt)
	}
	if !res.Valid(base) {
		t.Fatal("must be valid at issue time")
	}
	if res.Valid(base.Add(31 * time.Minute)) {
		t.Fatal("must be expired after the TTL")
	}
	// SetClock(nil) is ignored rather than crashing the authenticator.
	svc.SetClock(nil)
	if _, err := svc.Authenticate(id, []byte("x")); err != nil {
		t.Fatalf("nil clock must be ignored: %v", err)
	}
}

func TestAuthenticationServiceRegisterValidation(t *testing.T) {
	tests := []struct {
		name string
		id   string
		hash string
		m    AuthMethod
	}{
		{"empty identity", "  ", "h", AuthMethodPassword},
		{"empty hash", "nx:human:x", "  ", AuthMethodPassword},
		{"unknown method", "nx:human:x", "h", AuthMethod("telepathy")},
		{"empty method", "nx:human:x", "h", AuthMethod("")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newServiceWithFakes()
			err := svc.Register(tc.id, tc.hash, tc.m)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
				t.Fatalf("category: got %s", nerrors.CategoryOf(err))
			}
			if repo.snapshotCount() != 0 || repo.saves != 0 {
				t.Fatal("a rejected registration must never reach the repository")
			}
		})
	}
}

func TestAuthenticationServicePropagatesSaveError(t *testing.T) {
	svc, repo := newServiceWithFakes()
	repo.saveErr = errors.New("disk full")
	if err := svc.Register("nx:human:x", "h", AuthMethodPassword); err == nil {
		t.Fatal("expected the repository error to surface")
	}
	if repo.snapshotCount() != 0 {
		t.Fatal("a failed save must not leave an in-memory credential")
	}
}

// Concurrent readers and writers must be race-free (run with -race).
func TestAuthenticationServiceConcurrentAccess(t *testing.T) {
	svc, _ := newServiceWithFakes()
	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		id := "nx:human:concurrent-" + string(rune('a'+i%26))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.Register(id, fakeVerifier{}.Hash([]byte(id)), AuthMethodToken)
			_, _ = svc.Authenticate(id, []byte(id))
			svc.SetTTL(time.Minute)
			svc.SetClock(func() time.Time { return time.Now().UTC() })
		}()
	}
	wg.Wait()
}
