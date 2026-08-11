# 4. Placement rationale

Why each package sits where it does, and who owns an adapter's interface. ([index](README.md))

Every directory placement should survive the question: **"Why HERE and not somewhere else?"** Copying a layout from another project without this reasoning leads to inconsistencies.

## 3.2 Why things live where they do

### Why `middleware/` is under `transport/http/`, NOT `transport/`

Middleware is a **Fiber/net-http concept** — it wraps `fiber.Handler` or `http.Handler`. gRPC has a completely different mechanism: **interceptors** (`grpc.UnaryServerInterceptor`). Placing middleware at `transport/middleware/` implies it applies to all transports, but it doesn't.

```bash
transport/
├── http/
│   └── middleware/       # ← HTTP middleware (framework signatures)
└── grpc/
    └── interceptor/      # ← gRPC middleware (interceptor signatures)
```

If you put them at the same level (`transport/middleware/`), you either:

- Mix HTTP and gRPC middleware in one package (different signatures, confusing)
- Name it `http_middleware/` anyway (defeats the purpose)

**Each transport owns its cross-cutting concerns.** This also means if you remove gRPC, you just delete `transport/grpc/` — nothing under `transport/http/` changes.

### Why `httpwriter/` is under `transport/http/`, NOT `pkg/`

The response **envelope** is framework-free and lives in `pkg/httputil/response.go`. The **writer** cannot follow it, because `httpwriter/writer.go` has these imports:

```go
import (
    "strconv"

    "github.com/go-playground/validator/v10"  // binding + validation
    "github.com/gofiber/fiber/v2"             // framework-specific

    "{module}/pkg/errors"                     // *errors.AppError, StashAppError
    "{module}/pkg/httputil"
    "{module}/pkg/locale"
)
```

Two disqualifiers from `pkg/`:

1. **Framework-coupled** — it accepts `*fiber.Ctx`, not `http.ResponseWriter`. Switching HTTP frameworks rewrites this file; the envelope beside it never changes.
2. **Transport-specific** — binding, validation and the request-scoped `AppError` stash are HTTP concerns, meaningless to a worker or a gRPC service.

General rule: if a package imports `internal/` or a specific framework, it must live inside `internal/`. (Status classification is _not_ on this list — it lives in `pkg/errors`, framework-free, so every transport maps a locale code to the same status.)

### Why `pkg/locale/` is in `pkg/`, not `internal/`

Locale codes are **consumed by multiple layers**:

- **usecase/** returns `errors.L(locale.UserNotFound)`
- **transport/** translates codes to HTTP/gRPC responses

`pkg/` (or an equivalent like `internal/shared/`) signals: "this package has no layer affiliation — it's a cross-cutting utility." It depends on nothing inside `internal/`.

> **Trade-off note**: Strict Clean Architecture says usecases should only depend on domain. Having usecases import `pkg/locale` couples them to your error code system. The purist alternative is: usecases return plain `error` values or domain error types, and the transport layer maps those to locale codes. We choose the pragmatic approach — `errors.L(locale.SomeCode)` is simpler and avoids duplicating error mapping in every transport handler.

**If you prefer stricter layering**, define error types in `domain/`:

```go
// domain/errors.go
var ErrUserNotFound = errors.New("user not found")

// usecase/user/service.go
return domain.ErrUserNotFound

// transport/http/httpwriter/writer.go
switch {
case errors.Is(err, domain.ErrUserNotFound):
    code = locale.UserNotFound
}
```

### Why `transport/` is a sibling of `adapter/`, not inside it

In Clean Architecture theory, HTTP handlers ARE adapters — they adapt external requests to internal use case calls. So `transport/` is technically part of the adapter layer.

We separate them for **practical, not theoretical** reasons:

- **Scale**: `adapter/` (repos, pubsub, external clients) and `transport/` (HTTP, gRPC, cronjob) serve different roles. As the project grows, mixing them creates a bloated `adapter/` directory.
- **Team ownership**: the team working on HTTP endpoints rarely touches repository implementations.
- **Deletion boundary**: removing gRPC means deleting `transport/grpc/`. No adapter code changes.

> Think of it as: `adapter/` implements **outbound** interfaces (your app calls external systems), `transport/` handles **inbound** delivery (external world calls your app).

### `pkg/` vs `internal/shared/` — do you actually need `pkg/`?

In Go, `pkg/` has **no special compiler meaning** (unlike `internal/` which enforces visibility). It's purely convention signaling "reusable across projects."

| Approach           | Use when                                                      |
| ------------------ | ------------------------------------------------------------- |
| `pkg/`             | You build a library or want to signal cross-project reuse     |
| `internal/shared/` | Private monorepo; no external consumers will ever import this |

For a microservice that nobody imports, `internal/shared/` is equally valid. We use `pkg/` here because it's a well-known Go convention and visually distinguishes "shared utilities" from "layered architecture code" inside `internal/`.

## 3.3 Where to put adapter interfaces — consumer owns the interface

Adapters (JWT, payment gateway, email sender, external APIs) need interfaces for dependency inversion. The rule: **the consumer owns the interface, not the implementor**.

### If the usecase calls it → interface in `domain/`

When a usecase needs to call an external service, the interface lives in `domain/` — same package as repository interfaces, one file per concern:

```go
// internal/domain/payment.go
package domain

import "context"

type PaymentGateway interface {
    Charge(ctx context.Context, orderID int64, amount int64) error
    Refund(ctx context.Context, transactionID string) error
}
```

```go
// internal/domain/notification.go
package domain

type NotificationSender interface {
    SendEmail(ctx context.Context, to, subject, body string) error
}
```

```go
// internal/domain/auth.go — only if usecase needs to GENERATE tokens (e.g. login flow)
package domain

type TokenGenerator interface {
    GenerateToken(userID int64, role string) (string, error)
}
```

Implementations live in `adapter/`, unless the port is pure computation with no external system behind it — that lands in `infrastructure/` ([03](03-architecture-rules.md#ports-implemented-outside-adapter)):

```go
// internal/adapter/external/stripe.go
var _ domain.PaymentGateway = (*StripeClient)(nil)

// internal/infrastructure/jwt/service.go — implements domain.TokenGenerator
// (signing is pure computation, so infrastructure/ rather than adapter/)
var _ domain.TokenGenerator = (*TokenService)(nil)
```

> **Shipped variant:** `nova new` does **not** emit `domain/auth.go`. The user service holds a concrete `*jwt.TokenService` (`internal/infrastructure/jwt`) and calls it directly from `Login`, so the scaffold has one usecase → infrastructure import. It buys a working login flow with no extra indirection; the cost is that mocking `Login` in a unit test means constructing a real `TokenService` (a keypair) instead of a fake. Introduce `domain.TokenGenerator` and bind it in DI as soon as you want that seam — nothing else in the layout has to change.

### If only transport calls it → NO domain interface

JWT **verification** is typically a middleware concern — it runs before the handler, extracts user identity, and passes it via context. The usecase never calls "verify JWT" directly.

```bash
Request → [JWT Middleware] → Handler → UseCase
              ↑                          ↑
         extracts userID           receives the identity from context
         from token                (doesn't know about JWT at all)
```

No domain interface needed — the verifier is used directly by middleware:

```go
// internal/infrastructure/jwt/service.go — used directly by transport middleware
package jwt

func (s *TokenService) Verify(token string) (*Claims, error) { ... }
```

```go
// internal/transport/http/middleware/auth.go
func Auth(tokenSvc *jwt.TokenService) fiber.Handler {
    return func(c *fiber.Ctx) error {
        claims, err := tokenSvc.Verify(bearer(c.Get("Authorization")))
        // put the principal in ctx (domain/identity), then c.Next()
    }
}
```

### Decision flowchart

```bash
"Who calls this adapter?"
        │
   ┌────┴─────────┐
   │              │
UseCase        Transport only
   │              │
   ▼              ▼
domain/        No domain interface.
(one file      Adapter used directly
per concern)   by middleware/handler.
```

### Summary table

| Adapter                   | Usecase calls it? | Interface location                        | Implementation                  |
| ------------------------- | ----------------- | ----------------------------------------- | ------------------------------- |
| User repository           | Yes               | `domain/user.go`                          | `adapter/repository/postgres/`  |
| Payment gateway           | Yes               | `domain/payment.go`                       | `adapter/external/stripe.go`    |
| Email/SMS sender          | Yes               | `domain/notification.go`                  | `adapter/external/sendgrid.go`  |
| Token generator (login)   | Yes               | `domain/auth.go` — not shipped; see above | `infrastructure/jwt/service.go` |
| JWT verifier (middleware) | No                | None                                      | `infrastructure/jwt/service.go` |
| Rate limiter              | No                | None                                      | Middleware concern              |

### File layout for `domain/` with adapter interfaces

```bash
domain/
├── entity/
│   ├── user.go
│   └── order.go
├── user.go              # UserRepository + UserFilter
├── order.go             # OrderRepository + OrderFilter
├── payment.go           # PaymentGateway (usecase calls it)
├── notification.go      # NotificationSender (usecase calls it)
├── auth.go              # TokenGenerator (usecase calls it for login)
├── service/             # Domain service interfaces (business rules)
├── event/               # Domain events + EventPublisher interface
└── valueobject/         # Value objects (Email, Money, etc.)
```

> **One sentence rule**: If a usecase method needs to call it, define the interface in `domain/`. If only the transport layer uses it, skip the interface and use the adapter directly.
