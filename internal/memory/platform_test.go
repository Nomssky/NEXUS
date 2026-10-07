package memory

// Behavioural tests for the Agent Memory & Context Platform v1
// (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md): scope isolation, durability,
// CRUD, versioning, bounded deterministic retrieval, platform-owned provenance,
// redaction, conflicts and lifecycle.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// ---------- fixture ----------

type sink struct {
	mu     sync.Mutex
	events []Event
}

func (s *sink) Publish(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *sink) types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, e := range s.events {
		out = append(out, e.Type)
	}
	return out
}

func (s *sink) count(evType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.Type == evType {
			n++
		}
	}
	return n
}

// secretRedactor stands in for the platform redactor: one registered secret.
func secretRedactor(secret string) Redactor {
	return RedactorFunc(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
}

func memberships(t *testing.T) *identity.MembershipSet {
	t.Helper()
	ms := identity.NewMembershipSet()
	add := func(m identity.Membership) {
		t.Helper()
		if err := ms.Add(m); err != nil {
			t.Fatalf("membership: %v", err)
		}
	}
	// biz-1: business-wide member; biz-1/div-1: division member.
	add(identity.Membership{IdentityID: "owner", BusinessID: "biz-1", Role: identity.RoleMember, Status: identity.StatusActive})
	add(identity.Membership{IdentityID: "owner", BusinessID: "biz-1", DivisionID: "div-1", Role: identity.RoleMember, Status: identity.StatusActive})
	// A second identity that only holds the division.
	add(identity.Membership{IdentityID: "divider", BusinessID: "biz-1", DivisionID: "div-1", Role: identity.RoleMember, Status: identity.StatusActive})
	// ... and one that only holds a sibling division.
	add(identity.Membership{IdentityID: "otherdiv", BusinessID: "biz-1", DivisionID: "div-2", Role: identity.RoleMember, Status: identity.StatusActive})
	// A foreign business member, and a suspended member.
	add(identity.Membership{IdentityID: "foreign", BusinessID: "biz-2", Role: identity.RoleMember, Status: identity.StatusActive})
	add(identity.Membership{IdentityID: "suspended", BusinessID: "biz-1", Role: identity.RoleMember, Status: identity.StatusSuspended})
	return ms
}

type fixture struct {
	p     *Platform
	st    *store.MemStore
	bus   *sink
	clock time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ms := memberships(t)
	f := &fixture{st: store.NewMemStore(), bus: &sink{},
		clock: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	p, err := Open(f.st, Options{
		Now:    func() time.Time { return f.clock },
		Scopes: NewMembershipScopes(ms.AllowsScope),
		Redact: secretRedactor("tok-abcdef-123456"),
		Bus:    f.bus,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f.p = p
	return f
}

func owner(agent string) Identity {
	return Identity{ActorID: "owner", BusinessID: "biz-1", AgentID: agent}
}

// ---------- §4 scope and authorization ----------

func TestScopeIsolationAcrossBusinessAndDivisionAndAgent(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "k", Value: "v"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Scope != ScopeAgent || rec.AgentID != "a1" {
		t.Fatalf("an agent write must default to its own agent scope, got %+v", rec)
	}
	// Sibling agent: invisible.
	if _, err := f.p.Get(owner("a2"), rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sibling agent must not read private memory: %v", err)
	}
	// Division member of the same business, different division view: still the
	// same business membership, but agent-scope memory stays private.
	if _, err := f.p.Get(Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1", AgentID: "a2"}, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another identity must not read agent memory: %v", err)
	}
	// Foreign business: another business' record is indistinguishable from a
	// record that does not exist (G5 — no existence leak).
	if _, err := f.p.Get(Identity{ActorID: "foreign", BusinessID: "biz-2", AgentID: "a1"}, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a foreign business must see nothing: %v", err)
	}
	// Suspended member: refused.
	if _, err := f.p.Get(Identity{ActorID: "suspended", BusinessID: "biz-1", AgentID: "a1"}, rec.ID); !errors.Is(err, ErrScope) {
		t.Fatalf("a suspended member must be refused: %v", err)
	}
	// A non-member is refused at authorization, before any store read.
	if _, err := f.p.Query(Identity{ActorID: "nobody", BusinessID: "biz-1"}, Query{}); !errors.Is(err, ErrScope) {
		t.Fatalf("a non-member must be refused: %v", err)
	}
	if _, err := f.p.Write(Identity{ActorID: "nobody", BusinessID: "biz-1"}, WriterUser, Candidate{Key: "k", Value: "v"}); !errors.Is(err, ErrScope) {
		t.Fatalf("a non-member must not write: %v", err)
	}
}

func TestBusinessScopeVisibleWithinBusinessOnly(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner(""), WriterUser, Candidate{Key: "policy", Value: "shared", Scope: ScopeBusiness})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Scope != ScopeBusiness || rec.AgentID != "" {
		t.Fatalf("business memory must not be attributed to one agent: %+v", rec)
	}
	// Any member of the business sees it, including a different identity.
	got, err := f.p.Get(Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1", AgentID: "other"}, rec.ID)
	if err != nil || got.Value != "shared" {
		t.Fatalf("business memory must be visible inside the business: %+v %v", got, err)
	}
	// Another business never sees it, and never learns that it exists.
	if _, err := f.p.Get(Identity{ActorID: "foreign", BusinessID: "biz-2"}, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-business read must see nothing: %v", err)
	}
	if _, err := f.p.Query(Identity{ActorID: "foreign", BusinessID: "biz-2"}, Query{}); err != nil {
		t.Fatalf("a foreign business may still query its own (empty) memory: %v", err)
	}
	// A division member cannot see it through a query scoped to their division:
	// business memory has no division, so it is not division-scoped data.
	res, err := f.p.Query(Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1", AgentID: "other"},
		Query{DivisionID: "div-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Records {
		if r.Scope == ScopeBusiness {
			t.Fatalf("a division-scoped query must not return business memory: %+v", r)
		}
	}
}

func TestDivisionScopeVisibility(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1", AgentID: ""},
		WriterUser, Candidate{Key: "k", Value: "division value", Scope: ScopeDivision, DivisionID: "div-1"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// The division's own member sees it.
	if _, err := f.p.Get(Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1"}, rec.ID); err != nil {
		t.Fatalf("division member must read its division memory: %v", err)
	}
	// A member of a SIBLING division does not see it: no scope widens on request.
	// The answer is a scope denial, which reveals nothing about the record: the
	// caller already knows it holds no div-1 membership.
	if _, err := f.p.Get(Identity{ActorID: "otherdiv", BusinessID: "biz-1", DivisionID: "div-2"}, rec.ID); !errors.Is(err, ErrScope) {
		t.Fatalf("division memory must stay inside its division: %v", err)
	}
	// A business-wide member covers every division of its business, exactly as
	// in the tool platform: that is the canonical membership rule, not a
	// memory-specific one.
	if _, err := f.p.Get(Identity{ActorID: "owner", BusinessID: "biz-1"}, rec.ID); err != nil {
		t.Fatalf("a business-wide member must reach division memory: %v", err)
	}
	// Division scope without a division is a validation failure.
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "x", Value: "v", Scope: ScopeDivision}); !errors.Is(err, ErrValidation) {
		t.Fatalf("division scope without a division must be rejected: %v", err)
	}
}

func TestAgentCannotWidenItsMemoryScope(t *testing.T) {
	f := newFixture(t)
	divider := Identity{ActorID: "owner", BusinessID: "biz-1", DivisionID: "div-1", AgentID: "a1",
		AgentMemoryMode: "division"}
	// Division mode may write division or agent memory.
	if _, err := f.p.Write(divider, WriterAgent, Candidate{Key: "d", Value: "v",
		Scope: ScopeDivision, DivisionID: "div-1"}); err != nil {
		t.Fatalf("division mode must allow division memory: %v", err)
	}
	if _, err := f.p.Write(divider, WriterAgent, Candidate{Key: "a", Value: "v", Scope: ScopeAgent}); err != nil {
		t.Fatalf("division mode must allow agent memory: %v", err)
	}
	// Business mode may not be claimed by a division-scoped agent.
	if _, err := f.p.Write(divider, WriterAgent, Candidate{Key: "b", Value: "v", Scope: ScopeBusiness}); !errors.Is(err, ErrPermission) {
		t.Fatalf("an agent must not widen its scope to business: %v", err)
	}
	// memory mode none refuses every durable write.
	none := Identity{ActorID: "owner", BusinessID: "biz-1", AgentID: "a1", AgentMemoryMode: "none"}
	if _, err := f.p.Write(none, WriterAgent, Candidate{Key: "n", Value: "v"}); !errors.Is(err, ErrPermission) {
		t.Fatalf("memory mode none must refuse durable writes: %v", err)
	}
	// An agent may not write into another division than the one it acts for.
	// An agent acting for div-1 cannot write into div-2. Whether the refusal is
	// "no membership" or "not your acting division" depends on the caller, and
	// both are refusals: the write never happens.
	crossDiv := Identity{ActorID: "divider", BusinessID: "biz-1", DivisionID: "div-1", AgentID: "a1", AgentMemoryMode: "division"}
	_, err := f.p.Write(crossDiv, WriterAgent, Candidate{Key: "x", Value: "v", Scope: ScopeDivision, DivisionID: "div-2"})
	if !errors.Is(err, ErrPermission) && !errors.Is(err, ErrScope) {
		t.Fatalf("an agent must not write into a foreign division: %v", err)
	}
	// An agent acting for a division it holds can write there.
	other := Identity{ActorID: "otherdiv", BusinessID: "biz-1", DivisionID: "div-2", AgentID: "a1", AgentMemoryMode: "division"}
	if _, err := f.p.Write(other, WriterAgent, Candidate{Key: "y", Value: "v",
		Scope: ScopeDivision, DivisionID: "div-2"}); err != nil {
		t.Fatalf("an agent acting for a division it holds must be able to write: %v", err)
	}
}

func TestForeignRecordDoesNotLeakExistence(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "secret-note", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	// The same id, asked by two identities: one may see it, one must get exactly
	// the answer a non-existent id produces.
	_, okErr := f.p.Get(owner("a1"), rec.ID)
	_, hiddenErr := f.p.Get(owner("a2"), rec.ID)
	_, missingErr := f.p.Get(owner("a2"), "mem:biz-1:a2:does-not-exist")
	if okErr != nil {
		t.Fatalf("the owner must read its own record: %v", okErr)
	}
	if !errors.Is(hiddenErr, ErrNotFound) || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("both foreign reads must be not-found: %v / %v", hiddenErr, missingErr)
	}
	if strings.Contains(hiddenErr.Error(), rec.Value) || strings.Contains(missingErr.Error(), rec.Value) {
		t.Fatalf("the error must not echo record content")
	}
	if !strings.Contains(hiddenErr.Error(), "not found") || !strings.Contains(missingErr.Error(), "not found") {
		t.Fatalf("both answers must be the same not-found shape: %q / %q", hiddenErr, missingErr)
	}
}

// ---------- §8 durability ----------

func TestDurableMemorySurvivesReopen(t *testing.T) {
	f := newFixture(t)
	if _, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "durable"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.st, Options{Scopes: NewMembershipScopes(memberships(t).AllowsScope)})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	rec, err := reopened.Get(owner("a1"), memoryIDForTest("biz-1", "", "a1", "k"))
	if err != nil || rec.Value != "durable" {
		t.Fatalf("durable memory must survive reopen: %+v %v", rec, err)
	}
}

func memoryIDForTest(biz, div, agent, key string) string { return MemoryID(biz, div, agent, key) }

func TestCorruptRecordFailsClosedAtOpen(t *testing.T) {
	st := store.NewMemStore()
	stored := &store.Record{ID: "mem:biz-1:a1:k", Type: store.RecordTypeMemory,
		Status: store.RecordStatusActive, BusinessID: "biz-1", Data: []byte(`{"key":"k","business_id":"biz-1"}`)}
	if err := st.Put(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(st, Options{Scopes: NewMembershipScopes(memberships(t).AllowsScope)}); err == nil {
		t.Fatal("a record without canonical scope/type/source must abort the open")
	}
}

// ---------- §3/§10 CRUD and lifecycle ----------

func TestCreateReadDeleteAndRecreate(t *testing.T) {
	f := newFixture(t)
	created, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "k", Value: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Status != StatusActive || created.CreatedAt.IsZero() {
		t.Fatalf("a new record starts active at version 1: %+v", created)
	}
	if err := f.p.Delete(owner("a1"), created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.clock = f.clock.Add(time.Hour)
	if _, err := f.p.Get(owner("a1"), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted record must be invisible: %v", err)
	}
	// Re-creating the key does not resurrect the deleted record.
	again, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "k", Value: "two"})
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if again.Value != "two" || again.Status != StatusActive || again.Conflict {
		t.Fatalf("recreation must be a fresh active record: %+v", again)
	}
	if f.bus.count(EventDeleted) != 1 || f.bus.count(EventCreated) != 2 {
		t.Fatalf("lifecycle events wrong: %v", f.bus.types())
	}
}

func TestExpiryRemovesFromRetrieval(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.p.Expire(owner("a1"), rec.ID, f.clock.Add(time.Hour)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := f.p.Get(owner("a1"), rec.ID); err != nil {
		t.Fatalf("an unexpired record must still be readable: %v", err)
	}
	// Move the clock past the expiry: normal retrieval must no longer see it.
	f.clock = f.clock.Add(2 * time.Hour)
	res, err := f.p.Query(owner("a1"), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 0 {
		t.Fatalf("an expired record must not be returned: %+v", res.Records)
	}
	if _, err := f.p.Get(owner("a1"), rec.ID); err != nil {
		t.Fatalf("an expired record stays auditable by id: %v", err)
	}
	if f.bus.count(EventExpired) != 1 {
		t.Fatalf("expiry must be audited: %v", f.bus.types())
	}
	// An expiry in the past is refused at write time.
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "p", Value: "v",
		ExpiresAt: ptrTime(f.clock.Add(-time.Minute))}); !errors.Is(err, ErrValidation) {
		t.Fatalf("a past expiry must be rejected: %v", err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestDeleteIsScoped(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.p.Delete(owner("a2"), rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an agent must not delete another agent's memory: %v", err)
	}
	if _, err := f.p.Get(owner("a1"), rec.ID); err != nil {
		t.Fatalf("the record must still exist: %v", err)
	}
}

func TestDeleteExactRemovesOnlyTheNamedSet(t *testing.T) {
	f := newFixture(t)
	a, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k1", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k2", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k3", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	// An exact delete removes the named records and nothing else: a record that
	// was never admitted to governance stays, even if it matches the query the
	// caller used to resolve its set.
	if err := f.p.DeleteExact(owner("a1"), []string{a.ID, b.ID}); err != nil {
		t.Fatalf("exact delete: %v", err)
	}
	if _, err := f.p.Get(owner("a1"), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a must be deleted: %v", err)
	}
	if _, err := f.p.Get(owner("a1"), b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b must be deleted: %v", err)
	}
	if _, err := f.p.Get(owner("a1"), c.ID); err != nil {
		t.Fatalf("an unadmitted record must survive: %v", err)
	}
}

func TestDeleteExactRefusesAWhollyUnavailableSet(t *testing.T) {
	f := newFixture(t)
	a, _ := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k1", Value: "v"})
	b, _ := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k2", Value: "v"})
	// One admitted target vanishes before the mutation runs.
	if err := f.p.Delete(owner("a1"), b.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.p.DeleteExact(owner("a1"), []string{a.ID, b.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a shrunk admitted set must fail closed, got %v", err)
	}
	if _, err := f.p.Get(owner("a1"), a.ID); err != nil {
		t.Fatalf("the whole set must be refused, not partially deleted: %v", err)
	}
	// An unauthorized member is refused the same way.
	if err := f.p.DeleteExact(owner("a2"), []string{a.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another agent must not delete an admitted id: %v", err)
	}
}

func TestDeleteExactUnderConcurrentWritesRemovesOnlyItsOwnSet(t *testing.T) {
	f := newFixture(t)
	id := owner("a1")
	stop := make(chan struct{})
	var (
		mu    sync.Mutex
		live  []string
		wg    sync.WaitGroup
		turns = make(chan struct{}, 1)
	)
	// Writers keep adding records to the same business while deletions run. None
	// of them is ever named in an admitted set, so none may ever disappear.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				select {
				case <-stop:
					return
				default:
				}
				rec, err := f.p.Write(id, WriterUser, Candidate{Key: fmt.Sprintf("live-%d-%d", w, i), Value: "v"})
				if err != nil {
					return
				}
				mu.Lock()
				live = append(live, rec.ID)
				mu.Unlock()
			}
		}(w)
	}
	for round := 0; round < 100; round++ {
		admitted, err := f.p.Write(id, WriterUser, Candidate{Key: "doomed", Value: "v"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.p.DeleteExact(id, []string{admitted.ID}); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		select {
		case turns <- struct{}{}:
		default:
		}
	}
	close(stop)
	wg.Wait()
	mu.Lock()
	survivors := append([]string(nil), live...)
	mu.Unlock()
	for _, rid := range survivors {
		if _, err := f.p.Get(id, rid); err != nil {
			t.Fatalf("a record outside the admitted set was removed (%s): %v", rid, err)
		}
	}
}

// ---------- §9 versioning ----------

func TestVersioningRejectsStaleUpdates(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.p.Update(owner("a1"), rec.ID, rec.Version, Candidate{Value: "v2"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != rec.Version+1 || updated.Value != "v2" {
		t.Fatalf("an update must bump the version: %+v", updated)
	}
	if !updated.CreatedAt.Equal(rec.CreatedAt) {
		t.Fatalf("an update must preserve created_at")
	}
	// A stale write is refused and the stored record is untouched.
	if _, err := f.p.Update(owner("a1"), rec.ID, rec.Version, Candidate{Value: "v3"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale update must conflict: %v", err)
	}
	current, _ := f.p.Get(owner("a1"), rec.ID)
	if current.Value != "v2" {
		t.Fatalf("a stale update must not overwrite: %+v", current)
	}
	// Identity fields are immutable.
	if _, err := f.p.Update(owner("a1"), rec.ID, current.Version, Candidate{Value: "v4", Scope: ScopeBusiness}); !errors.Is(err, ErrValidation) {
		t.Fatalf("scope must be immutable: %v", err)
	}
	if _, err := f.p.Update(owner("a1"), rec.ID, current.Version, Candidate{Value: "v4", Key: "other"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("key must be immutable: %v", err)
	}
}

func TestConcurrentUpdateDoesNotSilentlyOverwrite(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "base"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.p.Update(owner("a1"), rec.ID, rec.Version, Candidate{Value: "writer-a"})
	if err != nil {
		t.Fatal(err)
	}
	// writer-b still holds version 1.
	_, err = f.p.Update(owner("a1"), rec.ID, rec.Version, Candidate{Value: "writer-b"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("the second writer must conflict, got %v", err)
	}
	final, _ := f.p.Get(owner("a1"), rec.ID)
	if final.Value != "writer-a" || final.Version != first.Version {
		t.Fatalf("the loser must not have overwritten the winner: %+v", final)
	}
}

// ---------- §5 bounds ----------

func TestContentBoundsAreExplicit(t *testing.T) {
	f := newFixture(t)
	big := strings.Repeat("x", DefaultBounds().MaxValueBytes+1)
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "big", Value: big}); !errors.Is(err, ErrValidation) {
		t.Fatalf("an oversized value must be rejected: %v", err)
	}
	meta := map[string]string{}
	for i := 0; i < DefaultBounds().MaxMetadataFields+1; i++ {
		meta[fmt.Sprintf("k%d", i)] = "v"
	}
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "m", Value: "v", Metadata: meta}); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many metadata fields must be rejected: %v", err)
	}
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "m2", Value: "v",
		Metadata: map[string]string{"k": strings.Repeat("y", DefaultBounds().MaxMetadataBytes+1)}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("an oversized metadata value must be rejected: %v", err)
	}
}

func TestRecordCountBoundPerScope(t *testing.T) {
	ms := memberships(t)
	st := store.NewMemStore()
	p, err := Open(st, Options{
		Scopes: NewMembershipScopes(ms.AllowsScope),
		Bounds: Bounds{MaxValueBytes: 1024, MaxMetadataFields: 4, MaxMetadataBytes: 64,
			MaxRecordsPerScope: 2, MaxQueryResults: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := p.Write(owner("a1"), WriterAgent, Candidate{Key: fmt.Sprintf("k%d", i), Value: "v"}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if _, err := p.Write(owner("a1"), WriterAgent, Candidate{Key: "k2", Value: "v"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("the record-count bound must be enforced: %v", err)
	}
	// Updating an existing record is still allowed at the bound.
	if _, err := p.Write(owner("a1"), WriterAgent, Candidate{Key: "k0", Value: "v2"}); err != nil {
		t.Fatalf("an update at the bound must be allowed: %v", err)
	}
}

func TestQueryBoundIsClampedToThePlatformLimit(t *testing.T) {
	ms := memberships(t)
	p, err := Open(store.NewMemStore(), Options{
		Scopes: NewMembershipScopes(ms.AllowsScope),
		Bounds: Bounds{MaxValueBytes: 1024, MaxMetadataFields: 4, MaxMetadataBytes: 64,
			MaxRecordsPerScope: 50, MaxQueryResults: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := p.Write(owner("a1"), WriterAgent, Candidate{Key: fmt.Sprintf("k%d", i), Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := p.Query(owner("a1"), Query{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 3 || !res.Truncated || res.Dropped != 2 {
		t.Fatalf("the platform query bound must win over the requested limit: %+v", res)
	}
}

func TestQueryResultIsBounded(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 12; i++ {
		if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: fmt.Sprintf("k%02d", i), Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := f.p.Query(owner("a1"), Query{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 5 || !res.Truncated || res.Dropped != 7 {
		t.Fatalf("the query must be bounded and honest about the drop: %d dropped=%d truncated=%v",
			len(res.Records), res.Dropped, res.Truncated)
	}
	// A limit above the platform bound is clamped, never honoured.
	res, err = f.p.Query(owner("a1"), Query{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 12 || res.Truncated {
		t.Fatalf("below the bound nothing is dropped, got %d truncated=%v", len(res.Records), res.Truncated)
	}
}

// ---------- §6 provenance and trust ----------

func TestProvenanceIsPlatformOwned(t *testing.T) {
	f := newFixture(t)
	agent, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "k", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Source != SourceValidatedAgentOutput || agent.Trust != TrustUnverified {
		t.Fatalf("an agent write must be unverified agent output: %+v", agent)
	}
	user, _ := f.p.Write(owner(""), WriterUser, Candidate{Key: "k2", Value: "v"})
	if user.Source != SourceUserInstruction || user.Trust != TrustExplicit {
		t.Fatalf("a user write must be explicit: %+v", user)
	}
	sys, _ := f.p.Write(owner(""), WriterSystem, Candidate{Key: "k3", Value: "v"})
	if sys.Source != SourceSystemRecord || sys.Trust != TrustValidated {
		t.Fatalf("a system write must be validated: %+v", sys)
	}
	obs, _ := f.p.Write(owner("a1"), WriterObservation, Candidate{Key: "k4", Value: "observed",
		Type: TypeObservation, Outcome: "unknown", Attempts: 1, ReconciliationRequired: true})
	if obs.Source != SourceToolObservation || obs.Trust != TrustObserved {
		t.Fatalf("a promoted observation must be observed: %+v", obs)
	}
	if obs.Outcome != "unknown" || !obs.ReconciliationRequired {
		t.Fatalf("the reliability outcome must survive promotion: %+v", obs)
	}
	// An unknown writer kind is refused: provenance is never invented.
	if _, err := f.p.Write(owner("a1"), WriterKind("model"), Candidate{Key: "k5", Value: "v"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("an unknown writer must be refused: %v", err)
	}
	// An agent cannot create observation memory directly.
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "k6", Value: "v", Type: TypeObservation}); !errors.Is(err, ErrPermission) {
		t.Fatalf("an agent must not forge observation provenance: %v", err)
	}
}

// ---------- §16 redaction ----------

func TestSecretsNeverEnterDurableMemory(t *testing.T) {
	f := newFixture(t)
	const secret = "tok-abcdef-123456"
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{
		Key: "k", Value: "the bearer token is " + secret + " ok",
		Metadata: map[string]string{"token": secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Value, secret) || strings.Contains(rec.Metadata["token"], secret) {
		t.Fatalf("a secret must never be persisted: %+v", rec)
	}
	if !strings.Contains(rec.Value, "[redacted]") {
		t.Fatalf("redaction must be visible in the stored value: %q", rec.Value)
	}
	// And it never leaves through a read.
	got, _ := f.p.Get(owner("a1"), rec.ID)
	if strings.Contains(got.Value, secret) {
		t.Fatalf("a secret must never be returned: %q", got.Value)
	}
	// Nor through events.
	for _, e := range f.bus.events {
		for k, v := range e.Fields {
			if strings.Contains(v, secret) {
				t.Fatalf("event %s field %s leaked a secret: %q", e.Type, k, v)
			}
		}
	}
}

// ---------- §8 conflicts ----------

func TestConflictingMemoriesAreMarkedNotResolved(t *testing.T) {
	f := newFixture(t)
	first, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "endpoint", Value: "X", Subject: "api-endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Conflict {
		t.Fatalf("the first record cannot conflict with itself: %+v", first)
	}
	second, err := f.p.Write(owner("a2"), WriterUser, Candidate{Key: "endpoint-alt", Value: "Y", Subject: "api-endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Conflict || len(second.ConflictWith) != 1 || second.ConflictWith[0] != first.ID {
		t.Fatalf("the second record must be marked as conflicting: %+v", second)
	}
	marked, _ := f.p.Get(owner("a1"), first.ID)
	if !marked.Conflict || len(marked.ConflictWith) != 1 || marked.ConflictWith[0] != second.ID {
		t.Fatalf("the conflict marker must be symmetric: %+v", marked)
	}
	if f.bus.count(EventConflict) == 0 {
		t.Fatalf("a conflict must be audited: %v", f.bus.types())
	}
	// Both values remain readable: memory marks the disagreement, it does not
	// invent a winner.
	res, _ := f.p.Query(owner("a1"), Query{})
	if len(res.Records) != 1 {
		t.Fatalf("agent scope must keep the records apart: %d", len(res.Records))
	}
}

// ---------- §11 retrieval and ranking ----------

func TestRankingIsDeterministicAndTotal(t *testing.T) {
	f := newFixture(t)
	// Two records with the same key in different scopes.
	if _, err := f.p.Write(owner(""), WriterUser, Candidate{Key: "same", Value: "business", Scope: ScopeBusiness}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "same", Value: "agent"}); err != nil {
		t.Fatal(err)
	}
	first, err := f.p.Query(owner("a1"), Query{})
	if err != nil {
		t.Fatal(err)
	}
	// Agent scope ranks before business scope.
	if len(first.Records) != 2 || first.Records[0].Scope != ScopeAgent {
		t.Fatalf("narrower scope must rank first: %+v", first.Records)
	}
	// Same query, same state, same order — repeatedly.
	for i := 0; i < 5; i++ {
		again, _ := f.p.Query(owner("a1"), Query{})
		for j := range again.Records {
			if again.Records[j].ID != first.Records[j].ID {
				t.Fatalf("ranking must be deterministic: run %d differs at %d", i, j)
			}
		}
	}
	// Trust ordering: explicit before unverified at the same scope.
	if _, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "trust-order", Value: "explicit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "trust-order-2", Value: "unverified"}); err != nil {
		t.Fatal(err)
	}
	res, _ := f.p.Query(owner("a1"), Query{Scope: ScopeAgent})
	if len(res.Records) < 2 || res.Records[0].Trust != TrustExplicit {
		t.Fatalf("higher trust must rank first: %+v", res.Records)
	}
}

func TestQueryFilters(t *testing.T) {
	f := newFixture(t)
	_, _ = f.p.Write(owner("a1"), WriterUser, Candidate{Key: "alpha", Value: "needle here", Type: TypeFact})
	_, _ = f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "beta", Value: "other", Type: TypePreference})
	_, _ = f.p.Write(owner("a1"), WriterSystem, Candidate{Key: "gamma", Value: "system value", Type: TypeFact})
	id := owner("a1")
	for _, tc := range []struct {
		name  string
		q     Query
		wantK string
	}{
		{"by key", Query{Key: "beta"}, "beta"},
		{"by type", Query{Type: TypePreference}, "beta"},
		{"by source", Query{Source: SourceSystemRecord}, "gamma"},
		{"by trust", Query{Trust: TrustExplicit}, "alpha"},
		{"by terms", Query{Terms: "needle"}, "alpha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := f.p.Query(id, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Records) != 1 || res.Records[0].Key != tc.wantK {
				t.Fatalf("%s: got %+v", tc.name, res.Records)
			}
		})
	}
}

func TestKeysListsVisibleRecordsOnly(t *testing.T) {
	f := newFixture(t)
	_, _ = f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "b", Value: "v"})
	_, _ = f.p.Write(owner("a1"), WriterAgent, Candidate{Key: "a", Value: "v"})
	keys, err := f.p.Keys(owner("a1"))
	if err != nil || len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("keys must be deterministic and scoped: %v %v", keys, err)
	}
	if keys, err := f.p.Keys(owner("a2")); err != nil || len(keys) != 0 {
		t.Fatalf("another agent must see no keys: %v %v", keys, err)
	}
}

// ---------- §16 events ----------

func TestMutationEventsAreMetadataOnly(t *testing.T) {
	f := newFixture(t)
	rec, err := f.p.Write(owner("a1"), WriterUser, Candidate{Key: "k", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.p.Query(owner("a1"), Query{})
	_, _ = f.p.Get(owner("a1"), rec.ID)
	seen := map[string]bool{}
	for _, e := range f.bus.events {
		seen[e.Type] = true
		if e.BusinessID != "biz-1" {
			t.Fatalf("every memory event must carry its scope: %+v", e)
		}
		for k, v := range e.Fields {
			if k == "value" || k == "content" {
				t.Fatalf("events must never carry memory content: %+v", e.Fields)
			}
			if v == "v" {
				t.Fatalf("event %s leaked the record value in field %s", e.Type, k)
			}
		}
	}
	for _, want := range []string{EventCreated, EventRetrieved} {
		if !seen[want] {
			t.Fatalf("missing %s in %v", want, f.bus.types())
		}
	}
}

// ---------- concurrency ----------

func TestConcurrentWritesAreSerialized(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = f.p.Write(owner("a1"), WriterAgent, Candidate{
				Key: fmt.Sprintf("k%d", i), Value: "v"})
		}(i)
	}
	wg.Wait()
	res, err := f.p.Query(owner("a1"), Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 8 {
		t.Fatalf("every concurrent write must land exactly once, got %d", len(res.Records))
	}
	ids := map[string]bool{}
	for _, r := range res.Records {
		if ids[r.ID] {
			t.Fatalf("duplicate record id %s", r.ID)
		}
		ids[r.ID] = true
	}
}
