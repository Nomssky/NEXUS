package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// DivisionStatus is the Division lifecycle status (contract §4.1:
// active/suspended/archived).
type DivisionStatus string

const (
	DivisionActive    DivisionStatus = "active"
	DivisionSuspended DivisionStatus = "suspended"
	DivisionArchived  DivisionStatus = "archived"
)

// IsValid reports whether s is a canonical division status.
func (s DivisionStatus) IsValid() bool {
	switch s {
	case DivisionActive, DivisionSuspended, DivisionArchived:
		return true
	}
	return false
}

// DivisionSettings is the contract DivisionSettings record (§4.2).
type DivisionSettings struct {
	AllowedModelProviders []string `json:"allowed_model_providers,omitempty"`
	MaxConcurrentAgents   int      `json:"max_concurrent_agents,omitempty"`
	// DataClassification is the default classification for division data.
	DataClassification string `json:"data_classification,omitempty"`
}

// Validate rejects a nonsensical settings block (negative concurrency).
func (s DivisionSettings) Validate() error {
	if s.MaxConcurrentAgents < 0 {
		return nerrors.Validation("identity.division_settings_invalid",
			"max_concurrent_agents must not be negative")
	}
	return nil
}

// Division is the contract Division Record (SCHEMA_IDENTITIES_ORG §4.1).
//
// A Division exists within exactly one Business (§4.3); its scope is narrower
// than the business scope. Division membership is an isolation boundary, not
// an authority.
type Division struct {
	SchemaVersion string `json:"schema_version"`
	EntityType    string `json:"entity_type"` // always "division"
	EntityID      string `json:"entity_id"`
	NexusID       string `json:"nexus_id"`
	// BusinessID is the parent business (scope enforcement, §4.1).
	BusinessID string         `json:"business_id"`
	Name       string         `json:"name"`
	Status     DivisionStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  *time.Time     `json:"updated_at,omitempty"`
	// OwnerIdentityID is the required division owner identity (§4.1).
	OwnerIdentityID string `json:"owner_identity_id"`
	// ParentBusinessID references the parent business (§4.1) and always equals
	// BusinessID — a division never spans businesses (§4.3).
	ParentBusinessID string `json:"parent_business_id"`
	// Agents lists agent IDs in this division (§4.1).
	Agents     []string             `json:"agents,omitempty"`
	Settings   *DivisionSettings    `json:"settings,omitempty"`
	Provenance schema.ProvenanceRef `json:"provenance"`
	Metadata   map[string]string    `json:"metadata,omitempty"`
}

// Validate checks the division record against §4.1. The single-business rule
// (§4.3) is enforced as BusinessID == ParentBusinessID.
func (d *Division) Validate() error {
	if d.SchemaVersion == "" {
		return nerrors.Validation("identity.division_schema_required", "division schema_version is required")
	}
	if d.EntityType != "division" {
		return nerrors.Validation("identity.division_entity_type_invalid",
			fmt.Sprintf("entity_type must be %q, got %q", "division", d.EntityType))
	}
	if strings.TrimSpace(d.EntityID) == "" {
		return nerrors.Validation("identity.division_id_required", "division entity_id is required")
	}
	if err := ValidateEntityID(d.EntityID); err != nil {
		return err
	}
	if strings.TrimSpace(d.NexusID) == "" {
		return nerrors.Validation("identity.division_nexus_required", "division nexus_id is required")
	}
	if strings.TrimSpace(d.BusinessID) == "" {
		return nerrors.Validation("identity.division_business_required", "division business_id is required")
	}
	if d.ParentBusinessID != d.BusinessID {
		return nerrors.Validation("identity.division_parent_invalid",
			"parent_business_id must equal business_id (§4.3: one business only)")
	}
	if strings.TrimSpace(d.Name) == "" {
		return nerrors.Validation("identity.division_name_required", "division name is required")
	}
	if !d.Status.IsValid() {
		return nerrors.Validation("identity.division_status_invalid",
			fmt.Sprintf("division status %q is not valid", d.Status))
	}
	if d.CreatedAt.IsZero() {
		return nerrors.Validation("identity.division_created_required", "division created_at is required")
	}
	if strings.TrimSpace(d.OwnerIdentityID) == "" {
		return nerrors.Validation("identity.division_owner_required", "division owner_identity_id is required")
	}
	if !d.Provenance.Valid() {
		return nerrors.Validation("identity.division_provenance_required",
			"division provenance must carry origin, producer and produced_at")
	}
	if d.Settings != nil {
		if err := d.Settings.Validate(); err != nil {
			return err
		}
	}
	return validateMetadataMap(d.Metadata)
}

// NewDivision constructs a division record with a generated entity_id and the
// contract envelope stamped. Cross-record validation (business exists, owner
// exists) belongs to the Registry.
func NewDivision(nexusID, businessID, name, ownerIdentityID string) (Division, error) {
	id, err := security.NewID("division")
	if err != nil {
		return Division{}, err
	}
	now := time.Now().UTC()
	d := Division{
		SchemaVersion:    schema.Version,
		EntityType:       "division",
		EntityID:         id,
		NexusID:          nexusID,
		BusinessID:       businessID,
		Name:             name,
		Status:           DivisionActive,
		CreatedAt:        now,
		OwnerIdentityID:  ownerIdentityID,
		ParentBusinessID: businessID,
		Provenance: schema.ProvenanceRef{
			Origin:     "system",
			Producer:   "identity.NewDivision",
			ProducedAt: now,
		},
	}
	if err := d.Validate(); err != nil {
		return Division{}, err
	}
	return d, nil
}
