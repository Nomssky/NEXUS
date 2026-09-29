package main

import (
	"net"
	"strconv"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
)

// TEST-GW-DEFAULT-ADDR-01: the default gateway address must not collide with
// the health server's bind address (both default to the same host). Regression
// for the F2 defect where defaultAddr hard-coded port 8080 — identical to the
// default health port — so a default boot died with "address already in use".
func TestDefaultAddrDoesNotCollideWithHealth(t *testing.T) {
	cfg := config.Defaults()
	gw := defaultAddr(cfg)
	healthAddr := net.JoinHostPort(cfg.Health.Host, strconv.Itoa(cfg.Health.Port))

	if gw == healthAddr {
		t.Fatalf("gateway default %q collides with health bind %q", gw, healthAddr)
	}
	if want := net.JoinHostPort(cfg.Health.Host, "8081"); gw != want {
		t.Fatalf("gateway default: want %q, got %q", want, gw)
	}
}

// Health disabled → still a valid non-colliding default.
func TestDefaultAddrHealthDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Health.Enabled = false
	if gw := defaultAddr(cfg); gw != "127.0.0.1:8081" {
		t.Fatalf("gateway default with health disabled: got %q", gw)
	}
}

// TEST-BOOTSTRAP-01: with enforcement active, NEXUS_BOOTSTRAP_CREDENTIAL
// provisions identity + business + membership + credential — the F3 gap where
// a fresh install could never create its first member (API chicken-egg) and
// every scoped endpoint stayed 401 forever.
func TestBootstrapIdentityProvisionsFirstCredential(t *testing.T) {
	t.Setenv(config.EnvBootstrapCredential, "boot-secret")
	t.Setenv(config.EnvBootstrapBusiness, "")
	cfg := config.Defaults()
	reg := identity.NewRegistry("nx:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()
	log := logging.New(logging.Options{})

	if err := bootstrapIdentity(cfg, log, reg, auth, members); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	ident, ok := reg.GetIdentity("nx:human:bootstrap")
	if !ok {
		t.Fatal("bootstrap identity not created")
	}
	if ident.Type != identity.TypeHuman || ident.Status != identity.StatusActive {
		t.Errorf("identity: type=%s status=%s", ident.Type, ident.Status)
	}
	biz, ok := reg.GetBusiness("default")
	if !ok {
		t.Fatal("bootstrap business not created")
	}
	if biz.OwnerIdentityID != "nx:human:bootstrap" {
		t.Errorf("business owner: %q", biz.OwnerIdentityID)
	}
	if !members.IsMember("nx:human:bootstrap", "default", "") {
		t.Error("bootstrap membership missing")
	}
	res, err := auth.Authenticate("nx:human:bootstrap", []byte("boot-secret"))
	if err != nil || !res.Authenticated {
		t.Fatalf("bootstrap credential does not authenticate: res=%+v err=%v", res, err)
	}
	bad, _ := auth.Authenticate("nx:human:bootstrap", []byte("wrong"))
	if bad.Authenticated {
		t.Error("wrong credential must not authenticate")
	}

	// Restart simulation: registry persists, authenticator/memberships do not.
	// A second bootstrap must not collide and must re-arm both.
	auth2 := identity.NewLocalAuthenticator()
	auth2.SetRegistry(reg)
	members2 := identity.NewMembershipSet()
	if err := bootstrapIdentity(cfg, log, reg, auth2, members2); err != nil {
		t.Fatalf("second bootstrap (restart): %v", err)
	}
	if _, ok := reg.GetIdentity("nx:human:bootstrap"); !ok {
		t.Fatal("identity lost after second bootstrap")
	}
	res2, err := auth2.Authenticate("nx:human:bootstrap", []byte("boot-secret"))
	if err != nil || !res2.Authenticated {
		t.Fatalf("credential not re-armed after restart: res=%+v err=%v", res2, err)
	}
	if !members2.IsMember("nx:human:bootstrap", "default", "") {
		t.Error("membership not re-added after restart")
	}
}

// TEST-BOOTSTRAP-02: enforcement off → bootstrap must not provision anything.
func TestBootstrapIdentitySkippedWhenEnforcementOff(t *testing.T) {
	t.Setenv(config.EnvBootstrapCredential, "boot-secret")
	cfg := config.Defaults()
	cfg.Security.RequireAuthentication = false
	cfg.Security.EnforceBusinessScope = false
	reg := identity.NewRegistry("nx:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)

	if err := bootstrapIdentity(cfg, logging.New(logging.Options{}), reg, auth, identity.NewMembershipSet()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, ok := reg.GetIdentity("nx:human:bootstrap"); ok {
		t.Error("bootstrap must be skipped when enforcement is off")
	}
}

// TEST-BOOTSTRAP-03: enforcement on without the env secret stays fail-closed
// (no provisioning) but does not error — the system boots and answers 401.
func TestBootstrapIdentityMissingCredentialFailsClosed(t *testing.T) {
	t.Setenv(config.EnvBootstrapCredential, "")
	cfg := config.Defaults()
	reg := identity.NewRegistry("nx:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)

	if err := bootstrapIdentity(cfg, logging.New(logging.Options{}), reg, auth, identity.NewMembershipSet()); err != nil {
		t.Fatalf("bootstrap must not fail without the secret: %v", err)
	}
	if _, ok := reg.GetIdentity("nx:human:bootstrap"); ok {
		t.Error("no provisioning without NEXUS_BOOTSTRAP_CREDENTIAL")
	}
	if res, _ := auth.Authenticate("nx:human:bootstrap", []byte("anything")); res.Authenticated {
		t.Error("authenticator must stay empty")
	}
}
