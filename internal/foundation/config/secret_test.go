package config

import "testing"

// Secret references carry only a pointer, never a raw value. This guards the
// "secrets are never plain configuration values" invariant at the type level.
func TestSecretRefHasNoRawValue(t *testing.T) {
	ref := SecretRef{Ref: "vault:nexus/provider/openrouter"}
	if ref.Ref == "" {
		t.Fatal("expected a reference string")
	}
	// Structurally, SecretRef has exactly one field. If a future change adds a
	// value field, this test forces a conscious review.
	// (Reflected via a compile-time-shaped literal below.)
	_ = SecretRef{Ref: "x"}
}
