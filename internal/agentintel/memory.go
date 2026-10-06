package agentintel

// Memory wiring for the intelligence loop (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md).
//
// The durable store itself lives in internal/memory — this package only binds it
// to the loop: the scope authority, the shared event bus and the platform
// redactor. There is exactly one durable agent-memory store, one authorized
// retrieval path and one context-assembly boundary; the loop never reaches past
// them.

import (
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// Deps are the dependencies the memory platform needs from the runtime. They are
// the same seams every other layer uses — there is no memory-specific authority.
type Deps struct {
	// Scopes is the canonical scope authority, bound to
	// identity.MembershipSet.AllowsScope. Without it every memory operation
	// fails closed.
	Scopes memory.ScopeChecker
	// Redactor is the platform's mandatory redaction, so a secret cannot
	// survive in durable memory just because another boundary redacted it.
	Redactor memory.Redactor
	// Bus is the existing event bus; memory publishes onto it and creates none.
	Bus *event.MemBus
	// Now is injectable for deterministic tests.
	Now func() time.Time
}

// MemoryPlatform returns the single durable memory platform.
type MemoryPlatform = memory.Platform

// OpenMemory hydrates the durable agent memory, fail-closed: a corrupt or
// out-of-contract record aborts the call rather than running on partial memory
// (contract §4, G4 — memory is durable, executions are not).
func OpenMemory(st store.Store, deps Deps) (*MemoryPlatform, error) {
	opts := memory.Options{
		Now:    deps.Now,
		Scopes: deps.Scopes,
		Redact: deps.Redactor,
	}
	if deps.Bus != nil {
		opts.Bus = memory.NewBusSink(deps.Bus)
	}
	return memory.Open(st, opts)
}

// memoryIdentity builds the runtime-established identity for a memory
// operation. Every field comes from the admitted request or the executing agent;
// none of it comes from the model payload (contract §4, §7).
func (r *Runtime) memoryIdentity(rn *run, agentID string) memory.Identity {
	id := memory.Identity{
		ActorID:    rn.req.ActorID,
		BusinessID: rn.businessID,
		DivisionID: rn.divisionID,
		AgentID:    agentID,
	}
	if r.Registry != nil && agentID != "" {
		if def, ok := r.Registry.Get(agentID); ok {
			id.AgentMemoryMode = def.Memory.Mode
		}
	}
	return id
}

// retrieveForContext returns the bounded, ranked memory the model may see for
// this decision. It is the authorized path: scope first, then a bounded query.
// A failure yields no memory and never an unfiltered fallback (contract §11, §29).
func (r *Runtime) retrieveForContext(id memory.Identity, limit int) []memory.Record {
	if r.Memory == nil || limit <= 0 {
		return nil
	}
	res, err := r.Memory.Query(id, memory.Query{BusinessID: id.BusinessID, Limit: limit})
	if err != nil {
		return nil
	}
	return res.Records
}
