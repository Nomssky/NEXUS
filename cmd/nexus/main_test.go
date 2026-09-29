package main

import (
	"net"
	"strconv"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
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
