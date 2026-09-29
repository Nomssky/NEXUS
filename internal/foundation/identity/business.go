package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// BusinessStatus is the Business lifecycle status (contract §3.1:
// active/suspended/archived).
type BusinessStatus string

const (
	BusinessActive    BusinessStatus = "active"
	BusinessSuspended BusinessStatus = "suspended"
	BusinessArchived  BusinessStatus = "archived"
)

// IsValid reports whether s is a canonical business status.
func (s BusinessStatus) IsValid() bool {
	switch s {
	case BusinessActive, BusinessSuspended, BusinessArchived:
		return true
	}
	return false
}

// BusinessSettings is the contract BusinessSettings record (§3.2).
type BusinessSettings struct {
	DefaultModelProvider string `json:"default_model_provider,omitempty"`
	MaxConcurrentAgents  int    `json:"max_concurrent_agents,omitempty"`
	DataResidency        string `json:"data_residency,omitempty"`
	RetentionPolicy      string `json:"retention_policy,omitempty"`
}

// Validate rejects a nonsensical settings block (negative concurrency).
func (s BusinessSettings) Validate() error {
	if s.MaxConcurrentAgents < 0 {
		return nerrors.Validation("identity.business_settings_invalid",
			"max_concurrent_agents must not be negative")
	}
	return nil
}

// Business is the contract Business Record (SCHEMA_IDENTITIES_ORG §3.1).
//
// A Business is the primary isolation boundary (§3.3). It grants no authority
// to anyone; owner_identity_id is a descriptive ownership reference resolved
// by Governance, not a permission.
type Business struct {
	SchemaVersion string `json:"schema_version"`
	EntityType    string `json:"entity_type"` // always "business"
	EntityID      string `json:"entity_id"`
	NexusID       string `json:"nexus_id"`
	// BusinessID is the self-reference for scope enforcement (§3.1) and always
	// equals EntityID.
	BusinessID string         `json:"business_id"`
	Name       string         `json:"name"`
	Status     BusinessStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  *time.Time     `json:"updated_at,omitempty"`
	// OwnerIdentityID is the required primary human owner identity (§3.1).
	OwnerIdentityID string `json:"owner_identity_id"`
	// Divisions lists division IDs within this business (§3.1).
	Divisions  []string             `json:"divisions,omitempty"`
	Settings   *BusinessSettings    `json:"settings,omitempty"`
	Provenance schema.ProvenanceRef `json:"provenance"`
	Metadata   map[string]string    `json:"metadata,omitempty"`
}

// Validate checks the business record against §3.1. It fails closed on a
// self-reference mismatch, a missing owner, or a malformed status.
func (b *Business) Validate() error {
	if b.SchemaVersion == "" {
		return nerrors.Validation("identity.business_schema_required", "business schema_version is required")
	}
	if b.EntityType != "business" {
		return nerrors.Validation("identity.business_entity_type_invalid",
			fmt.Sprintf("entity_type must be %q, got %q", "business", b.EntityType))
	}
	if strings.TrimSpace(b.EntityID) == "" {
		return nerrors.Validation("identity.business_id_required", "business entity_id is required")
	}
	if err := ValidateEntityID(b.EntityID); err != nil {
		return err
	}
	if strings.TrimSpace(b.NexusID) == "" {
		return nerrors.Validation("identity.business_nexus_required", "business nexus_id is required")
	}
	if b.BusinessID != b.EntityID {
		return nerrors.Validation("identity.business_selfref_invalid",
			"business_id must equal entity_id (self-reference, §3.1)")
	}
	if strings.TrimSpace(b.Name) == "" {
		return nerrors.Validation("identity.business_name_required", "business name is required")
	}
	if !b.Status.IsValid() {
		return nerrors.Validation("identity.business_status_invalid",
			fmt.Sprintf("business status %q is not valid", b.Status))
	}
	if b.CreatedAt.IsZero() {
		return nerrors.Validation("identity.business_created_required", "business created_at is required")
	}
	if strings.TrimSpace(b.OwnerIdentityID) == "" {
		return nerrors.Validation("identity.business_owner_required", "business owner_identity_id is required")
	}
	if !b.Provenance.Valid() {
		return nerrors.Validation("identity.business_provenance_required",
			"business provenance must carry origin, producer and produced_at")
	}
	if b.Settings != nil {
		if err := b.Settings.Validate(); err != nil {
			return err
		}
	}
	return validateMetadataMap(b.Metadata)
}

// NewBusiness constructs a business record with a generated entity_id and the
// contract envelope stamped. Cross-record validation (owner identity exists)
// belongs to the Registry.
func NewBusiness(nexusID, name, ownerIdentityID string) (Business, error) {
	id, err := security.NewID("business")
	if err != nil {
		return Business{}, err
	}
	now := time.Now().UTC()
	b := Business{
		SchemaVersion:   schema.Version,
		EntityType:      "business",
		EntityID:        id,
		NexusID:         nexusID,
		BusinessID:      id,
		Name:            name,
		Status:          BusinessActive,
		CreatedAt:       now,
		OwnerIdentityID: ownerIdentityID,
		Provenance: schema.ProvenanceRef{
			Origin:     "system",
			Producer:   "identity.NewBusiness",
			ProducedAt: now,
		},
	}
	if err := b.Validate(); err != nil {
		return Business{}, err
	}
	return b, nil
}
