package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// GET /api/v1/control/status documents `uptime` as a present field. It was
// carried in the response struct but never populated, so the wire always
// showed "".
func TestControlStatusUptimePopulated(t *testing.T) {
	f := divisionFixture(t)

	w := f.doAuth(t, http.MethodGet, "/api/v1/control/status", "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("control status: got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Uptime string `json:"uptime"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if body.Uptime == "" {
		t.Fatal("uptime is empty — docs/http-gateway.md documents it as present")
	}
	if !strings.Contains(body.Uptime, "s") {
		t.Errorf("uptime %q does not look like a Go duration", body.Uptime)
	}
}
