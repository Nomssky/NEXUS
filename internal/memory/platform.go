package memory

// The durable memory platform (contract §3–§10): one store, authorized before
// retrieval, bounded on every write, versioned, explicitly deletable.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Event type names, emitted on the existing bus (contract §16).
const (
	EventCreated   = "memory.created"
	EventUpdated   = "memory.updated"
	EventDeleted   = "memory.deleted"
	EventExpired   = "memory.expired"
	EventRetrieved = "memory.retrieved"
	EventConflict  = "memory.conflict"
)

// Platform is the single durable agent-memory store.
type Platform struct {
	mu     sync.RWMutex
	st     store.Store
	now    func() time.Time
	bounds Bounds
	scopes ScopeChecker
	redact Redactor
	bus    EventSink
}

// New returns a platform over st. With a nil store the platform is
// process-local and durable writes are no-ops that still validate and emit.
func New(st store.Store, opts Options) *Platform {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	bounds := opts.Bounds
	if d := DefaultBounds(); bounds.MaxValueBytes == 0 {
		bounds = d
	}
	return &Platform{st: st, now: now, bounds: bounds, scopes: opts.Scopes,
		redact: opts.Redact, bus: opts.Bus}
}

// Open hydrates a durable store fail-closed, like the org registry: a corrupt or
// out-of-contract record aborts boot rather than running on partial memory.
func Open(st store.Store, opts Options) (*Platform, error) {
	p := New(st, opts)
	if st == nil {
		return p, nil
	}
	if err := p.hydrate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Bounds returns the effective limits.
func (p *Platform) Bounds() Bounds { return p.bounds }

func (p *Platform) nowTime() time.Time { return p.now() }

func (p *Platform) hydrate() error {
	recs, err := p.st.List(store.Filter{Type: store.RecordTypeMemory})
	if err != nil {
		return fmt.Errorf("memory: hydrate: %w", err)
	}
	for _, rec := range recs {
		var r Record
		if err := json.Unmarshal(rec.Data, &r); err != nil {
			return fmt.Errorf("memory: corrupt record %q: %w", rec.ID, err)
		}
		if err := p.validateStored(&r, rec.ID); err != nil {
			return err
		}
	}
	return nil
}

// validateStored enforces the invariants a persisted record must always hold.
func (p *Platform) validateStored(r *Record, gotID string) error {
	if r.Key == "" || r.BusinessID == "" {
		return fmt.Errorf("memory: invalid record %q", gotID)
	}
	if !r.Scope.Valid() || !r.Type.Valid() || !r.Trust.Valid() || r.Source == "" {
		return fmt.Errorf("memory: record %q has non-canonical scope/type/source/trust", gotID)
	}
	if len(r.Value) > p.bounds.MaxValueBytes {
		return fmt.Errorf("memory: record %q exceeds the value bound", gotID)
	}
	if p.idFor(r) != gotID {
		return fmt.Errorf("memory: record %q carries key %q at scope %q", gotID, r.Key, r.Scope)
	}
	return nil
}

// idFor derives the canonical id of a record from its scope, never from content.
func (p *Platform) idFor(r *Record) string {
	agent := "_"
	if r.Scope == ScopeAgent {
		agent = r.AgentID
		if agent == "" {
			agent = "_"
		}
	}
	return memoryID(r.BusinessID, r.DivisionID, agent, r.Key)
}

// ---------- authorization ----------

// authorize ensures the caller may act for its business/division at all. It is
// the canonical membership check, run BEFORE any store read (contract §4).
func (p *Platform) authorize(id Identity, divisionID string) error {
	if strings.TrimSpace(id.ActorID) == "" || strings.TrimSpace(id.BusinessID) == "" {
		return fmt.Errorf("%w: actor and business are required", ErrScope)
	}
	if p.scopes == nil {
		return fmt.Errorf("%w: no scope authority configured", ErrScope)
	}
	if !p.scopes.AllowsScope(id.ActorID, id.BusinessID, divisionID) {
		return fmt.Errorf("%w: actor is not a member of this business scope", ErrScope)
	}
	return nil
}

// visible reports whether a stored record may be returned to this caller.
func (p *Platform) visible(r *Record, id Identity) bool {
	if r.BusinessID != id.BusinessID {
		return false
	}
	if r.Scope == ScopeDivision && r.DivisionID != id.DivisionID {
		// A division record needs the caller to hold that division's scope.
		return p.scopes != nil && p.scopes.AllowsScope(id.ActorID, r.BusinessID, r.DivisionID)
	}
	if r.Scope == ScopeAgent && r.AgentID != id.AgentID {
		return false // agent memory is private to its agent
	}
	return true
}

// active reports whether a record may be returned by normal retrieval: it must
// be active and unexpired (contract §10).
func (p *Platform) active(r *Record, now time.Time) bool {
	if r.Status != StatusActive {
		return false
	}
	return !(r.ExpiresAt != nil && now.After(*r.ExpiresAt))
}

// writableScope clamps a requested scope to what the caller may create, using
// the acting agent's declared memory mode. It can only narrow.
func (p *Platform) writableScope(id Identity, kind WriterKind, requested Scope, divisionID string) (Scope, string, error) {
	scope := requested
	if scope == "" {
		// The default is the narrowest scope that still makes sense: an agent
		// writes to its own memory, a non-agent caller to business memory.
		if kind == WriterAgent || id.AgentID != "" {
			scope = ScopeAgent
		} else {
			scope = ScopeBusiness
		}
	}
	if !scope.Valid() {
		return "", "", fmt.Errorf("%w: scope %q is not canonical", ErrValidation, requested)
	}
	if scope == ScopeDivision {
		if strings.TrimSpace(divisionID) == "" {
			return "", "", fmt.Errorf("%w: division scope requires a division", ErrValidation)
		}
		if divisionID != id.DivisionID && kind == WriterAgent {
			// An agent may not write into a division it is not acting for.
			return "", "", fmt.Errorf("%w: agent may only write in its own division", ErrPermission)
		}
	}
	if kind == WriterAgent {
		switch strings.ToLower(strings.TrimSpace(id.AgentMemoryMode)) {
		case "none":
			return "", "", fmt.Errorf("%w: this agent's memory mode is none", ErrPermission)
		case "division":
			if scope == ScopeBusiness {
				return "", "", fmt.Errorf("%w: this agent may only write division or agent memory", ErrPermission)
			}
		}
	}
	return scope, divisionID, nil
}

// ---------- writes ----------

// Write creates or updates one record from a validated candidate (contract §7).
// The writer kind — not the payload — decides provenance and trust.
func (p *Platform) Write(id Identity, kind WriterKind, c Candidate) (Record, error) {
	source, trust, ok := ProvenanceOf(kind)
	if !ok {
		return Record{}, fmt.Errorf("%w: unknown writer %q", ErrValidation, kind)
	}
	if err := p.authorize(id, divisionFor(id, c)); err != nil {
		return Record{}, err
	}
	scope, divisionID, err := p.writableScope(id, kind, c.Scope, strings.TrimSpace(c.DivisionID))
	if err != nil {
		return Record{}, err
	}
	if err := p.authorize(id, divisionForScope(scope, divisionID)); err != nil {
		return Record{}, err
	}
	key := normalizeKey(c.Key)
	if key == "" {
		return Record{}, fmt.Errorf("%w: key is required", ErrValidation)
	}
	mtype := c.Type
	if mtype == "" {
		mtype = TypeFact
	}
	if !mtype.Valid() {
		return Record{}, fmt.Errorf("%w: type %q is not canonical", ErrValidation, c.Type)
	}
	if mtype == TypeObservation && kind != WriterObservation && kind != WriterSystem {
		return Record{}, fmt.Errorf("%w: observation memory requires an observation or system writer", ErrPermission)
	}
	value, redacted := p.redactValue(c.Value)
	if value == "" {
		return Record{}, fmt.Errorf("%w: value is required", ErrValidation)
	}
	if len(value) > p.bounds.MaxValueBytes {
		return Record{}, fmt.Errorf("%w: value exceeds %d bytes", ErrValidation, p.bounds.MaxValueBytes)
	}
	meta, metaRedacted, err := p.redactMetadata(c.Metadata)
	if err != nil {
		return Record{}, err
	}
	redacted = redacted || metaRedacted
	if c.ExpiresAt != nil && !c.ExpiresAt.After(p.nowTime()) {
		return Record{}, fmt.Errorf("%w: expires_at must be in the future", ErrValidation)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.nowTime()
	rec := Record{
		BusinessID: id.BusinessID, DivisionID: divisionID, AgentID: id.AgentID,
		Scope: scope, Type: mtype, Key: key, Value: value,
		Subject: normalizeSubject(c.Subject, key), Source: source, Trust: trust, Writer: kind,
		Metadata: meta, CreatedAt: now, UpdatedAt: now, ExpiresAt: c.ExpiresAt,
		Status:  StatusActive,
		Outcome: c.Outcome, Attempts: c.Attempts,
		RetryRecommended: c.RetryRecommended, ReconciliationRequired: c.ReconciliationRequired,
	}
	if c.ObservationID != "" {
		rec.Metadata = withMeta(rec.Metadata, "observation_id", c.ObservationID)
	}
	rec.ID = p.idFor(&rec)

	// Existing record: preserve identity fields and created_at, bump version.
	var prev *store.Record
	if p.st != nil {
		existing, err := p.st.Get(rec.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Record{}, fmt.Errorf("memory: read before write %q: %w", rec.ID, err)
		}
		prev = existing
		if prev == nil {
			// Get answers NotFound for a soft-deleted record too. Recover the
			// deleted envelope so re-creating a key is not mistaken for a
			// concurrent modification (contract §10).
			prev = p.deletedEnvelope(rec.ID, id.BusinessID)
		}
	}
	created := true
	if prev != nil {
		var old Record
		if err := json.Unmarshal(prev.Data, &old); err != nil {
			return Record{}, fmt.Errorf("memory: corrupt record %q: %w", rec.ID, err)
		}
		if err := p.validateStored(&old, rec.ID); err != nil {
			return Record{}, err
		}
		if prev.Status == store.RecordStatusDeleted {
			// Re-creating a deleted key starts a new record: the deleted one is
			// not resurrected (contract §10).
			old = Record{}
		}
		created = prev.Status == store.RecordStatusDeleted
		rec.CreatedAt = old.CreatedAt
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if old.Conflict {
			rec.Conflict, rec.ConflictWith = old.Conflict, append([]string(nil), old.ConflictWith...)
		}
	}
	if created {
		if err := p.checkRecordCount(rec); err != nil {
			return Record{}, err
		}
	}

	stored, err := p.persist(&rec, prevVersion(prev))
	if err != nil {
		return Record{}, err
	}
	peers := p.markConflicts(stored)
	evType := EventCreated
	if prev != nil && !created {
		evType = EventUpdated
	}
	p.emit(evType, *stored, map[string]string{
		"type": string(stored.Type), "scope": string(stored.Scope), "source": string(stored.Source),
		"trust": string(stored.Trust), "version": fmt.Sprint(stored.Version),
		"redacted": fmt.Sprint(redacted), "conflict": fmt.Sprint(stored.Conflict),
	})
	if len(peers) > 0 {
		p.emit(EventConflict, *stored, map[string]string{
			"subject": stored.Subject, "peers": fmt.Sprint(len(peers)),
		})
	}
	return *stored, nil
}

// Update replaces a record's content under optimistic concurrency (contract §9).
// Identity fields (business, division, scope, agent, key, subject) are immutable.
func (p *Platform) Update(id Identity, memID string, expectedVersion int, c Candidate) (Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, prev, err := p.load(memID)
	if err != nil {
		return Record{}, err
	}
	if !p.visible(rec, id) {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	if expectedVersion <= 0 || expectedVersion != rec.Version {
		return Record{}, fmt.Errorf("%w: expected version %d, stored %d", ErrConflict, expectedVersion, rec.Version)
	}
	if c.Scope != "" && c.Scope != rec.Scope {
		return Record{}, fmt.Errorf("%w: scope is immutable", ErrValidation)
	}
	if strings.TrimSpace(c.DivisionID) != "" && c.DivisionID != rec.DivisionID {
		return Record{}, fmt.Errorf("%w: division is immutable", ErrValidation)
	}
	if normalizeKey(c.Key) != "" && normalizeKey(c.Key) != rec.Key {
		return Record{}, fmt.Errorf("%w: key is immutable", ErrValidation)
	}
	mtype := c.Type
	if mtype == "" {
		mtype = rec.Type
	}
	if !mtype.Valid() {
		return Record{}, fmt.Errorf("%w: type %q is not canonical", ErrValidation, c.Type)
	}
	value, _ := p.redactValue(c.Value)
	if value == "" {
		return Record{}, fmt.Errorf("%w: value is required", ErrValidation)
	}
	if len(value) > p.bounds.MaxValueBytes {
		return Record{}, fmt.Errorf("%w: value exceeds %d bytes", ErrValidation, p.bounds.MaxValueBytes)
	}
	meta, _, err := p.redactMetadata(c.Metadata)
	if err != nil {
		return Record{}, err
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(p.nowTime()) {
		return Record{}, fmt.Errorf("%w: expires_at must be in the future", ErrValidation)
	}
	next := *rec
	next.Type = mtype
	next.Value = value
	next.Metadata = meta
	next.ExpiresAt = c.ExpiresAt
	next.Outcome = c.Outcome
	next.Attempts = c.Attempts
	next.RetryRecommended = c.RetryRecommended
	next.ReconciliationRequired = c.ReconciliationRequired
	next.UpdatedAt = p.nowTime()
	stored, err := p.persist(&next, prevVersion(prev))
	if err != nil {
		return Record{}, err
	}
	peers := p.markConflicts(stored)
	p.emit(EventUpdated, *stored, map[string]string{
		"type": string(stored.Type), "scope": string(stored.Scope), "version": fmt.Sprint(stored.Version),
		"trust": string(stored.Trust), "conflict": fmt.Sprint(stored.Conflict),
	})
	if len(peers) > 0 {
		p.emit(EventConflict, *stored, map[string]string{
			"subject": stored.Subject, "peers": fmt.Sprint(len(peers)),
		})
	}
	return *stored, nil
}

// Delete removes one record the caller may see. The record stops being visible
// to every normal read immediately (contract §10).
func (p *Platform) Delete(id Identity, memID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, prev, err := p.load(memID)
	if err != nil {
		return err
	}
	if !p.visible(rec, id) {
		return fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	if p.st != nil {
		if err := p.st.Delete(memID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("memory: delete %q: %w", memID, err)
		}
	}
	rec.Status = StatusDeleted
	rec.UpdatedAt = p.nowTime()
	_ = prev
	p.emit(EventDeleted, *rec, map[string]string{"scope": string(rec.Scope), "type": string(rec.Type)})
	return nil
}

// Expire sets an expiry on a record the caller may see. Expired records are
// excluded from normal retrieval immediately (contract §10).
func (p *Platform) Expire(id Identity, memID string, at time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, prev, err := p.load(memID)
	if err != nil {
		return err
	}
	if !p.visible(rec, id) {
		return fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	if !at.After(p.nowTime()) {
		return fmt.Errorf("%w: expiry must be in the future", ErrValidation)
	}
	rec.ExpiresAt = &at
	rec.UpdatedAt = p.nowTime()
	if _, err := p.persist(rec, prevVersion(prev)); err != nil {
		return err
	}
	p.emit(EventExpired, *rec, map[string]string{"scope": string(rec.Scope), "type": string(rec.Type)})
	return nil
}

// ---------- reads ----------

// Get returns one record by id, or ErrNotFound — the same answer for records
// that do not exist and records the caller may not see (G5, no existence leak).
func (p *Platform) Get(id Identity, memID string) (Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	rec, _, err := p.load(memID)
	if err != nil {
		return Record{}, err
	}
	if !p.visible(rec, id) {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	return *rec, nil
}

// Query is the canonical retrieval path (contract §11): authorize, then store
// query at the caller's business, then narrow per record, then rank, then bound.
func (p *Platform) Query(id Identity, q Query) (Result, error) {
	if err := p.authorize(id, divisionForQuery(id, q)); err != nil {
		return Result{}, err
	}
	p.mu.RLock()
	recs, err := p.list(q.BusinessID)
	p.mu.RUnlock()
	if err != nil {
		return Result{}, err
	}
	now := p.nowTime()
	out := Result{Scope: q.Scope}
	for _, r := range recs {
		if r.BusinessID != id.BusinessID || !p.visible(&r, id) || !p.active(&r, now) {
			continue
		}
		if !matchesQuery(&r, id, q) {
			continue
		}
		out.Records = append(out.Records, r)
	}
	rank(out.Records, q)
	limit := q.Limit
	if limit <= 0 || limit > p.bounds.MaxQueryResults {
		limit = p.bounds.MaxQueryResults
	}
	if len(out.Records) > limit {
		out.Dropped = len(out.Records) - limit
		out.Records = out.Records[:limit]
		out.Truncated = true
	}
	p.emit(EventRetrieved, Record{BusinessID: id.BusinessID, AgentID: id.AgentID}, map[string]string{
		"returned": fmt.Sprint(len(out.Records)), "dropped": fmt.Sprint(out.Dropped),
		"scope": string(out.Scope), "truncated": fmt.Sprint(out.Truncated),
	})
	return out, nil
}

// Keys lists the active keys visible to the caller at agent scope (the shape the
// intelligence layer used before this milestone).
func (p *Platform) Keys(id Identity) ([]string, error) {
	res, err := p.Query(id, Query{Scope: ScopeAgent, Limit: p.bounds.MaxQueryResults})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(res.Records))
	for _, r := range res.Records {
		keys = append(keys, r.Key)
	}
	sort.Strings(keys)
	return keys, nil
}

// ---------- internals ----------

func (p *Platform) list(businessID string) ([]Record, error) {
	if p.st == nil {
		return nil, nil
	}
	filter := store.Filter{Type: store.RecordTypeMemory}
	if businessID != "" {
		filter.BusinessID = businessID
	}
	recs, err := p.st.List(filter)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		var r Record
		if err := json.Unmarshal(rec.Data, &r); err != nil {
			continue // corrupt records were rejected at hydrate; skip defensively
		}
		out = append(out, r)
	}
	return out, nil
}

// deletedEnvelope finds the soft-deleted envelope of a record id, if any.
func (p *Platform) deletedEnvelope(memID, businessID string) *store.Record {
	recs, err := p.st.List(store.Filter{Type: store.RecordTypeMemory,
		Status: store.RecordStatusDeleted, BusinessID: businessID})
	if err != nil {
		return nil
	}
	for _, rec := range recs {
		if rec.ID == memID {
			return rec
		}
	}
	return nil
}

// load returns one stored record with its store envelope (for versioning).
func (p *Platform) load(memID string) (*Record, *store.Record, error) {
	if p.st == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	rec, err := p.st.Get(memID)
	if err != nil || rec == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, memID)
	}
	var r Record
	if err := json.Unmarshal(rec.Data, &r); err != nil {
		return nil, nil, fmt.Errorf("memory: corrupt record %q: %w", memID, err)
	}
	if err := p.validateStored(&r, memID); err != nil {
		return nil, nil, err
	}
	r.Version = rec.Version
	r.CreatedAt = rec.CreatedAt
	if rec.ExpiresAt != nil && r.ExpiresAt == nil {
		r.ExpiresAt = rec.ExpiresAt
	}
	return &r, rec, nil
}

// persist writes the record through the store's optimistic concurrency.
func (p *Platform) persist(r *Record, prevVersion int) (*Record, error) {
	if p.st == nil {
		r.Version = prevVersion + 1
		return r, nil
	}
	r.Version = prevVersion
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("memory: encode %q: %w", r.ID, err)
	}
	sr := &store.Record{
		ID: r.ID, Type: store.RecordTypeMemory, Status: store.RecordStatusActive,
		BusinessID: r.BusinessID, DivisionID: r.DivisionID, CorrelationID: r.CorrelationID,
		Version: prevVersion, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		ExpiresAt: r.ExpiresAt, Data: data,
	}
	if err := p.st.Put(sr); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: record %q was modified concurrently", ErrConflict, r.ID)
		}
		return nil, fmt.Errorf("memory: persist %q: %w", r.ID, err)
	}
	stored, err := p.st.Get(r.ID)
	if err != nil || stored == nil {
		return nil, fmt.Errorf("memory: persist %q: read-back failed", r.ID)
	}
	var back Record
	if err := json.Unmarshal(stored.Data, &back); err != nil {
		return nil, fmt.Errorf("memory: persist %q: %w", r.ID, err)
	}
	back.Version = stored.Version
	back.CreatedAt = stored.CreatedAt
	return &back, nil
}

// checkRecordCount enforces the per-scope record bound before creating.
func (p *Platform) checkRecordCount(rec Record) error {
	recs, err := p.list(rec.BusinessID)
	if err != nil {
		return err
	}
	now := p.nowTime()
	n := 0
	for _, r := range recs {
		if p.idFor(&r) == rec.ID {
			continue
		}
		if r.Scope == rec.Scope && r.AgentID == rec.AgentID && r.BusinessID == rec.BusinessID {
			if p.active(&r, now) {
				n++
			}
		}
	}
	if n+1 > p.bounds.MaxRecordsPerScope {
		return fmt.Errorf("%w: scope already holds %d records (bound %d)",
			ErrConflict, n, p.bounds.MaxRecordsPerScope)
	}
	return nil
}

// markConflicts marks every active record in the same scope that shares the
// subject and disagrees on value, deterministically (contract §8).
func (p *Platform) markConflicts(rec *Record) []string {
	recs, err := p.list(rec.BusinessID)
	if err != nil {
		return nil
	}
	now := p.nowTime()
	peers := []string{}
	for i := range recs {
		r := recs[i]
		if r.ID == rec.ID || r.Subject != rec.Subject || !p.active(&r, now) {
			continue
		}
		if r.Scope != rec.Scope || r.DivisionID != rec.DivisionID {
			continue
		}
		if r.Value == rec.Value {
			continue
		}
		peers = append(peers, r.ID)
	}
	if len(peers) == 0 {
		return nil
	}
	sort.Strings(peers)
	with := append([]string(nil), peers...)
	rec.Conflict, rec.ConflictWith = true, with
	if stored, err := p.persistNoVersionBump(rec); err == nil {
		*rec = *stored
	}
	// Mark the peers too, so the marker is symmetric and stable.
	for _, pid := range peers {
		peer, prev, err := p.load(pid)
		if err != nil || !peer.Conflict {
			continue
		}
		peer.Conflict = true
		peer.ConflictWith = sortedUnique(append(append([]string(nil), peer.ConflictWith...), rec.ID))
		peer.UpdatedAt = p.nowTime()
		if _, err := p.persist(peer, prevVersion(prev)); err == nil {
			continue
		}
	}
	return peers
}

// persistNoVersionBump re-writes a record that was just persisted, keeping its
// version so the conflict marker is not an update in disguise.
func (p *Platform) persistNoVersionBump(rec *Record) (*Record, error) {
	if p.st == nil {
		return rec, nil
	}
	cur, err := p.st.Get(rec.ID)
	if err != nil || cur == nil {
		return rec, nil
	}
	return p.persist(rec, cur.Version)
}

// redactValue redacts content and reports whether anything was removed.
func (p *Platform) redactValue(v string) (string, bool) {
	if p.redact == nil {
		return v, false
	}
	out := p.redact.Redact(v)
	return out, out != v
}

// redactMetadata bounds and redacts metadata.
func (p *Platform) redactMetadata(in map[string]string) (map[string]string, bool, error) {
	if len(in) == 0 {
		return nil, false, nil
	}
	if len(in) > p.bounds.MaxMetadataFields {
		return nil, false, fmt.Errorf("%w: at most %d metadata fields", ErrValidation, p.bounds.MaxMetadataFields)
	}
	out := make(map[string]string, len(in))
	redacted := false
	for k, v := range in {
		if len(v) > p.bounds.MaxMetadataBytes {
			return nil, false, fmt.Errorf("%w: metadata %q exceeds %d bytes", ErrValidation, k, p.bounds.MaxMetadataBytes)
		}
		r := v
		if p.redact != nil {
			r = p.redact.Redact(v)
			if r != v {
				redacted = true
			}
		}
		out[k] = r
	}
	return out, redacted, nil
}

func withMeta(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

func prevVersion(rec *store.Record) int {
	if rec == nil {
		return 0
	}
	return rec.Version
}

func divisionFor(id Identity, c Candidate) string {
	if strings.TrimSpace(c.DivisionID) != "" {
		return c.DivisionID
	}
	return id.DivisionID
}

func divisionForScope(scope Scope, divisionID string) string {
	if scope == ScopeDivision {
		return divisionID
	}
	return ""
}

func divisionForQuery(id Identity, q Query) string {
	if strings.TrimSpace(q.DivisionID) != "" {
		return q.DivisionID
	}
	return id.DivisionID
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (p *Platform) emit(evType string, rec Record, fields map[string]string) {
	if p.bus == nil {
		return
	}
	merged := map[string]string{"memory_id": rec.ID}
	for k, v := range fields {
		merged[k] = v
	}
	_ = p.bus.Publish(Event{
		Type: evType, BusinessID: rec.BusinessID, DivisionID: rec.DivisionID,
		AgentID: rec.AgentID, CorrelationID: rec.CorrelationID, Fields: merged,
	})
}
