package identity

import (
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// Benchmarks give the CI a compile-and-run smoke gate for the hot path and a
// baseline for the constant-time failure path.

func benchmarkAuthenticator(b *testing.B, register bool) *LocalAuthenticator {
	b.Helper()
	a := NewLocalAuthenticator()
	if register {
		if err := a.Register("nx:human:bench", security.HashCredential([]byte("secret")), AuthMethodPassword); err != nil {
			b.Fatalf("register: %v", err)
		}
	}
	return a
}

func BenchmarkAuthenticateKnown(b *testing.B) {
	a := benchmarkAuthenticator(b, true)
	cred := []byte("secret")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.Authenticate("nx:human:bench", cred); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuthenticateUnknown(b *testing.B) {
	a := benchmarkAuthenticator(b, false)
	cred := []byte("secret")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.Authenticate("nx:human:absent", cred)
	}
}

func BenchmarkAuthenticateWrongCredential(b *testing.B) {
	a := benchmarkAuthenticator(b, true)
	cred := []byte("wrong")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.Authenticate("nx:human:bench", cred)
	}
}

func BenchmarkAuthenticateParallel(b *testing.B) {
	a := benchmarkAuthenticator(b, true)
	cred := []byte("secret")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := a.Authenticate("nx:human:bench", cred); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkRegister(b *testing.B) {
	a := NewLocalAuthenticator()
	hash := security.HashCredential([]byte("secret"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.Register("nx:human:bench", hash, AuthMethodPassword); err != nil {
			b.Fatal(err)
		}
	}
}
