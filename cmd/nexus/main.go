// Command nexus is the NEXUS process entrypoint.
//
// It loads configuration, initializes logging, wires the Core Runtime
// and HTTP Gateway, and manages the process lifecycle.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/app"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
	"github.com/Nomssky/NEXUS/internal/launcher"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configFile  = flag.String("config", "", "path to a JSON configuration file (optional)")
		showVersion = flag.Bool("version", false, "print version and exit")
		httpAddr    = flag.String("http-addr", "", "HTTP listen address (default: <health-host>:<health-port+1>, e.g. 127.0.0.1:8081)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("nexus %s (%s)\n", version.Version, version.Commit)
		return int(lifecycle.ExitOK)
	}

	// Load configuration using the foundation app package
	a, err := app.New(app.Options{ConfigFile: *configFile})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus: startup aborted: %s\n", safeMessage(err))
		return int(lifecycle.ExitFailure)
	}

	cfg := a.Config()
	log := a.Logger()
	healthSrv := a.Health()
	life := a.Lifecycle()

	// Determine HTTP address
	addr := *httpAddr
	if addr == "" {
		addr = defaultAddr(cfg)
	}

	// Wire the launcher. Control API key is a secret: loaded from the
	// environment only (never from the config file — config holds SecretRef
	// references, not raw secrets).
	//
	// Identity binding (A6): pass security flags and foundation identity
	// components so the gateway enforces authenticated actor + membership on
	// scoped paths when require_authentication / enforce_business_scope are on.
	// The authenticator hydrates the stored verification hashes (never raw
	// credentials) from the same record store as the registry, so credentials
	// registered through the API survive a restart, and is bound to the
	// organization registry, so a credential only authenticates when the
	// identity record exists and is usable (active, unexpired). Memberships
	// hydrate from that store inside app.New.
	auth, err := identity.OpenLocalAuthenticator(a.Store())
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus: startup aborted: %s\n", safeMessage(err))
		return int(lifecycle.ExitFailure)
	}
	auth.SetRegistry(a.Registry())

	// F3: with enforcement active a fresh install has no credential and no
	// API path can create the first membership (identity-in-business creation
	// requires an existing member) — every scoped endpoint would 401 forever.
	// Provision the bootstrap identity from the env-only secret.
	if err := bootstrapIdentity(cfg, log, a.Registry(), auth, a.Memberships()); err != nil {
		fmt.Fprintf(os.Stderr, "nexus: startup aborted: %s\n", safeMessage(err))
		return int(lifecycle.ExitFailure)
	}

	launch := launcher.New(launcher.Options{
		Config:        cfg,
		Logger:        log,
		Health:        healthSrv,
		Lifecycle:     life,
		Addr:          addr,
		ControlAPIKey: os.Getenv(config.EnvControlAPIKey),

		Authenticator:         auth,
		Memberships:           a.Memberships(),
		Registry:              a.Registry(),
		Store:                 a.Store(),
		RequireAuthentication: cfg.Security.RequireAuthentication,
		EnforceBusinessScope:  cfg.Security.EnforceBusinessScope,
	})

	return int(launch.Run(context.Background()))
}

// defaultAddr derives the gateway listen address. It must never collide with
// the health server, which binds cfg.Health.Host:cfg.Health.Port first (app
// package) — the previous hard-coded "8080" made a default boot fail with
// "address already in use". Default gateway port = health port + 1; when the
// health server is disabled or bound to an ephemeral/absent port, fall back
// to 8081.
func defaultAddr(cfg config.Config) string {
	host := cfg.Health.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := 8081
	if cfg.Health.Enabled && cfg.Health.Port > 0 && cfg.Health.Port < 65535 {
		port = cfg.Health.Port + 1
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func safeMessage(err error) string {
	var ne *nerrors.Error
	if errors.As(err, &ne) {
		return fmt.Sprintf("[%s] %s", ne.Category, ne.Message)
	}
	return "internal startup error"
}

// Bootstrap identity constants. The business record's owner_identity_id must
// reference an existing, active human identity (§3.1), so the bootstrap
// identity is a human admin rather than a system record.
const (
	bootstrapIdentityID   = "nx:human:bootstrap"
	bootstrapIdentityName = "NEXUS bootstrap admin"
	bootstrapActor        = "runtime:bootstrap"
	bootstrapDefaultBiz   = "default"
)

// bootstrapIdentity performs first-run identity provisioning (F3): with
// enforcement active, a fresh install has no credentials and the org API can
// never create the FIRST membership (creating an identity inside a business
// requires an existing member), so every scoped endpoint would answer 401
// forever. When NEXUS_BOOTSTRAP_CREDENTIAL is set it ensures the bootstrap
// identity, its business and its membership exist, and registers the
// credential — re-registering on every boot overwrites the stored hash with
// the current env value, so rotating NEXUS_BOOTSTRAP_CREDENTIAL takes effect
// immediately. The membership is re-added idempotently (it is persisted too;
// the duplicate check makes the second boot a no-op).
//
// Note (SCHEMA_IDENTITIES_ORG §10): the env variable is this identity's only
// *source*, but it is no longer its only *state*. Once a hash has been stored,
// unsetting NEXUS_BOOTSTRAP_CREDENTIAL on a later boot stops refreshing it —
// it does not remove it. Revoking or suspending the identity record remains
// the revocation path, and that record is enforced on every authentication.
//
// Posture mirrors NEXUS_CONTROL_API_KEY: the secret is environment-only,
// never config. With enforcement off, bootstrap is skipped entirely. With
// enforcement on and no secret, the system stays fail-closed (401) and a
// warning names the missing variable. A provisioning error aborts startup —
// boot-but-locked would be a silent failure.
func bootstrapIdentity(cfg config.Config, log *logging.Logger, reg *identity.Registry, auth *identity.LocalAuthenticator, members *identity.MembershipSet) error {
	enforced := cfg.Security.RequireAuthentication || cfg.Security.EnforceBusinessScope
	if !enforced {
		return nil
	}
	credential := os.Getenv(config.EnvBootstrapCredential)
	if credential == "" {
		if _, exists := reg.GetIdentity(bootstrapIdentityID); !exists {
			log.Warn("identity enforcement is active but NEXUS_BOOTSTRAP_CREDENTIAL is not set: all scoped endpoints will fail closed (401) until the bootstrap credential is provided", logging.Fields{})
		}
		return nil
	}
	businessID := os.Getenv(config.EnvBootstrapBusiness)
	if businessID == "" {
		businessID = bootstrapDefaultBiz
	}

	now := time.Now().UTC()
	if _, exists := reg.GetIdentity(bootstrapIdentityID); !exists {
		if _, err := reg.CreateIdentity(identity.Identity{
			ID:          bootstrapIdentityID,
			Type:        identity.TypeHuman,
			DisplayName: bootstrapIdentityName,
			Status:      identity.StatusActive,
			Scope:       identity.GlobalScope(),
			CreatedAt:   now,
		}, bootstrapActor); err != nil {
			return nerrors.Wrap(err, "bootstrap.identity_failed", nerrors.CategoryInternalFailure,
				"bootstrap identity provisioning failed")
		}
	}
	if _, exists := reg.GetBusiness(businessID); !exists {
		if _, err := reg.CreateBusiness(identity.Business{
			EntityID:        businessID,
			Name:            businessID,
			OwnerIdentityID: bootstrapIdentityID,
			CreatedAt:       now,
		}, bootstrapActor); err != nil {
			return nerrors.Wrap(err, "bootstrap.business_failed", nerrors.CategoryInternalFailure,
				"bootstrap business provisioning failed")
		}
	}
	// Idempotent: after a restart the membership is already hydrated, so this
	// is a no-op; on a first run it is the write that persists it.
	if err := members.Add(identity.Membership{
		IdentityID: bootstrapIdentityID,
		BusinessID: businessID,
		Role:       identity.RoleAdmin,
		Status:     identity.StatusActive,
	}); err != nil {
		return nerrors.Wrap(err, "bootstrap.membership_failed", nerrors.CategoryInternalFailure,
			"bootstrap membership provisioning failed")
	}
	// Idempotent overwrite: re-arm the credential every boot from the
	// env-only secret (see the function comment for why this is authoritative
	// over whatever hash is already stored).
	if err := auth.Register(bootstrapIdentityID, security.HashCredential([]byte(credential)), identity.AuthMethodPassword); err != nil {
		return nerrors.Wrap(err, "bootstrap.credential_failed", nerrors.CategoryInternalFailure,
			"bootstrap credential registration failed")
	}
	log.Warn("bootstrap identity provisioned", logging.Fields{
		Context: map[string]any{
			"identity_id": bootstrapIdentityID,
			"business_id": businessID,
			"note":        "credential sourced from NEXUS_BOOTSTRAP_CREDENTIAL (env-only)",
		},
	})
	return nil
}
