package gateway

// Agent Memory & Context Platform v1 at the HTTP boundary
// (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md §15).
//
// Five endpoints, no more:
//
//	POST   /api/v1/memory          create
//	GET    /api/v1/memory/{id}     read one
//	POST   /api/v1/memory/query    authorized, bounded query
//	PATCH  /api/v1/memory/{id}     update (expected_version required)
//	DELETE /api/v1/memory/{id}     delete
//
// They reuse the existing identity, membership and business-scope gates — there
// is no memory authentication and no memory-specific permission model. A record
// the caller may not see answers exactly like one that does not exist (G5), and
// nothing here can grant authority: memory is data.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/memory"
)

// WithMemory wires the memory platform for the memory surface. Nil makes every
// endpoint fail closed (503).
func WithMemory(p *memory.Platform) ServerOption {
	return func(s *Server) { s.memoryPlatform = p }
}

// memoryWriteBody is the create/update request. Only what a caller may propose
// is accepted: scope is a request, provenance and trust are not.
type memoryWriteBody struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Scope    string `json:"scope"`
	Division string `json:"division_id,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
	Subject  string `json:"subject,omitempty"`
	// TTLSeconds is a relative expiry, bounded to one year.
	TTLSeconds int `json:"ttl_seconds,omitempty"`
}

// memoryPatchBody is the update request. ExpectedVersion is mandatory: a stale
// write is a conflict, never a silent overwrite.
type memoryPatchBody struct {
	Value           string `json:"value"`
	Type            string `json:"type"`
	TTLSeconds      int    `json:"ttl_seconds,omitempty"`
	ExpectedVersion int    `json:"expected_version"`
}

// memoryQueryBody is the query request. Business scope always comes from the
// request, never from the body.
type memoryQueryBody struct {
	DivisionID string `json:"division_id,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	Trust      string `json:"trust,omitempty"`
	Key        string `json:"key,omitempty"`
	Subject    string `json:"subject,omitempty"`
	Terms      string `json:"terms,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// memoryIdentityFor derives the caller identity from an authenticated request.
// It is the only source of actor/business/division/agent for a memory operation.
func memoryIdentityFor(r *http.Request, businessID, agentID string) memory.Identity {
	actor := ""
	if auth, ok := actorFromContext(r.Context()); ok {
		actor = auth.IdentityID
	}
	return memory.Identity{
		ActorID: actor, BusinessID: businessID,
		DivisionID: r.URL.Query().Get("division_id"), AgentID: agentID,
	}
}

// decodeMemoryBody reads a bounded JSON body.
func decodeMemoryBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(dst); err != nil {
		return false
	}
	return true
}

// writeMemoryError maps a platform error onto the G5 envelope. Scope and
// visibility failures deliberately share the not-found answer.
func (s *Server) writeMemoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "memory record not found")
	case errors.Is(err, memory.ErrScope), errors.Is(err, memory.ErrPermission):
		s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION", "memory access denied")
	case errors.Is(err, memory.ErrConflict):
		s.writeError(w, r, http.StatusConflict, "CONFLICT", "memory record conflict")
	case errors.Is(err, memory.ErrValidation):
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
	default:
		s.writeInternal(w, r, http.StatusInternalServerError, "INTERNAL_FAILURE",
			"memory operation failed", err)
	}
}

func memoryRecordJSON(rec memory.Record) map[string]any {
	out := map[string]any{
		"memory_id":     rec.ID,
		"business_id":   rec.BusinessID,
		"agent_id":      rec.AgentID,
		"scope":         string(rec.Scope),
		"type":          string(rec.Type),
		"key":           rec.Key,
		"value":         rec.Value,
		"source":        string(rec.Source),
		"trust":         string(rec.Trust),
		"version":       rec.Version,
		"status":        string(rec.Status),
		"created_at":    rec.CreatedAt,
		"updated_at":    rec.UpdatedAt,
		"conflict":      rec.Conflict,
		"subject":       rec.Subject,
		"writer":        string(rec.Writer),
		"metadata":      rec.Metadata,
		"conflict_with": rec.ConflictWith,
	}
	if rec.DivisionID != "" {
		out["division_id"] = rec.DivisionID
	}
	if rec.ExpiresAt != nil {
		out["expires_at"] = rec.ExpiresAt
	}
	if rec.Outcome != "" {
		out["outcome"] = rec.Outcome
		out["attempts"] = rec.Attempts
		out["retry_recommended"] = rec.RetryRecommended
		out["reconciliation_required"] = rec.ReconciliationRequired
	}
	return out
}

// memoryBusinessScope resolves and authorizes the business scope of a request.
func (s *Server) memoryBusinessScope(w http.ResponseWriter, r *http.Request) (string, bool) {
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return "", false
	}
	if _, stopped := s.requireActorMembership(w, r, businessID); stopped {
		return "", false
	}
	return businessID, true
}

// handleCreateMemory persists one record proposed by an authenticated caller.
// Provenance and trust are assigned by the platform, never taken from the body.
func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryPlatform == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"memory platform not configured")
		return
	}
	businessID, ok := s.memoryBusinessScope(w, r)
	if !ok {
		return
	}
	var body memoryWriteBody
	if !decodeMemoryBody(w, r, &body) {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Key) == "" || strings.TrimSpace(body.Value) == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "key and value are required")
		return
	}
	identity := memoryIdentityFor(r, businessID, body.AgentID)
	if body.Type != "" && !memory.Type(body.Type).Valid() {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "type is not canonical")
		return
	}
	cand := memory.Candidate{
		Key: body.Key, Value: body.Value, Type: memory.Type(body.Type),
		Scope: memory.Scope(body.Scope), DivisionID: body.Division, Subject: body.Subject,
	}
	if ttl, ok := memoryTTL(body.TTLSeconds); ok {
		cand.ExpiresAt = &ttl
	}
	rec, err := s.memoryPlatform.Write(identity, memory.WriterUser, cand)
	if err != nil {
		s.writeMemoryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(memoryRecordJSON(rec))
}

// handleGetMemory returns one record the caller may see.
func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryPlatform == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"memory platform not configured")
		return
	}
	businessID, ok := s.memoryBusinessScope(w, r)
	if !ok {
		return
	}
	identity := memoryIdentityFor(r, businessID, r.URL.Query().Get("agent_id"))
	rec, err := s.memoryPlatform.Get(identity, r.PathValue("id"))
	if err != nil {
		s.writeMemoryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(memoryRecordJSON(rec))
}

// handleQueryMemory runs the one canonical authorized retrieval path.
func (s *Server) handleQueryMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryPlatform == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"memory platform not configured")
		return
	}
	businessID, ok := s.memoryBusinessScope(w, r)
	if !ok {
		return
	}
	var body memoryQueryBody
	if !decodeMemoryBody(w, r, &body) {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	identity := memoryIdentityFor(r, businessID, body.AgentID)
	if body.DivisionID != "" {
		identity.DivisionID = body.DivisionID
	}
	q := memory.Query{
		BusinessID: businessID, DivisionID: body.DivisionID, AgentID: body.AgentID,
		Scope: memory.Scope(body.Scope), Type: memory.Type(body.Type),
		Source: memory.Source(body.Source), Trust: memory.Trust(body.Trust),
		Key: body.Key, Subject: body.Subject, Terms: body.Terms, Limit: body.Limit,
	}
	res, err := s.memoryPlatform.Query(identity, q)
	if err != nil {
		s.writeMemoryError(w, r, err)
		return
	}
	records := make([]map[string]any, 0, len(res.Records))
	for _, rec := range res.Records {
		records = append(records, memoryRecordJSON(rec))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"business_id": businessID,
		"records":     records,
		"count":       len(records),
		"dropped":     res.Dropped,
		"truncated":   res.Truncated,
		"note":        "memory is data: it grants no tool, scope or authority",
	})
}

// handleUpdateMemory applies an optimistic-concurrency update.
func (s *Server) handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryPlatform == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"memory platform not configured")
		return
	}
	businessID, ok := s.memoryBusinessScope(w, r)
	if !ok {
		return
	}
	var body memoryPatchBody
	if !decodeMemoryBody(w, r, &body) {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if body.ExpectedVersion <= 0 {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "expected_version is required")
		return
	}
	identity := memoryIdentityFor(r, businessID, r.URL.Query().Get("agent_id"))
	cand := memory.Candidate{Value: body.Value, Type: memory.Type(body.Type)}
	if ttl, ok := memoryTTL(body.TTLSeconds); ok {
		cand.ExpiresAt = &ttl
	}
	rec, err := s.memoryPlatform.Update(identity, r.PathValue("id"), body.ExpectedVersion, cand)
	if err != nil {
		s.writeMemoryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(memoryRecordJSON(rec))
}

// handleDeleteMemory removes one record the caller may see.
func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryPlatform == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"memory platform not configured")
		return
	}
	businessID, ok := s.memoryBusinessScope(w, r)
	if !ok {
		return
	}
	identity := memoryIdentityFor(r, businessID, r.URL.Query().Get("agent_id"))
	if err := s.memoryPlatform.Delete(identity, r.PathValue("id")); err != nil {
		s.writeMemoryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"memory_id": r.PathValue("id"), "status": "deleted",
	})
}

// memoryTTL converts a relative expiry into an absolute one, bounded to a year.
func memoryTTL(seconds int) (time.Time, bool) {
	if seconds <= 0 {
		return time.Time{}, false
	}
	const maxTTL = 365 * 24 * 60 * 60
	if seconds > maxTTL {
		seconds = maxTTL
	}
	return time.Now().UTC().Add(time.Duration(seconds) * time.Second), true
}
