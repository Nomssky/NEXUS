// Package schema holds the common data-schema primitives shared by entity
// records (contracts/SCHEMA_COMMON.md §3–§4): the schema version stamp and
// the provenance model.
//
// Provenance is evidence, not proof of truth (SCHEMA_COMMON §4.3): it records
// where a record came from, never what is true about the world. Chains are
// append-only; nothing in this package grants authority or permission.
package schema

import "time"

// Version is the initial schema version stamped on entities that carry the
// common metadata envelope (SCHEMA_COMMON §3.1: schema_version e.g. "1.0.0").
const Version = "1.0.0"

// ProvenanceRef is the contract ProvenanceRef (SCHEMA_COMMON §4.1): a
// reference to the origin and history of a record.
type ProvenanceRef struct {
	// Origin is the original source class (human, tool, model, external_api,
	// system).
	Origin string `json:"origin"`
	// Producer is the specific producer (e.g. "gateway:POST /api/v1/identities",
	// "tool:web_search").
	Producer string `json:"producer"`
	// ProducedAt is when this record was produced.
	ProducedAt time.Time `json:"produced_at"`
	// SourceReference optionally references the source material (URL, file
	// path, tool call ID).
	SourceReference string `json:"source_reference,omitempty"`
	// InputHash optionally hashes the input that produced this record.
	InputHash string `json:"input_hash,omitempty"`
	// Chain optionally carries the append-only provenance steps (§4.2).
	Chain []ProvenanceStep `json:"chain,omitempty"`
}

// Valid reports whether the required provenance fields (§4.1) are present.
func (p ProvenanceRef) Valid() bool {
	return p.Origin != "" && p.Producer != "" && !p.ProducedAt.IsZero()
}

// ProvenanceStep is one append-only step in a provenance chain (§4.2).
type ProvenanceStep struct {
	StepID    string    `json:"step_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	InputRef  string    `json:"input_ref,omitempty"`
	OutputRef string    `json:"output_ref,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
