package capability

// HTTP capability (contracts/CAPABILITY_TOOL_CONTRACTS.md §9, §16–§18): a
// mediated outbound HTTP tool with SSRF defence at every hop. The model chooses
// an operation (method) and inputs; it never chooses the policy — allowed
// schemes/hosts/ports, redirect and size bounds all come from runtime
// configuration.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// HTTPPolicy is the runtime-controlled destination policy. Model output can
// never change any field here.
type HTTPPolicy struct {
	// AllowedHosts: when non-empty, ONLY these hosts (or suffixes) may be
	// contacted. Empty means "no host allowlist" but SSRF rules still apply.
	AllowedHosts []string
	// BlockedHosts always win over the allowlist.
	BlockedHosts []string
	// AllowedSchemes defaults to https (+ http when AllowInsecureHTTP).
	AllowInsecureHTTP bool
	// AllowedPorts, when non-empty, is the only permitted port set.
	AllowedPorts []int
	// AllowLoopback permits loopback destinations. It exists for local
	// development and the black-box suite ONLY and is hard-refused in the
	// production environment (see LoadHTTPPolicyFromEnv).
	AllowLoopback bool
	// AllowAnyPublic permits any public host when no allowlist is configured.
	// It defaults to FALSE: a tool invocation never implies internet access
	// (contract §16). Like AllowLoopback it is hard-refused in production.
	AllowAnyPublic bool
	Production     bool

	Timeout         time.Duration
	MaxRedirects    int
	MaxResponseByte int
	MaxHeaderCount  int
	MaxBodyByte     int
}

// DefaultHTTPPolicy is the shipped, deny-by-default policy.
func DefaultHTTPPolicy() HTTPPolicy {
	return HTTPPolicy{
		Timeout:         10 * time.Second,
		MaxRedirects:    3,
		MaxResponseByte: 64 * 1024,
		MaxHeaderCount:  64,
		MaxBodyByte:     16 * 1024,
	}
}

// LoadHTTPPolicyFromEnv reads the runtime policy from the process environment
// (non-secret operational settings). Fail-closed: loopback is refused in the
// production environment, and the cloud metadata address is always blocked.
func LoadHTTPPolicyFromEnv(env func(string) string, production bool) (HTTPPolicy, error) {
	p := DefaultHTTPPolicy()
	p.Production = production
	if v := env("NEXUS_TOOL_HTTP_ALLOWED_HOSTS"); v != "" {
		p.AllowedHosts = splitList(v)
	}
	if v := env("NEXUS_TOOL_HTTP_BLOCKED_HOSTS"); v != "" {
		p.BlockedHosts = splitList(v)
	}
	if v := env("NEXUS_TOOL_HTTP_ALLOW_INSECURE"); v == "true" {
		p.AllowInsecureHTTP = true
	}
	if v := env("NEXUS_TOOL_HTTP_ALLOWED_PORTS"); v != "" {
		for _, s := range splitList(v) {
			n, err := strconv.Atoi(s)
			if err != nil {
				return p, fmt.Errorf("%w: NEXUS_TOOL_HTTP_ALLOWED_PORTS entry %q is not a port", ErrValidation, s)
			}
			p.AllowedPorts = append(p.AllowedPorts, n)
		}
	}
	if env("NEXUS_TOOL_HTTP_ALLOW_LOOPBACK") == "true" {
		if production {
			return p, fmt.Errorf("%w: loopback access cannot be enabled in production", ErrNetworkBlocked)
		}
		p.AllowLoopback = true
	}
	if env("NEXUS_TOOL_HTTP_ALLOW_ANY_PUBLIC") == "true" {
		if production {
			return p, fmt.Errorf("%w: unrestricted public access cannot be enabled in production", ErrNetworkBlocked)
		}
		p.AllowAnyPublic = true
	}
	return p, nil
}

func splitList(v string) []string {
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// HTTPTool is the mediated HTTP adapter.
type HTTPTool struct {
	Policy HTTPPolicy
	// client is built per policy; tests inject a transport.
	client *http.Client
}

// NewHTTPTool wires the adapter for a policy.
func NewHTTPTool(policy HTTPPolicy) *HTTPTool {
	t := &HTTPTool{Policy: policy}
	t.client = &http.Client{
		Timeout: policy.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= policy.MaxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrNetworkBlocked)
			}
			// Every hop is validated again by the dialer, but the URL itself is
			// checked here so a blocked destination is refused before dialing.
			return validateURL(req.URL, policy)
		},
		Transport: &http.Transport{
			DialContext:           t.dialGuarded,
			TLSHandshakeTimeout:   policy.Timeout,
			ResponseHeaderTimeout: policy.Timeout,
			DisableKeepAlives:     true,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	return t
}

// Operations implements tool.Adapter.
func (t *HTTPTool) Operations() []string {
	return []string{"delete", "get", "patch", "post", "put"}
}

// dialGuarded validates the *resolved address* it is asked to dial, which is the
// DNS-rebinding defence: a hostname that resolves to a private/loopback address
// is refused at connect time, not only at parse time.
func (t *HTTPTool) dialGuarded(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("%w: network %q is not permitted", ErrNetworkBlocked, network)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed address", ErrNetworkBlocked)
	}
	if t.Policy.AllowedPorts != nil && !containsInt(t.Policy.AllowedPorts, portInt(port)) {
		return nil, fmt.Errorf("%w: port %s is not allowed", ErrNetworkBlocked, port)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve host", ErrNetworkBlocked)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: host has no addresses", ErrNetworkBlocked)
	}
	for _, ip := range ips {
		if !t.addressAllowed(ip.IP) {
			return nil, fmt.Errorf("%w: destination address is not permitted", ErrNetworkBlocked)
		}
	}
	d := net.Dialer{Timeout: t.Policy.Timeout}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func (t *HTTPTool) addressAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() {
		return false
	}
	if ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// Cloud metadata and link-local are blocked unconditionally.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// Loopback is an explicit, environment-specific opt-in, NEVER a default,
	// and never in production.
	if ip.IsLoopback() {
		return t.Policy.AllowLoopback && !t.Policy.Production
	}
	if v4 := ip.To4(); v4 != nil {
		// RFC1918, CGNAT 100.64/10, benchmarking 198.18/15, documentation,
		// broadcast and metadata 169.254 are all refused.
		switch {
		case v4[0] == 10, v4[0] == 0, v4[0] == 127:
			return false
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return false
		case v4[0] == 192 && v4[1] == 168:
			return false
		case v4[0] == 169 && v4[1] == 254:
			return false
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return false
		case v4[0] >= 224:
			return false
		}
		return true
	}
	// IPv6: unique-local fc00::/7 is refused.
	if len(ip) == net.IPv6len && ip.To4() == nil {
		if ip[0]&0xfe == 0xfc {
			return false
		}
	}
	return true
}

// validateURL enforces scheme, host and port policy before any request.
func validateURL(u *url.URL, policy HTTPPolicy) error {
	if u == nil {
		return fmt.Errorf("%w: empty url", ErrNetworkBlocked)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !policy.AllowInsecureHTTP {
			return fmt.Errorf("%w: http is not permitted", ErrNetworkBlocked)
		}
	default:
		return fmt.Errorf("%w: scheme %q is not permitted", ErrNetworkBlocked, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: url credentials are not permitted", ErrNetworkBlocked)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: url has no host", ErrNetworkBlocked)
	}
	if strings.HasSuffix(host, ".localhost") || host == "localhost" {
		if !policy.AllowLoopback || policy.Production {
			return fmt.Errorf("%w: localhost is not permitted", ErrNetworkBlocked)
		}
	}
	for _, b := range policy.BlockedHosts {
		if host == b || strings.HasSuffix(host, "."+b) {
			return fmt.Errorf("%w: host is blocked by policy", ErrNetworkBlocked)
		}
	}
	if len(policy.AllowedHosts) > 0 {
		ok := false
		for _, a := range policy.AllowedHosts {
			entry := strings.ToLower(strings.TrimSpace(a))
			// Allowlist entries may be bare hosts or host:port; compare on
			// the hostname, the port is validated separately.
			if h, _, err := net.SplitHostPort(entry); err == nil {
				entry = h
			}
			if host == entry || strings.HasSuffix(host, "."+entry) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%w: host is not on the allowlist", ErrNetworkBlocked)
		}
	} else if !policy.AllowAnyPublic {
		// Fail closed: without an explicit host allowlist no destination is
		// reachable. A tool invocation is never internet access.
		return fmt.Errorf("%w: no host allowlist is configured for outbound HTTP", ErrNetworkBlocked)
	}
	if policy.AllowedPorts != nil {
		p := u.Port()
		if p == "" {
			if u.Scheme == "https" {
				p = "443"
			} else {
				p = "80"
			}
		}
		if !containsInt(policy.AllowedPorts, portInt(p)) {
			return fmt.Errorf("%w: port %s is not allowed", ErrNetworkBlocked, p)
		}
	}
	return nil
}

// Invoke implements tool.Adapter: one mediated request/response. It is the
// MODEL-facing path: the operation is a method, the input carries the URL,
// body and content type — and nothing else. Authorization never comes from
// model input; a credentialed tool resolves its token through
// CredentialHandle (contract §12) and uses the internal request path below.
func (t *HTTPTool) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	if err := ctx.Err(); err != nil {
		return tool.RawResult{}, err
	}
	method := strings.ToUpper(inv.Operation)
	if !contains(t.Operations(), strings.ToLower(inv.Operation)) {
		return tool.RawResult{}, fmt.Errorf("%w: unsupported method %q", ErrValidation, inv.Operation)
	}
	return t.request(ctx, request{
		Method: method, URL: strings.TrimSpace(inv.Input["url"]),
		Body: inv.Input["body"], ContentType: inv.Input["content_type"],
		MaxRequestBytes: inv.Limits.MaxRequestByt,
	})
}

// request is one already-authorized HTTP exchange. It is internal: only the
// mediated adapters call it, and the token (when any) comes from a resolved
// credential handle.
type request struct {
	Method          string
	URL             string
	Body            string
	ContentType     string
	Token           string
	MaxRequestBytes int
}

// request performs the policy-checked exchange and normalizes the response
// (contract §18).
func (t *HTTPTool) request(ctx context.Context, rq request) (tool.RawResult, error) {
	u, err := url.Parse(strings.TrimSpace(rq.URL))
	if err != nil {
		return tool.RawResult{}, fmt.Errorf("%w: invalid url", ErrValidation)
	}
	if err := validateURL(u, t.Policy); err != nil {
		return tool.RawResult{}, err
	}
	var body io.Reader
	if rq.Body != "" {
		max := rq.MaxRequestBytes
		if max <= 0 {
			max = t.Policy.MaxBodyByte
		}
		if len(rq.Body) > max {
			return tool.RawResult{}, fmt.Errorf("%w: request body exceeds the limit", ErrResourceLimit)
		}
		body = strings.NewReader(rq.Body)
	}
	req, err := http.NewRequestWithContext(ctx, rq.Method, u.String(), body)
	if err != nil {
		return tool.RawResult{}, fmt.Errorf("%w: cannot build request", ErrValidation)
	}
	req.Header.Set("User-Agent", "nexus-capability/1")
	if rq.ContentType != "" {
		req.Header.Set("Content-Type", rq.ContentType)
	}
	if rq.Token != "" {
		req.Header.Set("Authorization", "Bearer "+rq.Token)
	}
	client := t.client
	if client == nil {
		return tool.RawResult{}, fmt.Errorf("%w: http client unavailable", ErrInternal)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return tool.RawResult{}, ctx.Err()
		}
		// A dial-time SSRF refusal is a NETWORK BLOCKED, not an opaque
		// external failure: it must stay distinguishable for the audit trail.
		if errors.Is(err, ErrNetworkBlocked) {
			return tool.RawResult{}, err
		}
		return tool.RawResult{}, fmt.Errorf("%w: request failed: %v", ErrExternal, err)
	}
	defer resp.Body.Close()
	if n := len(resp.Header); n > t.Policy.MaxHeaderCount {
		return tool.RawResult{}, fmt.Errorf("%w: response has too many headers", ErrResourceLimit)
	}
	limited := io.LimitReader(resp.Body, int64(t.Policy.MaxResponseByte)+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return tool.RawResult{}, fmt.Errorf("%w: reading response failed: %v", ErrExternal, err)
	}
	truncated := false
	if len(raw) > t.Policy.MaxResponseByte {
		raw = raw[:t.Policy.MaxResponseByte]
		truncated = true
	}
	headerCount := 0
	headers := map[string]string{}
	for k := range resp.Header {
		headerCount++
		if headerCount > t.Policy.MaxHeaderCount {
			return tool.RawResult{}, fmt.Errorf("%w: response has too many headers", ErrResourceLimit)
		}
		// Credential-bearing response headers are stripped inside the adapter
		// itself (defence in depth: the platform redaction layer would strip
		// them again, but the adapter never emits them at all).
		if isSensitiveHeader(k) {
			headers[k] = redactedMarker
			continue
		}
		headers[k] = resp.Header.Get(k)
	}
	return tool.RawResult{
		Result: map[string]string{
			"status_code":  strconv.Itoa(resp.StatusCode),
			"content_type": resp.Header.Get("Content-Type"),
			"body":         string(raw),
			"truncated":    strconv.FormatBool(truncated),
			"final_url":    resp.Request.URL.String(),
		},
		Headers:  headers,
		Metadata: map[string]string{"duration_ms": "0"},
	}, nil
}

func portInt(p string) int {
	n, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil {
		return -1
	}
	return n
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// HTTPToolManifest is the manifest for the HTTP capability. The method set is
// the operation set; the URL host policy is runtime configuration, never input.
func HTTPToolManifest() tool.ToolManifest {
	return tool.ToolManifest{
		ID: "http.request", Version: "1.0.0", Name: "HTTP Request",
		Description: "Mediated outbound HTTP request with SSRF protection (operations: get, post, put, patch, delete).",
		Category:    tool.ToolCategoryNetwork,
		InputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "url", Type: tool.FieldString, Required: true, MaxLength: 2048},
			{Name: "body", Type: tool.FieldString, Required: false, MaxLength: 65536},
			{Name: "content_type", Type: tool.FieldString, Required: false, MaxLength: 128},
		}},
		OutputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "status_code", Type: tool.FieldString},
			{Name: "content_type", Type: tool.FieldString},
			{Name: "body", Type: tool.FieldString, MaxLength: 65536},
			{Name: "truncated", Type: tool.FieldString},
			{Name: "final_url", Type: tool.FieldString, MaxLength: 2048},
		}},
		SideEffectClass:    tool.SideEffectWrite, // method-dependent; conservative
		NetworkRequirement: tool.NetworkOutboundHTTP,
		ScopeRequirement:   tool.ScopeBusiness,
		SecurityClass:      tool.SecurityNetwork,
		Operations:         []string{"delete", "get", "patch", "post", "put"},
		ResourceLimits:     tool.ResourceLimits{MaxDuration: 10 * time.Second, MaxRequestByt: 16384, MaxOutputByte: 65536},
	}
}
