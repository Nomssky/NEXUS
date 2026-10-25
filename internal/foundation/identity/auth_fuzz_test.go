package identity

import (
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// FuzzAuthenticateUnknownIdentity: with no credentials registered, no input may
// ever authenticate, and every failure must stay in the uniform AUTH shape.
func FuzzAuthenticateUnknownIdentity(f *testing.F) {
	f.Add("nx:human:absent", []byte("guess"))
	f.Add("", []byte{})
	f.Add("nx:human:\x00", []byte{0xff, 0x00, 0x7f})

	auth := NewLocalAuthenticator()
	f.Fuzz(func(t *testing.T, identityID string, credential []byte) {
		res, err := auth.Authenticate(identityID, credential)
		if res.Authenticated {
			t.Fatalf("authenticated with no registered credential: id=%q", identityID)
		}
		if err == nil {
			t.Fatal("failure must return an error")
		}
		if nerrors.CategoryOf(err) != nerrors.CategoryAuth {
			t.Fatalf("category: got %s", nerrors.CategoryOf(err))
		}
		if res.Method != AuthMethodNone {
			t.Fatalf("failure method: got %q", res.Method)
		}
		if res.Reason != "authentication failed" {
			t.Fatalf("failure reason: got %q", res.Reason)
		}
	})
}

// FuzzRegisterAuthenticateRoundTrip: a credential registered for an identity
// authenticates that identity (and only with the same bytes).
func FuzzRegisterAuthenticateRoundTrip(f *testing.F) {
	f.Add("nx:human:fuzz", []byte("credential"))
	f.Add("nx:agent:fuzz", []byte("another"))

	f.Fuzz(func(t *testing.T, identityID string, credential []byte) {
		// Registration requires a non-blank id; an empty credential is rejected
		// by the authentication rule regardless of its hash (len check).
		if strings.TrimSpace(identityID) == "" || len(credential) == 0 {
			return
		}
		auth := NewLocalAuthenticator()
		if err := auth.Register(identityID, security.HashCredential(credential), AuthMethodPassword); err != nil {
			t.Fatalf("register: %v", err)
		}
		res, err := auth.Authenticate(identityID, credential)
		if err != nil || !res.Authenticated {
			t.Fatalf("round-trip failed: res=%+v err=%v", res, err)
		}
	})
}

// FuzzHydrateNeverPanics: arbitrary stored bytes must either hydrate cleanly or
// fail closed — never panic, and never produce a partial authenticator.
func FuzzHydrateNeverPanics(f *testing.F) {
	f.Add([]byte(`{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:x","hash":"h","method":"password"}`))
	f.Add([]byte(`{"identity_id":`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		st := store.NewMemStore()
		if err := st.Put(&store.Record{
			ID:     credentialRecordID("nx:human:fuzz"),
			Type:   store.RecordTypeCredential,
			Status: store.RecordStatusActive,
			Data:   data,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		auth, err := OpenLocalAuthenticator(st)
		if err != nil {
			if auth != nil {
				t.Fatal("a failed open must not return a usable authenticator")
			}
			return
		}
		if auth == nil {
			t.Fatal("a successful open must return an authenticator")
		}
	})
}
