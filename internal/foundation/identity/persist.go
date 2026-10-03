// Package identity — write-through persistence helpers shared by the
// registry, the authenticator and the membership set (SCHEMA_IDENTITIES_ORG
// §10). Each of those three owns its own records; this file owns only the
// "store first, then memory" mechanics so they cannot drift.
package identity

import (
	"errors"
	"fmt"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// persistRecord writes one record through to the backing store. The stored
// version of an existing record is carried over so both store
// implementations accept the write (MemStore requires an exact version
// match; FileStore rejects a mismatched non-zero version). A nil store makes
// this a no-op. Errors are wrapped, preserving store sentinels for callers
// that check with errors.Is.
//
// Note: MemStore soft-deletes, so a Put after a Delete of the same id reports
// a version conflict. Identity ids are random and never reused, so this only
// matters for a re-create of a removed id — which callers never do.
func persistRecord(st store.Store, record *store.Record) error {
	if st == nil {
		return nil
	}
	prev, err := st.Get(record.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("read before persist %q: %w", record.ID, err)
	}
	if prev != nil {
		record.Version = prev.Version
	}
	if err := st.Put(record); err != nil {
		return fmt.Errorf("persist %q: %w", record.ID, err)
	}
	return nil
}
