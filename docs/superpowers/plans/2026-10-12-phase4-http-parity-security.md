# Phase 4: HTTP Transport Parity and Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`, the one-off chi bootstrap) run alone from the main thread; see Global Constraints.

**Goal:** Every `nova new` HTTP project, whichever of the four frameworks it uses, resolves the client IP only through configured trusted proxies, never widens CORS to `*`, bounds request bodies and request time, answers framework errors (404/405/413) in the standard envelope, challenges and throttles credential endpoints per RFC 6750 and RFC 6585, refuses to let one user mutate another, and reports readiness without leaking dependency error text.

**Architecture:** `HTTPConfig` gains `trusted_proxies` and `request_timeout`, validated at load. Each router installs its framework's trusted-proxy mechanism (fiber `TrustedProxies`, gin `SetTrustedProxies`, echo `IPExtractor`, a chi `ClientIP` middleware that rewrites `RemoteAddr`), a `Timeout` middleware, a body limit (fiber's config field, a `BodyLimit` middleware elsewhere), and an error handler that routes framework errors through `httpwriter.WriteError` using a new `errors.FromStatus`. The fiber and echo access-log middlewares resolve handler errors inline so the logged status is the one the client sees. Auth compares the scheme case-insensitively and sets `WWW-Authenticate`; the limiter evicts idle buckets, sets `Retry-After`, and also guards registration. The usecase enforces the bcrypt length bound, fails closed when the timing hash cannot be built, and checks ownership on `Update`/`Delete`. The readiness checker returns `"fail"` per dependency plus a joined error for the log. The OpenAPI document is rewritten from the handlers, and `TestHTTPParity` pins all of it across the four rendered variants.

**Tech Stack:** fiber v2, gin, chi v5, echo v4, `golang.org/x/time/rate`, `net/http.MaxBytesReader`, `golang.org/x/crypto/bcrypt`.

**Status (2026-10-12):** all five tasks implemented and verified (`make test-all` green at ~150 s with the fifth bootstrap; `make lint` 0 issues; markdownlint 0 issues on every edited file); commit steps pending the user's `/grimoire-core:commit`.

**Deviations from the plan as written:**

- Task 2: the echo writer needed a `net/http` import for `bindError` (the plan assumed chi and echo both had it; only chi did).
- Task 3: the fiber access-log middleware crossed `gocognit` 20 once it resolved errors inline, so its line emission moved into a `logAccess` helper shaped like echo's. The one-off chi bootstrap then showed chi's `Logging` had been over the same limit since phase 3 (chi had no standing lint run); it got the same split.
- Task 5: instead of a one-off chi bootstrap, `TestGeneratedProjectBootstrap` gained a standing fifth case (`http_chi_postgres_fx`), so every framework now gets a real `golangci-lint run`; CLAUDE.md and the Makefile help describe 34 vet rows and 5 bootstraps.
- Test assertion for fiber's `ErrorHandler` matches the gofmt-aligned struct literal (`"ErrorHandler:"` plus `"httpwriter.HandleError,"`).

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 2, 3 and 5. Task 5 also runs a one-off chi bootstrap (render + `make gen` + `go build` + `golangci-lint run`) because the standing bootstrap cases cover fiber, gin and echo only.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`.
- Framework-prefixed templates change in all four variants (fiber, gin, chi, echo). A framework that has a native mechanism uses it; a framework that lacks one gets a prefixed middleware rendered only for it (`chi_clientip`, `{gin,chi,echo}_bodylimit`).
- Transport templates depend on `pkg/observability.Logger`, never on `infrastructure/logger` (`TestTransportLoggerPortDecoupling`).
- `NewRouter` keeps its signature `(cfg *config.Config, log observability.Logger, checker *health.Checker, registrars []Registrar) <framework type>`: trusted-proxy entries are validated in `config.Load`, so routers may ignore the parse error with a comment saying so.
- Field and header names: `Retry-After`, `WWW-Authenticate`, `Vary: Origin`; log fields unchanged from phase 3.
- Markdown edits pass markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: Trusted proxies decide the client IP

**Files:**

- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (`HTTPConfig` fields, `TrustedProxyNets`, validation in `Load`)
- Modify: `internal/generator/templates/infrastructure/config/base.yaml.tmpl`, `production.yaml.tmpl`, `internal/generator/templates/env.example.tmpl`
- Modify: `internal/generator/templates/transport/http/fiber_router.go.tmpl`, `gin_router.go.tmpl`, `echo_router.go.tmpl`, `chi_router.go.tmpl`
- Create: `internal/generator/templates/transport/http/middleware/chi_clientip.go.tmpl`
- Modify: `internal/generator/templates/transport/http/middleware/chi_loginlimit.go.tmpl` (drop the header-trusting `clientIP`), `chi_logging.go.tmpl` (no change needed once `RemoteAddr` is rewritten; verify)
- Modify: `internal/generator/generator.go` (`transportFiles`: `clientip.go` for chi)
- Test: `internal/generator/generator_test.go` (`TestHTTPParity`, first assertions)

**Interfaces:**

- Produces: `config.HTTPConfig.TrustedProxies []string`, `config.HTTPConfig.RequestTimeout time.Duration`, `func (c HTTPConfig) TrustedProxyNets() ([]*net.IPNet, error)`, chi `middleware.ClientIP(trusted []*net.IPNet) func(http.Handler) http.Handler`.

- [x] **Step 1: Write the failing parity test**

Append to `internal/generator/generator_test.go`:

```go
// TestHTTPParity pins the transport hardening every framework variant must
// share: trusted-proxy-aware client IPs, CORS that never widens to "*", a
// body limit, server and per-request timeouts, framework 404/405/413 in the
// envelope, case-insensitive bearer auth with RFC 6750 challenges, a bounded
// credential limiter with Retry-After, ownership on mutations, and a readiness
// body that names the failing check without its error text. The vet matrix
// proves these compile; this proves they are present in every variant.
func TestHTTPParity(t *testing.T) {
	t.Parallel()
	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := renderProject(t, httpMatrixConfig(fw, "postgres", "wire"))
			assertHTTPParity(t, dir, fw)
		})
	}
}

func assertHTTPParity(t *testing.T, dir, fw string) {
	t.Helper()
	tr := filepath.Join(dir, "internal/transport/http")
	mw := filepath.Join(tr, "middleware")
	router := readFile(t, filepath.Join(tr, "router.go"))
	limiter := readFile(t, filepath.Join(mw, "loginlimit.go"))

	// Client IP: X-Forwarded-For is honoured only from configured proxies.
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go")),
		`"trusted_proxies"`, `"request_timeout"`, "func (c HTTPConfig) TrustedProxyNets()", "http.trusted_proxies")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/base.yaml")),
		"trusted_proxies: []", "request_timeout:")
	switch fw {
	case "fiber":
		mustContainAll(t, router, "EnableTrustedProxyCheck: true", "TrustedProxies:")
	case "gin":
		mustContainAll(t, router, "SetTrustedProxies(")
	case "chi":
		mustContainAll(t, router, "middleware.ClientIP(")
		mustContainAll(t, readFile(t, filepath.Join(mw, "clientip.go")), "func ClientIP(", "X-Forwarded-For")
		mustNotContain(t, limiter, "X-Forwarded-For")
	case "echo":
		mustContainAll(t, router, "e.IPExtractor", "echo.ExtractIPDirect()", "echo.TrustPrivateNet(false)")
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestHTTPParity ./internal/generator/`
Expected: FAIL on the config assertions (`"trusted_proxies"` missing) for every framework.

- [x] **Step 3: Config fields, parser and validation**

In `config.go.tmpl`, extend the import block:

```go
import (
	"bytes"
	"embed"
	"fmt"
{{- if .HasHTTP}}
	"net"
	"strings"
{{- end}}
	"os"
{{- if or .HasHTTP .HasGRPC .HasDatabase .HasRedis .HasElasticsearch .HasMessageQueue}}
	"time"
{{- end}}

	"github.com/ilyakaznacheev/cleanenv"

	"{{.ModuleName}}/pkg/errors"
)
```

Replace the `HTTPConfig` struct and its comment:

```go
// HTTPConfig holds HTTP server configuration.
//
// RequestTimeout is the deadline put on every request's context
// (middleware.Timeout); keep it below WriteTimeout so a stalled dependency
// fails the request in the envelope before the socket is closed.
// TrustedProxies lists the reverse proxies (CIDR or IP) whose X-Forwarded-For
// is believed; with none configured the TCP peer is the client and the header
// is ignored, so a client cannot spoof its address to the login limiter or
// the access log.
type HTTPConfig struct {
	Port            string               `{{.ConfigFormat}}:"port"             env:"SERVER_PORT"`
	ReadTimeout     time.Duration        `{{.ConfigFormat}}:"read_timeout"     env:"SERVER_READ_TIMEOUT"`
	WriteTimeout    time.Duration        `{{.ConfigFormat}}:"write_timeout"    env:"SERVER_WRITE_TIMEOUT"`
	RequestTimeout  time.Duration        `{{.ConfigFormat}}:"request_timeout"  env:"SERVER_REQUEST_TIMEOUT"`
	ReadBufferSize  int                  `{{.ConfigFormat}}:"read_buffer_size" env:"SERVER_READ_BUFFER_SIZE"`
	BodyLimit       int                  `{{.ConfigFormat}}:"body_limit"       env:"SERVER_BODY_LIMIT"`
	ShutdownTimeout time.Duration        `{{.ConfigFormat}}:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT"`
	TrustedProxies  []string             `{{.ConfigFormat}}:"trusted_proxies"  env:"SERVER_TRUSTED_PROXIES" env-separator:","`
	CORS            CORSConfig           `{{.ConfigFormat}}:"cors"`
	LoginRateLimit  LoginRateLimitConfig `{{.ConfigFormat}}:"login_rate_limit"`
}

// TrustedProxyNets parses TrustedProxies. A bare IP is treated as a /32 (or
// /128) network. Load rejects invalid entries, so later callers may ignore the
// error.
func (c HTTPConfig) TrustedProxyNets() ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(c.TrustedProxies))
	for _, raw := range c.TrustedProxies {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			ip := net.ParseIP(entry)
			if ip == nil {
				return nil, fmt.Errorf("trusted proxy %q is not an IP address or CIDR", raw)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			entry = fmt.Sprintf("%s/%d", ip, bits)
		}
		_, n, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", raw, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}
```

In `Load`, after `cleanenv.ReadEnv`:

```go
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, errors.Wrap(err, "config env vars")
	}
{{- if .HasHTTP}}
	if _, err := cfg.HTTP.TrustedProxyNets(); err != nil {
		return nil, errors.Wrap(err, "config http.trusted_proxies")
	}
{{- end}}

	return &cfg, nil
```

Also add to `Summary` under `HasHTTP`: `fields = append(fields, "http_port", c.HTTP.Port, "http_trusted_proxies", c.HTTP.TrustedProxies)`.

`base.yaml.tmpl`, inside `http:` after `shutdown_timeout: 10s`:

```yaml
  # Deadline on every request's context; keep it below write_timeout so a
  # stalled dependency fails the request in the envelope, not at the socket.
  request_timeout: 20s
  # Reverse proxies (CIDR or IP) whose X-Forwarded-For is believed. Empty means
  # the app faces clients directly: the TCP peer is the client IP and forwarded
  # headers are ignored, so a client cannot spoof its address to the login
  # limiter or the access log. Set SERVER_TRUSTED_PROXIES per deployment.
  trusted_proxies: []
```

`production.yaml.tmpl` under `http:`: add `request_timeout: 10s` after `write_timeout: 15s`.

`env.example.tmpl`, after the CORS block:

```bash
# ── Reverse proxies (per-deployment) ────────────────────────
# Comma-separated CIDRs or IPs of the load balancers in front of this service.
# X-Forwarded-For is trusted only from these; empty means clients connect
# directly and the header is ignored.
SERVER_TRUSTED_PROXIES=
```

- [x] **Step 4: fiber, gin and echo use their native mechanism**

`fiber_router.go.tmpl`, in `fiber.New(fiber.Config{...})`:

```go
	app := fiber.New(fiber.Config{
		BodyLimit:      cfg.HTTP.BodyLimit,
		ReadBufferSize: cfg.HTTP.ReadBufferSize,
		JSONEncoder:    json.Marshal,
		JSONDecoder:    json.Unmarshal,
		ReadTimeout:    cfg.HTTP.ReadTimeout,
		WriteTimeout:   cfg.HTTP.WriteTimeout,
		// X-Forwarded-For is honoured only from the proxies listed in config;
		// with none configured c.IP() is the TCP peer. Entries are validated
		// by config.Load.
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          cfg.HTTP.TrustedProxies,
		EnablePrintRoutes:       cfg.AppEnv == config.EnvLocal, // Only print routes in local environment for easier debugging
	})
```

`gin_router.go.tmpl`, after `engine := gin.New()`:

```go
	// X-Forwarded-For is honoured only from the proxies listed in config; with
	// none configured c.ClientIP() is the TCP peer (gin's default trusts every
	// proxy). Entries are validated by config.Load.
	_ = engine.SetTrustedProxies(cfg.HTTP.TrustedProxies)
```

`echo_router.go.tmpl`, after `e.HideBanner = true`: `e.IPExtractor = ipExtractor(cfg.HTTP)`, and append:

```go
// ipExtractor honours X-Forwarded-For only from the proxies listed in config.
// echo's default extractor believes the header from anyone (and its XFF
// extractor trusts private ranges unless told otherwise), so with no trusted
// proxies the TCP peer is the client and the header is ignored.
func ipExtractor(cfg config.HTTPConfig) echo.IPExtractor {
	nets, _ := cfg.TrustedProxyNets() // entries are validated by config.Load
	if len(nets) == 0 {
		return echo.ExtractIPDirect()
	}
	opts := []echo.TrustOption{echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false)}
	for _, n := range nets {
		opts = append(opts, echo.TrustIPRange(n))
	}
	return echo.ExtractIPFromXFFHeader(opts...)
}
```

- [x] **Step 5: chi resolves the client IP once, in a middleware**

Create `internal/generator/templates/transport/http/middleware/chi_clientip.go.tmpl`:

```go
package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP resolves the client address once per request and rewrites
// r.RemoteAddr to the bare IP, so every later layer (access log, login
// limiter) reads one consistent value. X-Forwarded-For is honoured only when
// the TCP peer is one of the trusted proxies; the client is then the
// right-most entry that is not itself a trusted proxy. With no trusted
// proxies the header is ignored, so a client cannot spoof its address.
func ClientIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = clientIP(r, trusted)
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := hostOnly(r.RemoteAddr)
	if len(trusted) == 0 || !inNets(peer, trusted) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(hops[i])
		if net.ParseIP(ip) == nil {
			return peer // malformed header: fall back to the peer
		}
		if i == 0 || !inNets(ip, trusted) {
			return ip
		}
	}
	return peer
}

func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func inNets(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}
```

`chi_router.go.tmpl`: before `router := chi.NewRouter()` add `trusted, _ := cfg.HTTP.TrustedProxyNets() // entries are validated by config.Load`, and make `router.Use(middleware.ClientIP(trusted))` the first `Use` (before otelchi) so the span, the limiter and the access line all see the same address.

`chi_loginlimit.go.tmpl`: delete the `clientIP` function and the `"net"` import; the handler calls `l.allow(r.RemoteAddr)`. (`chi_logging.go.tmpl` already logs `r.RemoteAddr`, which is now the bare IP.)

`generator.go` `transportFiles`, after the `principal.go` entry:

```go
		// Client IP: fiber, gin and echo resolve trusted proxies natively; chi
		// needs a middleware that rewrites RemoteAddr.
		{
			"templates/transport/http/middleware/chi_clientip.go.tmpl",
			"internal/transport/http/middleware/clientip.go",
			cfg.HasHTTP() && cfg.HTTPFramework == "chi",
		},
```

- [x] **Step 6: Verify**

Run: `go test -run 'TestHTTPParity|TestRenderedGoIsGofmtClean|TestTransportLoggerPortDecoupling' ./internal/generator/` then `make test` and `make lint`.
Expected: PASS, 0 issues.

- [ ] **Step 7: Commit**

Stop and ask the user to run `/grimoire-core:commit` (suggested message: `feat(templates): resolve the client IP only through configured trusted proxies`).

---

### Task 2: CORS never widens; bodies, sockets and requests are bounded

**Files:**

- Modify: `internal/generator/templates/transport/http/middleware/fiber_cors.go.tmpl`, `echo_cors.go.tmpl` (pass-through when the list is empty), `gin_cors.go.tmpl`, `chi_cors.go.tmpl` (`Vary: Origin`)
- Create: `internal/generator/templates/transport/http/middleware/{gin,chi,echo}_bodylimit.go.tmpl`
- Create: `internal/generator/templates/transport/http/middleware/{fiber,gin,chi,echo}_timeout.go.tmpl`
- Modify: `internal/generator/templates/transport/http/httpwriter/{gin,chi,echo}_writer.go.tmpl` (`bindError` maps `*http.MaxBytesError` to 413)
- Modify: `internal/generator/templates/infrastructure/server/echo_http.go.tmpl` (`e.Server` timeouts)
- Modify: `internal/generator/templates/pkg/errors/errors.go.tmpl` (`context.DeadlineExceeded` → 504)
- Modify: the four `*_router.go.tmpl` (install `Timeout` after `Recovery`, `BodyLimit` after `Timeout` where rendered)
- Modify: `internal/generator/generator.go` (`transportFiles`: `timeout.go` for all, `bodylimit.go` for non-fiber)
- Test: `internal/generator/generator_test.go` (`assertHTTPParity`)

**Interfaces:**

- Produces: `middleware.Timeout(d time.Duration)` and `middleware.BodyLimit(limit int)` in each framework's handler type; `httpwriter.bindError(err error) *errors.AppError` (unexported, gin/chi/echo).

- [x] **Step 1: Extend the failing test**

Append to `assertHTTPParity`:

```go
	// CORS never widens to "*"; bodies and request time are bounded.
	cors := readFile(t, filepath.Join(mw, "cors.go"))
	mustContainAll(t, readFile(t, filepath.Join(mw, "timeout.go")), "func Timeout(", "context.WithTimeout(")
	mustContainAll(t, router, "middleware.Timeout(cfg.HTTP.RequestTimeout)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/errors/errors.go")), "context.DeadlineExceeded")
	if fw == "fiber" {
		mustContainAll(t, cors, "len(cfg.AllowOrigins) == 0")
		mustContainAll(t, router, "BodyLimit:")
	} else {
		mustContainAll(t, readFile(t, filepath.Join(mw, "bodylimit.go")), "func BodyLimit(", "http.MaxBytesReader(")
		mustContainAll(t, router, "middleware.BodyLimit(cfg.HTTP.BodyLimit)")
		mustContainAll(t, readFile(t, filepath.Join(tr, "httpwriter/writer.go")), "http.MaxBytesError", "locale.PayloadTooLarge")
	}
	switch fw {
	case "echo":
		mustContainAll(t, cors, "len(cfg.AllowOrigins) == 0")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/server/http.go")),
			"e.Server.ReadTimeout", "e.Server.WriteTimeout")
	case "gin", "chi":
		mustContainAll(t, cors, `"Vary"`)
	}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestHTTPParity ./internal/generator/`
Expected: FAIL (`timeout.go` missing).

- [x] **Step 3: CORS**

`fiber_cors.go.tmpl`:

```go
// CORS reads allowed origins/methods/headers from config. An empty origin list
// means no cross-origin access at all, so nothing is installed: fiber's cors
// middleware would otherwise treat the empty list as "*".
func CORS(cfg config.CORSConfig) fiber.Handler {
	if len(cfg.AllowOrigins) == 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	return cors.New(cors.Config{
		AllowOrigins: strings.Join(cfg.AllowOrigins, ","),
		AllowMethods: strings.Join(cfg.AllowMethods, ","),
		AllowHeaders: strings.Join(cfg.AllowHeaders, ","),
	})
}
```

`echo_cors.go.tmpl`:

```go
// CORS reads allowed origins/methods/headers from config. An empty origin list
// means no cross-origin access at all, so nothing is installed: echo's CORS
// middleware would otherwise treat the empty list as "*".
func CORS(cfg config.CORSConfig) echo.MiddlewareFunc {
	if len(cfg.AllowOrigins) == 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	return echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins: cfg.AllowOrigins,
		AllowMethods: cfg.AllowMethods,
		AllowHeaders: cfg.AllowHeaders,
	})
}
```

`gin_cors.go.tmpl` and `chi_cors.go.tmpl`: add `Vary: Origin` on every response, before the origin check, so caches never serve one origin's answer to another:

```go
		c.Header("Vary", "Origin")          // gin
		w.Header().Add("Vary", "Origin")    // chi
```

- [x] **Step 4: Body limit (gin, chi, echo)**

`gin_bodylimit.go.tmpl`:

```go
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps the request body at limit bytes. Reading past the limit
// fails with *http.MaxBytesError, which httpwriter.Bind maps to 413, and
// net/http closes the connection so the client stops sending. gin has no
// equivalent of fiber's BodyLimit, so this is where the cap lives. A
// non-positive limit leaves the body unbounded.
func BodyLimit(limit int) gin.HandlerFunc {
	if limit <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(limit))
		c.Next()
	}
}
```

`chi_bodylimit.go.tmpl`:

```go
package middleware

import "net/http"

// BodyLimit caps the request body at limit bytes. Reading past the limit
// fails with *http.MaxBytesError, which httpwriter.Bind maps to 413, and
// net/http closes the connection so the client stops sending. A non-positive
// limit leaves the body unbounded.
func BodyLimit(limit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, int64(limit))
			next.ServeHTTP(w, r)
		})
	}
}
```

`echo_bodylimit.go.tmpl`:

```go
package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// BodyLimit caps the request body at limit bytes. Reading past the limit
// fails with *http.MaxBytesError, which httpwriter.Bind maps to 413, and
// net/http closes the connection so the client stops sending. It uses
// net/http's reader rather than echo's BodyLimit so the failure mode matches
// the other frameworks. A non-positive limit leaves the body unbounded.
func BodyLimit(limit int) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		if limit <= 0 {
			return next
		}
		return func(c echo.Context) error {
			req := c.Request()
			req.Body = http.MaxBytesReader(c.Response(), req.Body, int64(limit))
			return next(c)
		}
	}
}
```

In the gin, chi and echo writers, replace the two `Bind` body-decoding failure lines (`return v, errors.L(locale.InvalidRequest).WithCause(err)` right after the decode call, not the validation one) with `return v, bindError(err)` and add:

```go
// bindError maps a body-decoding failure: a body over the configured limit is
// 413 (net/http's MaxBytesReader), anything else the generic 400.
func bindError(err error) *errors.AppError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errors.L(locale.PayloadTooLarge).WithCause(err)
	}
	return errors.L(locale.InvalidRequest).WithCause(err)
}
```

(gin's writer needs `"net/http"` added to its imports; chi and echo already import it.)

- [x] **Step 5: Timeouts**

`echo_http.go.tmpl`, in `NewHTTPServer` before building `s`: echo's `Start` serves through `e.Server`, which `echo.New` leaves without timeouts:

```go
	e.Server.ReadTimeout = cfg.ReadTimeout
	e.Server.WriteTimeout = cfg.WriteTimeout
```

`fiber_timeout.go.tmpl`:

```go
package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Timeout puts a deadline on every request's context so a stalled dependency
// fails the request (504 through errors.ToAppError) instead of holding the
// connection until the server's write timeout. Handlers see it as ctx.Done()
// on c.UserContext(). fiber cannot cancel on client disconnect, so this
// deadline is the only bound a request has. A non-positive d disables it.
func Timeout(d time.Duration) fiber.Handler {
	if d <= 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), d)
		defer cancel()
		c.SetUserContext(ctx)
		return c.Next()
	}
}
```

`gin_timeout.go.tmpl`:

```go
package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout puts a deadline on every request's context so a stalled dependency
// fails the request (504 through errors.ToAppError) instead of holding the
// connection until the server's write timeout. A non-positive d disables it.
func Timeout(d time.Duration) gin.HandlerFunc {
	if d <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
```

`chi_timeout.go.tmpl`:

```go
package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout puts a deadline on every request's context so a stalled dependency
// fails the request (504 through errors.ToAppError) instead of holding the
// connection until the server's write timeout. A non-positive d disables it.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

`echo_timeout.go.tmpl`:

```go
package middleware

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"
)

// Timeout puts a deadline on every request's context so a stalled dependency
// fails the request (504 through errors.ToAppError) instead of holding the
// connection until the server's write timeout. A non-positive d disables it.
func Timeout(d time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		if d <= 0 {
			return next
		}
		return func(c echo.Context) error {
			req := c.Request()
			ctx, cancel := context.WithTimeout(req.Context(), d)
			defer cancel()
			c.SetRequest(req.WithContext(ctx))
			return next(c)
		}
	}
}
```

`errors.go.tmpl`, in `classify` before `default:`:

```go
	case Is(err, context.DeadlineExceeded):
		// The request deadline (middleware.Timeout) or a dependency's own
		// timeout expired: the upstream did not answer in time.
		return &AppError{Code: http.StatusGatewayTimeout, Locale: locale.GatewayTimeout}
```

Routers: after `Recovery` install `middleware.Timeout(cfg.HTTP.RequestTimeout)`; gin, chi and echo then install `middleware.BodyLimit(cfg.HTTP.BodyLimit)`; then CORS and Locale as before.

`generator.go` `transportFiles`:

```go
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_timeout.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/timeout.go",
			cfg.HasHTTP(),
		},
		// Body limit: fiber enforces it in fiber.Config; the net/http frameworks
		// need a middleware.
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_bodylimit.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/bodylimit.go",
			cfg.HasHTTP() && cfg.HTTPFramework != "fiber",
		},
```

- [x] **Step 6: Verify, including the full matrix**

Run: `make test`, `make lint`, then alone: `make test-all`.
Expected: all green.

- [ ] **Step 7: Commit**

Stop and ask the user to run `/grimoire-core:commit` (suggested: `feat(templates): bound bodies, sockets and request time; CORS never widens to *`).

---

### Task 3: Framework errors answer in the envelope

**Files:**

- Modify: `internal/generator/templates/pkg/errors/errors.go.tmpl` (`statusLocale`, `FromStatus`)
- Modify: `internal/generator/templates/transport/http/httpwriter/fiber_writer.go.tmpl`, `echo_writer.go.tmpl` (`HandleError`)
- Modify: the four `*_router.go.tmpl` (fiber `ErrorHandler`, echo `HTTPErrorHandler`, gin `NoRoute`/`NoMethod`/no redirects, chi `NotFound`/`MethodNotAllowed`)
- Modify: `fiber_logging.go.tmpl`, `echo_logging.go.tmpl` (resolve the error inline; level by status)
- Modify: `gin_registrar.go.tmpl` (`""` group roots)
- Test: `internal/generator/generator_test.go`

**Interfaces:**

- Produces: `errors.FromStatus(status int) *AppError`; fiber `httpwriter.HandleError(c *fiber.Ctx, err error) error`; echo `httpwriter.HandleError(err error, c echo.Context)`.

- [x] **Step 1: Extend the failing test**

```go
	// Framework 404/405/413 answer in the envelope; the logged status is the
	// one the client receives.
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/errors/errors.go")), "func FromStatus(status int) *AppError")
	logging := readFile(t, filepath.Join(mw, "logging.go"))
	writer := readFile(t, filepath.Join(tr, "httpwriter/writer.go"))
	registrar := readFile(t, filepath.Join(tr, "v1/user/registrar.go"))
	switch fw {
	case "fiber":
		mustContainAll(t, router, "ErrorHandler: httpwriter.HandleError")
		mustContainAll(t, writer, "func HandleError(c *fiber.Ctx, err error) error", "errors.FromStatus(fe.Code)")
		mustContainAll(t, logging, "c.App().ErrorHandler(c, err)")
	case "echo":
		mustContainAll(t, router, "e.HTTPErrorHandler = httpwriter.HandleError")
		mustContainAll(t, writer, "func HandleError(err error, c echo.Context)", "errors.FromStatus(he.Code)")
		mustContainAll(t, logging, "c.Error(err)")
	case "gin":
		mustContainAll(t, router, "engine.NoRoute(", "engine.NoMethod(", "engine.HandleMethodNotAllowed = true",
			"engine.RedirectTrailingSlash = false", "engine.RedirectFixedPath = false")
		mustContainAll(t, registrar, `POST("", `, `GET("", `)
		mustNotContain(t, registrar, `POST("/", `, `GET("/", `)
	case "chi":
		mustContainAll(t, router, "router.NotFound(", "router.MethodNotAllowed(")
	}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestHTTPParity ./internal/generator/`
Expected: FAIL (`FromStatus` missing).

- [x] **Step 3: `errors.FromStatus`**

In `errors.go.tmpl`, after `httpStatusForLocale`:

```go
// statusLocale picks the canonical locale code for an HTTP status raised by
// the framework itself (router 404/405, server 413, …) rather than by
// application code, so those responses use the same envelope and translation
// as everything else. It is the inverse of localeHTTPStatus for the one code
// per status FromStatus should produce.
var statusLocale = map[int]locale.Locale{
	http.StatusBadRequest:            locale.InvalidRequest,
	http.StatusUnauthorized:          locale.Unauthorized,
	http.StatusForbidden:             locale.Forbidden,
	http.StatusNotFound:              locale.RecordNotFound,
	http.StatusMethodNotAllowed:      locale.MethodNotAllowed,
	http.StatusRequestTimeout:        locale.RequestTimeout,
	http.StatusConflict:              locale.Conflict,
	http.StatusRequestEntityTooLarge: locale.PayloadTooLarge,
	http.StatusUnsupportedMediaType:  locale.UnsupportedMediaType,
	http.StatusUnprocessableEntity:   locale.ValidationFailed,
	http.StatusTooManyRequests:       locale.TooManyRequests,
	http.StatusNotImplemented:        locale.Unimplemented,
	http.StatusBadGateway:            locale.BadGateway,
	http.StatusServiceUnavailable:    locale.ServiceUnavailable,
	http.StatusGatewayTimeout:        locale.GatewayTimeout,
}

// FromStatus builds the AppError for a framework-raised HTTP status. A status
// without a canonical code becomes 500, so a framework error can never leak an
// unmapped status or its own message to the client; attach the framework's
// error with WithCause so it still reaches the access log.
func FromStatus(status int) *AppError {
	code, ok := statusLocale[status]
	if !ok {
		code = locale.InternalError
	}
	return newAtL(httpStatusForLocale(code), code, nil)
}
```

- [x] **Step 4: fiber**

`fiber_writer.go.tmpl`, append:

```go
// HandleError is the app-level fiber ErrorHandler: every error that escapes
// the handler chain (the router's 404/405, fasthttp's 413, a handler's
// returned error) is written in the standard envelope. Framework errors map
// by status through errors.FromStatus and never leak their text to the
// client; the cause still reaches the access log through WithCause.
func HandleError(c *fiber.Ctx, err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return WriteError(c, errors.FromStatus(fe.Code).WithCause(err))
	}
	return WriteError(c, errors.ToAppError(err))
}
```

`fiber_router.go.tmpl`: add `ErrorHandler: httpwriter.HandleError,` to `fiber.Config` and import `"{{.ModuleName}}/internal/transport/http/httpwriter"`.

`fiber_logging.go.tmpl`, replace from `err := c.Next()` to the end of the handler:

```go
		err := c.Next()
		if err != nil {
			// Resolve the error here, inside the access-log scope, so the
			// status on the line is the one the client receives; fiber would
			// otherwise run the ErrorHandler after this middleware returns and
			// the line would read 200.
			if hErr := c.App().ErrorHandler(c, err); hErr != nil {
				_ = c.SendStatus(http.StatusInternalServerError)
			}
		}

		status := c.Response().StatusCode()
		ev := scoped.Info
		if status >= http.StatusInternalServerError {
			ev = scoped.Error
		} else if status >= http.StatusBadRequest {
			ev = scoped.Warn
		}

		fields := []any{
			"status", status,
			"route", c.Route().Path,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", errMsg(err),
			"client_ip", c.IP(),
			"user_agent", string(c.Request().Header.UserAgent()),
		}
		if p := principalFromSlot(ctx); p != nil {
			fields = append(fields, "user_id", p.UserID)
		}
		if app := errors.AppErrorFromContext(ctx); app != nil {
			fields = append(fields, "app_code", app.Code)
			if app.Locale != 0 {
				fields = append(fields, "app_locale", int(app.Locale))
			}
			// Surface the inner cause + trace whenever there's something to
			// diagnose: a server fault (5xx), or any error that wrapped an
			// underlying cause — e.g. a 4xx that failed decoding input or a
			// downstream call. A plain validation 4xx carries no inner Err, so
			// its line stays lean (app_code already names the client mistake)
			// and error-rate alerts stay free of client-driven noise.
			if err != nil || app.Err != nil || status >= http.StatusInternalServerError {
				fields = append(fields, "app_err", app.Error(), "app_trace", app.Trace())
			}
		}
		ev("request", fields...)
		return nil
```

- [x] **Step 5: echo**

`echo_writer.go.tmpl`, append:

```go
// HandleError is the echo HTTPErrorHandler: every error that escapes the
// handler chain (the router's 404/405, a handler's returned error) is written
// in the standard envelope. Framework errors map by status through
// errors.FromStatus and never leak their text to the client; the cause still
// reaches the access log through WithCause. A response that was already
// committed is left alone.
func HandleError(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	app := errors.ToAppError(err)
	var he *echo.HTTPError
	if errors.As(err, &he) {
		app = errors.FromStatus(he.Code).WithCause(err)
	}
	_ = WriteError(c, app) // a write failure means the client is gone
}
```

`echo_router.go.tmpl`: `e.HTTPErrorHandler = httpwriter.HandleError` after `e.HideBanner = true`; import httpwriter.

`echo_logging.go.tmpl`: in `Logging` replace `err := next(c)` / `logAccess(...)` / `return err` with:

```go
			err := next(c)
			if err != nil {
				// Resolve the error here so the status on the access line is
				// the one the client receives; echo would otherwise run
				// HTTPErrorHandler after this middleware returns.
				c.Error(err)
			}
			logAccess(ctx, scoped, c, err, start)
			return nil
```

and in `logAccess` make the level follow the status alone (`if status >= 500 { ev = scoped.Error } else if status >= 400 { ev = scoped.Warn }`); keep `err` in the fields and in the `app_err` condition.

- [x] **Step 6: gin and chi**

`gin_router.go.tmpl`, after `SetTrustedProxies`:

```go
	// The documented paths are canonical: no redirect to a trailing-slash twin
	// and no case or dot-segment fix-ups. Unknown paths and methods answer in
	// the envelope like every other error.
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.HandleMethodNotAllowed = true
	engine.NoRoute(func(c *gin.Context) { httpwriter.WriteError(c, errors.FromStatus(http.StatusNotFound)) })
	engine.NoMethod(func(c *gin.Context) { httpwriter.WriteError(c, errors.FromStatus(http.StatusMethodNotAllowed)) })
```

Imports: `httpwriter` and `"{{.ModuleName}}/pkg/errors"`.

`gin_registrar.go.tmpl`: `public.POST("", r.handler.Create)` and `protected.GET("", r.handler.List)` so `/api/v1/users` is registered exactly (with `"/"` gin registered `/users/` and redirected the documented path).

`chi_router.go.tmpl`, after the `Use` chain and before the probes:

```go
	// Unknown paths and methods answer in the envelope; set before mounting so
	// every subrouter inherits the handlers.
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpwriter.WriteError(w, r, errors.FromStatus(http.StatusNotFound))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpwriter.WriteError(w, r, errors.FromStatus(http.StatusMethodNotAllowed))
	})
```

- [x] **Step 7: Verify, including the full matrix**

Run: `make test`, `make lint`, then alone: `make test-all`.
Expected: all green.

- [ ] **Step 8: Commit**

Stop and ask the user to run `/grimoire-core:commit` (suggested: `feat(templates): framework 404/405/413 answer in the envelope with the logged status`).

---

### Task 4: Credential endpoints and ownership

**Files:**

- Modify: the four `*_auth.go.tmpl` (`bearerToken` case-insensitive, `WWW-Authenticate`)
- Modify: the four `*_loginlimit.go.tmpl` (TTL eviction, `Retry-After`)
- Modify: the four `*_registrar.go.tmpl` (limiter on registration)
- Modify: `internal/generator/templates/domain/security/hasher.go.tmpl` (`MaxPasswordLen`)
- Modify: `internal/generator/templates/usecase/user/service.go.tmpl` (`requireSelf`, password bound, `timingHash`)
- Test: `internal/generator/generator_test.go`

**Interfaces:**

- Produces: `security.MaxPasswordLen = 72`; usecase `requireSelf(ctx, id) error` and `(*Service).timingHash() (string, error)` (unexported); limiter `allow(ip string) (bool, time.Duration)` and `retryAfter(d time.Duration) string` (unexported, identical in all four).

- [x] **Step 1: Extend the failing test**

```go
	// Credential endpoints: RFC 6750 challenge, bounded limiter with
	// Retry-After on login and registration; ownership on mutations.
	mustContainAll(t, readFile(t, filepath.Join(mw, "auth.go")), "strings.EqualFold(", "WWW-Authenticate")
	mustContainAll(t, limiter, "Retry-After", "visitorTTL", "func (l *ipLimiter) allow(ip string) (bool, time.Duration)")
	if n := strings.Count(registrar, "middleware.LoginRateLimit("); n != 2 {
		t.Errorf("registrar applies LoginRateLimit %d times, want 2 (login and registration)", n)
	}
	usecase := readFile(t, filepath.Join(dir, "internal/usecase/user/service.go"))
	mustContainAll(t, usecase, "func requireSelf(", "security.MaxPasswordLen", "func (s *Service) timingHash()")
	mustNotContain(t, usecase, "dummyHashOnce")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/security/hasher.go")), "MaxPasswordLen = 72")
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestHTTPParity ./internal/generator/`
Expected: FAIL (`strings.EqualFold(` missing from auth.go).

- [x] **Step 3: Auth**

In all four auth templates replace `bearerToken` with:

```go
// bearerToken extracts the credential from an Authorization header. The
// scheme is case-insensitive (RFC 9110 §11.1).
func bearerToken(h string) string {
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
```

and add the RFC 6750 §3 challenges plus the header on each 401:

```go
// RFC 6750 §3 challenges: a missing credential gets the bare scheme, a
// rejected one names the reason.
const (
	challengeMissing = `Bearer`
	challengeInvalid = `Bearer error="invalid_token"`
)
```

fiber:

```go
		token := bearerToken(c.Get(fiber.HeaderAuthorization))
		if token == "" {
			c.Set(fiber.HeaderWWWAuthenticate, challengeMissing)
			return httpwriter.WriteError(c, errors.L(locale.Unauthorized).WithCause(errors.New("missing bearer token")))
		}
		principal, err := tokenSvc.ParseAndVerifyAccessToken(c.UserContext(), token)
		if err != nil {
			app := errors.ToAppError(err)
			if app.Code == fiber.StatusUnauthorized {
				c.Set(fiber.HeaderWWWAuthenticate, challengeInvalid)
			}
			return httpwriter.WriteError(c, app)
		}
```

gin: `c.Header("WWW-Authenticate", …)` and `app.Code == http.StatusUnauthorized` (import `net/http`); chi: `w.Header().Set("WWW-Authenticate", …)`; echo: `c.Response().Header().Set(echo.HeaderWWWAuthenticate, …)` and `app.Code == http.StatusUnauthorized`.

- [x] **Step 4: Limiter**

Replace the limiter core in all four templates (imports gain `"math"`, `"strconv"`, `"time"`):

```go
// ipLimiter holds one token bucket per client IP. Buckets idle for longer
// than visitorTTL are dropped by the first call after each sweepEvery
// interval, so memory is bounded by the clients seen recently rather than
// over the life of the process. It is per-process: N replicas allow N× the
// limit; global rate limiting belongs upstream (CDN, reverse proxy).
type ipLimiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	rps       rate.Limit
	burst     int
	lastSweep time.Time
}

type visitor struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

const (
	visitorTTL = 10 * time.Minute
	sweepEvery = time.Minute
)

func newIPLimiter(cfg config.LoginRateLimitConfig) *ipLimiter {
	rps := rate.Limit(cfg.RPS)
	if rps <= 0 {
		rps = rate.Limit(5.0 / 60.0) // 5 per minute
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = 5
	}
	return &ipLimiter{visitors: make(map[string]*visitor), rps: rps, burst: burst, lastSweep: time.Now()}
}

// allow reports whether ip may proceed now and, when it may not, how long
// until its bucket has a token again (the Retry-After value).
func (l *ipLimiter) allow(ip string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= sweepEvery {
		l.sweep(now)
	}
	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.rps, l.burst)}
		l.visitors[ip] = v
	}
	v.lastSeen = now
	res := v.lim.ReserveN(now, 1)
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return false, delay
	}
	return true, 0
}

func (l *ipLimiter) sweep(now time.Time) {
	for ip, v := range l.visitors {
		if now.Sub(v.lastSeen) > visitorTTL {
			delete(l.visitors, ip)
		}
	}
	l.lastSweep = now
}

// retryAfter renders a wait as the whole seconds Retry-After carries, never
// less than one.
func retryAfter(d time.Duration) string {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
```

Handlers (doc comment shared: "LoginRateLimit returns a per-IP token-bucket limiter for a credential endpoint (login, registration). Each call builds its own buckets, so the routes do not share a budget. The client IP comes from the framework, which honours X-Forwarded-For only from the configured trusted proxies."):

```go
// fiber
		ok, wait := l.allow(c.IP())
		if !ok {
			c.Set(fiber.HeaderRetryAfter, retryAfter(wait))
			return httpwriter.WriteError(c, errors.L(locale.TooManyRequests).WithCause(errors.New("login rate limit exceeded")))
		}
		return c.Next()
// gin
		ok, wait := l.allow(c.ClientIP())
		if !ok {
			c.Header("Retry-After", retryAfter(wait))
			httpwriter.WriteError(c, errors.L(locale.TooManyRequests).WithCause(errors.New("login rate limit exceeded")))
			c.Abort()
			return
		}
		c.Next()
// chi (RemoteAddr is the bare client IP after middleware.ClientIP)
			ok, wait := l.allow(r.RemoteAddr)
			if !ok {
				w.Header().Set("Retry-After", retryAfter(wait))
				httpwriter.WriteError(w, r, errors.L(locale.TooManyRequests).WithCause(errors.New("login rate limit exceeded")))
				return
			}
			next.ServeHTTP(w, r)
// echo
			ok, wait := l.allow(c.RealIP())
			if !ok {
				c.Response().Header().Set(echo.HeaderRetryAfter, retryAfter(wait))
				return httpwriter.WriteError(c, errors.L(locale.TooManyRequests).WithCause(errors.New("login rate limit exceeded")))
			}
			return next(c)
```

Registrars apply a second instance to registration: fiber `public.Post("/", middleware.LoginRateLimit(r.loginLimit), r.handler.Create)`; gin `public.POST("", middleware.LoginRateLimit(r.loginLimit), r.handler.Create)`; chi `sub.With(middleware.LoginRateLimit(r.loginLimit)).Post("/", r.handler.Create)`; echo `public.POST("", r.handler.Create, middleware.LoginRateLimit(r.loginLimit))`. Update the `Register` doc line to `POST /users (public — registration, rate limited like login)`.

- [x] **Step 5: Usecase**

`domain/security/hasher.go.tmpl`, before the interface:

```go
// MaxPasswordLen is the longest password, in bytes, a hasher must accept.
// bcrypt hashes only the first 72 bytes and rejects longer input, so the
// usecase refuses it up front instead of failing with a 500.
const MaxPasswordLen = 72
```

`usecase/user/service.go.tmpl`:

Service struct gains two fields (the package-level `dummyHash`/`dummyHashOnce` and `primeDummyHash` are deleted):

```go
	// timing hash for Login's unknown-email path; built on first use.
	dummyMu   sync.Mutex
	dummyHash string
```

`Register`, before the email lookup:

```go
	if len(input.Password) > security.MaxPasswordLen {
		return nil, errors.L(locale.InvalidRequest).
			WithCause(fmt.Errorf("password longer than %d bytes", security.MaxPasswordLen))
	}
```

`Update` and `Delete`, first line: `if err := requireSelf(ctx, id); err != nil { return nil, err }` (`return err` in Delete). Add:

```go
// requireSelf enforces that the authenticated principal is the user being
// mutated. It lives here, not in the handler, so every transport applies the
// same rule: no principal is 401, another user's id is 403. Widen it (roles,
// admin audience) here when the product needs it.
func requireSelf(ctx context.Context, id int64) error {
	p, ok := identity.UserPrincipalFromContext(ctx)
	if !ok {
		return errors.L(locale.Unauthorized).WithCause(errors.New("no principal in context"))
	}
	if p.UserID != id {
		return errors.L(locale.Forbidden).WithCause(fmt.Errorf("user %d may not modify user %d", p.UserID, id))
	}
	return nil
}
```

Replace the dummy-hash block and the start of `Login`:

```go
// timingHash returns the hash used to equalize the cost of an unknown-email
// login with a wrong-password login (bcrypt.CompareHashAndPassword is
// CPU-bound, so a fast "not found" would be an enumeration oracle). It is
// computed on first use; if hashing fails the login fails closed rather than
// answering quickly.
func (s *Service) timingHash() (string, error) {
	s.dummyMu.Lock()
	defer s.dummyMu.Unlock()
	if s.dummyHash == "" {
		h, err := s.hasher.Hash("dummy-password-for-timing-uniformity")
		if err != nil {
			return "", err
		}
		s.dummyHash = h
	}
	return s.dummyHash, nil
}

// Login verifies email+password and returns a signed access token + the user
// profile. Returns InvalidCredentials (401) for both "no such user" and "bad
// password" so attackers can't enumerate accounts via status or timing — the
// unknown-user path consumes a bcrypt verify against the timing hash so
// wall-clock time matches the bad-password path.
func (s *Service) Login(ctx context.Context, input LoginInput) (*LoginOutput, error) {
	dummy, err := s.timingHash()
	if err != nil {
		return nil, errors.Wrap(err, "login: timing hash")
	}

	user, err := s.userRepo.GetByEmail(ctx, input.Email)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			_ = s.hasher.Verify(dummy, input.Password)
			return nil, errors.L(locale.InvalidCredentials).WithCause(errors.New("account not found"))
		}
		return nil, errors.Wrapf(err, "login email=%s: lookup", input.Email)
	}
```

- [x] **Step 6: Verify**

Run: `make test`, `make lint`.
Expected: green.

- [ ] **Step 7: Commit**

Stop and ask the user to run `/grimoire-core:commit` (suggested: `feat(templates): RFC 6750 challenges, bounded limiter with Retry-After on login and registration, ownership on mutations`).

---

### Task 5: Readiness, OpenAPI, docs

**Files:**

- Modify: `internal/generator/templates/transport/http/health/checker.go.tmpl` (`Readiness` returns `(Status, error)`, details say `"fail"`)
- Modify: the four `*_router.go.tmpl` (`/readyz` logs the error)
- Modify: `internal/generator/templates/api/openapi/openapi.yaml.tmpl` (rewritten from the handlers)
- Modify: `internal/generator/templates/README.md.tmpl:52` (middleware list)
- Modify: `docs/06-usecase-layer.md`, `docs/07-transport-layer.md`, `docs/09-infrastructure-layer.md`, `docs/10-shared-packages.md`, `docs/12-build-and-deploy.md`, `docs/13-cross-cutting.md`, `docs/02-project-layout.md` (middleware files in the tree, if listed)
- Modify: `docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md` (phase 3 done at `98cd188`; phase 4 plan link)
- Test: `internal/generator/generator_test.go`

- [x] **Step 1: Extend the failing test**

```go
	// Readiness names the failing check without its error text; the OpenAPI
	// document matches the handlers.
	mustContainAll(t, readFile(t, filepath.Join(tr, "health/checker.go")),
		"func (c *Checker) Readiness(ctx context.Context) (Status, error)", `"fail"`)
	mustContainAll(t, router, `log.Warn("readiness check failed", "err", err)`)
	openapi := readFile(t, filepath.Join(dir, "api/openapi/openapi.yaml"))
	mustContainAll(t, openapi, "/api/v1/public/users:", "name: perPage", "name: cursor", "requestId:", "Retry-After:",
		"WWW-Authenticate:", "PageMeta:", "CursorMeta:", `"413":`)
	mustNotContain(t, openapi, "name: offset")
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestHTTPParity ./internal/generator/`
Expected: FAIL (`Readiness` signature).

- [x] **Step 3: Readiness**

`checker.go.tmpl`: import `"errors"` and `"fmt"` under `{{- if or .HasDatabase .HasRedis}}`; rewrite `Readiness`:

```go
// Readiness pings every configured dependency under a 1-second deadline. The
// body names each check as "ok" or "fail" so an anonymous caller learns which
// dependency is down but never its error text; the joined error carries the
// detail for the log.
func (c *Checker) Readiness(ctx context.Context) (Status, error) {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	details := map[string]string{}
	var errs []error
	check := func(name string, err error) {
		if err != nil {
			details[name] = "fail"
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		details[name] = "ok"
	}
{{- if and .HasDatabase (eq .Database "postgres")}}
	check("database", c.pool.Ping(probeCtx))
{{- end}}
{{- if and .HasDatabase (eq .Database "mysql")}}
	check("database", c.db.PingContext(probeCtx))
{{- end}}
{{- if .HasRedis}}
	check("redis", c.cache.Ping(probeCtx).Err())
{{- end}}

	return Status{OK: len(errs) == 0, Details: details}, errors.Join(errs...)
}
```

and the no-dependency variant `func (*Checker) Readiness(context.Context) (Status, error) { return Status{OK: true}, nil }`. Routers:

```go
		status, err := checker.Readiness(ctx)
		if err != nil {
			log.Warn("readiness check failed", "err", err)
		}
```

- [x] **Step 4: OpenAPI**

Rewrite `openapi.yaml.tmpl` from the handlers: `GET /api/v1/users` takes `page`, `perPage`, `email`; add `GET /api/v1/public/users` with `cursor`, `limit`; every envelope carries `requestId`, list envelopes carry `meta` (`PageMeta` / `CursorMeta`); `401` responses document `WWW-Authenticate`, `429` responses document `Retry-After`; POST bodies document `413`; `PUT`/`DELETE /users/{id}` document `403`; `CreateUserInput.password` has `maxLength: 72`; probes return `ProbeStatus` whose `details` values are `ok` or `fail`. (The full document is written in the task; see the template after this plan runs.)

- [x] **Step 5: Docs**

- `README.md.tmpl:52`: `middleware/     # RequestID, Logging, Recovery, Timeout{{if ne .HTTPFramework "fiber"}}, BodyLimit{{end}}{{if eq .HTTPFramework "chi"}}, ClientIP{{end}}, CORS, Locale, Auth, LoginRateLimit`.
- `docs/07-transport-layer.md`: router excerpt (fiber config block, `ErrorHandler`, `Timeout` in the chain), middleware-order table (Timeout after Recovery; BodyLimit/ClientIP where rendered), "Seven middlewares" → the new count and the per-framework extras, `cors.go` (empty list installs nothing; `Vary: Origin`), `auth.go` snippet (`EqualFold`, `WWW-Authenticate`), `loginlimit.go` (eviction, `Retry-After`, applied to login and registration), route table (registration rate limited; `PUT`/`DELETE` self only), the locale-flow diagram paragraph (framework errors via `errors.FromStatus`), the chi `client_ip` note.
- `docs/13-cross-cutting.md`: table rows (CORS: "empty list installs nothing"; new rows for client IP / trusted proxies, request deadline, body limit, framework errors in the envelope); security-posture rows (brute force on login and registration; ownership; password length; readiness detail).
- `docs/09-infrastructure-layer.md`: config excerpt with `request_timeout` and `trusted_proxies`; the CORS "defaults that carry a warning" paragraph.
- `docs/06-usecase-layer.md`: `timingHash` replaces the `sync.Once` snippet; `requireSelf` and the password bound.
- `docs/10-shared-packages.md`: `FromStatus` and the `context.DeadlineExceeded` → 504 mapping in the `pkg/errors` section.
- `docs/12-build-and-deploy.md`: `.env` notable entries gain `SERVER_TRUSTED_PROXIES`.
- Roadmap: phase 3 header `(done 2026-10-10, commit 98cd188)`; phase 4 gets `Detailed plan:` link to this file.

- [x] **Step 6: Verify everything, including a one-off chi bootstrap**

Run: `make test`, `make lint`, markdownlint on every edited `.md`, then alone: `make test-all`. Then, alone, render a chi project and run its real toolchain once (chi is not in the standing bootstrap set):

```bash
go run . new chi-check --module example.com/chi-check --transport http --http-framework chi --database postgres --cache redis --queue none --search none --di wire --force -o /tmp/nova-chi-check
cd /tmp/nova-chi-check && GOFLAGS='-mod=mod -p=2' GOGC=50 make gen && GOFLAGS='-mod=mod -p=2' go build ./... && GOGC=50 golangci-lint run
```

Expected: all green; the chi bootstrap prints no lint findings.

- [ ] **Step 7: Commit**

Stop and ask the user to run `/grimoire-core:commit` (suggested: `docs: describe the HTTP hardening contract; readiness hides error text; OpenAPI matches the handlers`).

---

## Self-review notes

- Spec coverage: every finding in the roadmap's phase 4 list maps to a task: CORS fallback (2), echo timeouts (2), body limits (2), spoofable limiter key and unbounded map (1, 4), fiber `UserContext` and no deadline (2, documented limitation), gin trailing-slash redirect (3), framework 404/405 outside the envelope and fiber's leaking default handler and the status=200 lines (3), ownership (4), readiness error text (5), OpenAPI drift (5), bearer case, `WWW-Authenticate`, `Retry-After`, `Vary: Origin`, chi `RemoteAddr` with port, unthrottled registration, bcrypt 72 bytes, empty dummy hash (2, 4, 1).
- Deliberately not done: a Redis-backed limiter (the per-process limiter now evicts; replicas still multiply the limit, documented), per-field validation errors, an admin role for `/users/{id}`.
- Type consistency: `allow` returns `(bool, time.Duration)` in all four; `FromStatus(int) *AppError`; `Readiness(ctx) (Status, error)`; `TrustedProxyNets() ([]*net.IPNet, error)`.
