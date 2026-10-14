# 6. Usecase layer

Layer 2 — application business rules. Depends on `domain/` and `pkg/` only, never on `infrastructure/`, a framework or a transport (`TestDependencyRule` in nova pins it). ([index](README.md))

A usecase **orchestrates**: it loads entities through ports, applies application policy (clamping, uniqueness checks, authorization), performs side effects, and returns its own DTOs. Entities never cross this boundary — the transport layer only ever sees `usecase/*.Output`.

Sections marked **not scaffolded** are patterns to grow into.

## What ships: internal/usecase/user/

`nova new` emits one usecase package per feature, always two files:

```bash
usecase/user/
├── service.go    # orchestration
└── dto.go        # Input/Output shapes (no json/validate tags)
```

For a `--transport=worker` project a second package, `usecase/useraudit/`, follows the same shape ([below](#useraudit--record-each-event-once)).

## internal/usecase/user/service.go

The service holds only ports — the token signer is `security.TokenIssuer` ([04](04-placement-rationale.md#33-where-to-put-adapter-interfaces--consumer-owns-the-interface)), so `service_test.go` runs every method against fakes. The publisher field only exists when a queue is selected:

```go
// Service implements the user use case. It never calls the logger directly —
// it wraps errors with errors.Wrapf and lets the transport boundary log once.
//
// Login lives on this service rather than a separate auth feature because
// authentication IS a user concern in the scaffold; it signs through the
// security.TokenIssuer port, so the usecase never imports infrastructure.
// Split it out when admin/service-account auth diverges from end-user auth.
type Service struct {
    userRepo  domain.UserRepository
    hasher    security.PasswordHasher
    tokens    security.TokenIssuer
    tx        domain.TxManager
    publisher domain.UserPublisher   // [--queue]
}
```

Seven methods ship: `Register`, `Get`, `Update`, `Delete`, `List`, `PublicList`, `Login`.

### Error discipline

Two error shapes, and the choice is not stylistic:

| Situation                  | Return                          | Why                                              |
| -------------------------- | ------------------------------- | ------------------------------------------------ |
| A rule the caller violated | `errors.L(locale.X, args…)`     | Carries a translatable code + HTTP status        |
| A dependency failed        | `errors.Wrapf(err, "op ctx=…")` | Adds caller frame + context, preserves the cause |

`L` returns `*errors.AppError` (it is never nil, so `.WithCause(err)` chains to attach the underlying error while the response stays user-facing); `Wrapf` returns `error`, so a nil cause yields a true nil rather than a typed-nil pointer. [10](10-shared-packages.md) documents the type.

The service **never logs** — except for one deliberate case (below). It wraps and returns; the transport boundary logs once. That keeps a single log line per request instead of one per layer.

### Register — hash, then check and insert in one transaction

```go
func (s *Service) Register(ctx context.Context, input RegisterInput) (*Output, error) {
    // Hash outside the transaction: bcrypt costs ~100ms and must not hold a
    // connection open.
    hash, err := s.hasher.Hash(input.Password)
    …
    user, err := entity.NewUser(input.Email, input.Name, hash)   // invariants → 400
    …
    txErr := s.tx.WithinTx(ctx, func(ctx context.Context) error {
        // Treat ErrNotFound as the only "ok, proceed" outcome; any other error
        // from the lookup (DB outage, network) must propagate so we don't
        // race-create a duplicate row when the uniqueness check itself failed.
        existing, lookupErr := s.userRepo.GetByEmail(ctx, user.Email)
        switch {
        case lookupErr == nil && existing != nil:
            return errors.L(locale.UserEmailExists, user.Email).WithCause(errors.New("email already registered"))
        case lookupErr != nil && !errors.Is(lookupErr, errors.ErrNotFound):
            return errors.Wrapf(lookupErr, "register user email=%s: lookup", user.Email)
        }
        if createErr := s.userRepo.Create(ctx, user); createErr != nil {
            // Two registrations can pass the check together; the unique index
            // decides, and the adapter reports it as ErrAlreadyExists.
            if errors.Is(createErr, errors.ErrAlreadyExists) {
                return errors.L(locale.UserEmailExists, user.Email).WithCause(createErr)
            }
            return errors.Wrapf(createErr, "register user email=%s", user.Email)
        }
        return nil
    })
```

Three things worth copying into your own usecases:

1. **The `switch` on the lookup error.** Collapsing it to `if err != nil { /* proceed */ }` turns a database outage into a duplicate insert. Only `ErrNotFound` means "free to create".
2. **The uniqueness check is advisory; the constraint decides.** Two concurrent registers can both pass the check. The loser's insert raises the unique violation, the adapter translates it to `errors.ErrAlreadyExists` ([08](08-adapter-layer.md#translating-the-unique-violation)), and the usecase turns that into the same `UserEmailExists` 409 — never a 500. `service_test.go` pins it with a fake repository whose `Create` returns the sentinel.
3. **Expensive work stays outside `WithinTx`.** The hash is computed first; only the check and the insert hold a connection.

The event publish is the one place the service logs, because the failure is deliberately swallowed:

```go
// best-effort publish — user is already persisted; broker outage shouldn't fail the request.
// Failure is logged so it can drive an alert; the API still returns success.
if pubErr := s.publisher.PublishUserCreated(ctx, domain.UserCreated{…}); pubErr != nil {
    logctx.From(ctx).Error("publish user.created failed", "user_id", user.ID, "error", pubErr)
}
```

An error that is never returned must be logged somewhere or it vanishes. If you need the event to be guaranteed, that is an outbox table written inside the same transaction — not a retry loop here.

### List — page-based, policy clamped in the usecase

Defaults and caps live in the usecase, not the handler, so every transport inherits one policy:

```go
const (
    listDefaultPerPage = 10
    listMaxPerPage     = 500
)
```

`List` clamps `Page`/`PerPage`, builds `domain.UserFilter`, then runs the page query **and** a `Count` against the same filter inside one `WithinTx` (the port contract says `Count` ignores `Limit`/`Offset`; one transaction gives the two reads one snapshot where the engine's isolation level provides it — MySQL's REPEATABLE READ does, Postgres READ COMMITTED does not, so raise it in `TxManager` if that matters). It returns the _clamped_ values in `ListOutput`, so the page meta the client sees matches what was actually queried — an out-of-range `per_page=9999` reports `per_page: 10`, not a lie.

Note that transport hands raw values in as `ListInput` and the service converts to `domain.UserFilter` internally: **transport never imports `domain`.**

### PublicList — cursor (keyset) paging

The second read shape, for unauthenticated callers. It exists separately because the two callers have different costs and different exposure:

- Reads `limit+1` rows to learn `HasMore` without a second query, then trims the extra.
- Encodes the last kept id as the next cursor: `base64.RawURLEncoding(strconv.FormatInt(id))`. Opaque, not encrypted — clients can't hand-craft offsets, but it is not a secret.
- Keyset (`id > cursor`) stays stable under concurrent inserts and avoids deep-`OFFSET` scans.
- Projects to `PublicOutput` — **the name only**. No email, no id, no timestamps: an anonymous caller cannot harvest addresses or learn anything beyond the display name.
- A malformed cursor is `errors.L(locale.InvalidRequest)` (400), not a 500 — the client sent back a token we issued, so a bad one is a bad request.

### Login — uniform failure, uniform timing

```go
// Login verifies email+password and returns a signed access token + the user
// profile. Returns InvalidCredentials (401) for both "no such user" and "bad
// password" so attackers can't enumerate accounts via status or timing — the
// unknown-user path consumes a bcrypt verify against a precomputed dummy
// hash so wall-clock time matches the bad-password path.
```

Both failure paths return `errors.L(locale.InvalidCredentials)`. Equal _status_ is the easy half; equal _timing_ is the half that gets forgotten. `bcrypt.CompareHashAndPassword` is CPU-bound, so a missing user would return in microseconds while a wrong password takes ~100ms — a reliable account-enumeration oracle even with identical response bodies. The fix:

```go
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
```

`Login` calls `timingHash()` first and fails closed (500) if the hash cannot be built, rather than comparing against an empty string and reopening the oracle; on the not-found path it runs `_ = s.hasher.Verify(dummy, input.Password)` before returning. One bcrypt op is paid on first use; every miss after that costs the same as a real verify.

On success it builds an `identity.UserPrincipal`, signs it, and returns the token plus `ExpiresIn` in seconds (RFC 6749 `expires_in`) — the transport does no token work at all.

### Ownership and input bounds

`Update` loads the entity, calls `user.Rename(name)` — the entity validates and stamps `UpdatedAt` — and persists it, so the `updatedAt` in the response is the value that was written, not the pre-update one. `Update` and `Delete` begin with `requireSelf(ctx, id)`: no principal in ctx is 401, another user's id is 403. The check lives in the usecase, not the handler, so a future gRPC or CLI transport inherits it; widen it here (roles, an admin audience) when the product needs it. `Register` refuses passwords over `security.MaxPasswordLen` (72 bytes, the bcrypt bound) with the generic 400 instead of letting the hasher fail with a 500.

## internal/usecase/user/dto.go

Plain structs, no tags. The transport layer owns `json`/`validate`; the assembler maps between the two ([07](07-transport-layer.md)).

```go
type RegisterInput struct{ Email, Name, Password string }
type UpdateInput struct{ Name string }

// Page is 1-based; the service clamps Page/PerPage and derives the SQL offset.
type ListInput struct {
    Email   string   // optional exact match; empty means no filter
    Page    int
    PerPage int
}

// Page/PerPage echo the clamped values the service actually used.
type ListOutput struct {
    Items       []*Output
    Page        int
    PerPage     int
    TotalRecord int64
}

type PublicListInput  struct{ Cursor string; Limit int }
type PublicListOutput struct{ Items []*PublicOutput; NextCursor string; HasMore bool }
type PublicOutput     struct{ Email, Name string }

// Output is the use-case's return shape — entities never cross this boundary.
type Output struct {
    ID                   int64
    Email, Name          string
    CreatedAt, UpdatedAt time.Time
}

type LoginInput  struct{ Email, Password string }
type LoginOutput struct {
    AccessToken string
    ExpiresIn   int64 // seconds; RFC 6749 expires_in
    User        *Output
}
```

Note there is no `Output` for `Delete` and no wrapper struct on the port side: the repository returns `[]*entity.User` and a count, and the usecase composes `ListOutput` itself. Result structs in the port would force both read shapes to share one type.

### Do you need a usecase DTO at all?

The **filter passthrough rule** from [05](05-domain-layer.md#internaldomainusergo--repository-port--filter-one-file-per-aggregate): if the usecase does zero transformation, let the transport assembler build `domain.UserFilter` directly and skip the DTO. The shipped `List` _does_ clamp, so `ListInput` earns its place.

## Package naming: the `svc` alias

Usecase packages are named after the aggregate (`package user`) but are **always imported with a `svc` suffix** at call sites, because the transport package for the same feature is also called `user`:

```go
import usersvc "{module}/internal/usecase/user"

func NewHandler(svc *usersvc.Service) *Handler { … }
```

`ordersvc`, `useraudit` → `userauditsvc`, and so on. Never import the bare name.

## Domain service implementations live here too

**Not scaffolded.**

A domain service interface lives in `domain/service/` ([05](05-domain-layer.md#internaldomainservicepricing_servicego)) but its implementation belongs in `usecase/`, because it is pure business logic with no external system behind it:

```go
package pricing

// Service implements domainservice.PricingService
type Service struct{}

func NewService() *Service { return &Service{} }

var _ domainservice.PricingService = (*Service)(nil)

func (s *Service) CalculateOrderTotal(
    ctx context.Context, items []entity.OrderItem, discount *entity.Discount,
) (valueobject.Money, error) {
    var total valueobject.Money
    for _, item := range items {
        total, _ = total.Add(item.Price.Multiply(item.Quantity))
    }
    if discount != nil {
        total = discount.Apply(total)
    }
    return total, nil
}
```

They are **injected into** other usecases, never called from a handler:

```bash
┌──────────┐      ┌──────────────┐      ┌────────────────┐
│ Handler  │ ───▶ │   Use Case   │ ───▶ │ Domain Service │
│(transport)│     │  (usecase/)  │      │ (usecase/ impl)│
└──────────┘      └──────────────┘      └────────────────┘
     ✗ NEVER directly ──────────────────────────▶
```

```go
// ✅ CORRECT — the use case injects and calls the domain service
type Service struct {
    orderRepo      domain.OrderRepository
    pricingService domainservice.PricingService
}

func (s *Service) PlaceOrder(ctx context.Context, input PlaceOrderInput) (*OrderOutput, error) {
    total, err := s.pricingService.CalculateOrderTotal(ctx, input.Items, input.Discount)  // 1. business rule
    if err != nil {
        return nil, err
    }
    order := entity.NewOrder(input.UserID, input.Items, total)                            // 2. entity
    if err := s.orderRepo.Create(ctx, order); err != nil {                                // 3. side effect
        return nil, err
    }
    return toOrderOutput(order), nil
}
```

```go
// ❌ WRONG — handler calling a domain service directly
func (h *OrderHandler) Create(c *fiber.Ctx) error {
    total := h.pricingService.CalculateOrderTotal(...)  // VIOLATES layering!
}
```

## Repository vs domain service vs usecase

| Scenario                                                                  | Where?                  | Why?                                                             |
| ------------------------------------------------------------------------- | ----------------------- | ---------------------------------------------------------------- |
| "List users with role=admin" (requires JOIN)                              | `repository`            | Pure data retrieval, no business logic — JOINs are SQL mechanics |
| "Get top 10 sellers by revenue" (JOIN + aggregation)                      | `repository`            | Aggregation query — returns data, not a decision                 |
| "Calculate total price with tax and discount"                             | `domain/service/`       | Enterprise-wide pricing rule, reusable across use cases          |
| "Place an order" (validate → calculate price → create order → send email) | `usecase/`              | Application workflow orchestrating multiple steps                |
| "Check if user is eligible for resale"                                    | `domain/service/`       | Business eligibility rule, pure logic                            |
| "Submit resale post" (check eligibility → create post → notify)           | `usecase/`              | Application flow with side effects                               |
| "Determine loyalty tier from user + order history"                        | `domain/service/`       | Business computation using multiple entities                     |
| "Validate email format"                                                   | `domain/valueobject/`   | Single-entity validation, belongs on the value object            |
| "Hash password"                                                           | `domain/security/` port | Utility, not a cross-entity rule                                 |

| Aspect                    | Repository                 | Domain Service              | Use Case                    |
| ------------------------- | -------------------------- | --------------------------- | --------------------------- |
| **Purpose**               | Data retrieval/persistence | Business rules/computation  | Orchestrate workflows       |
| **Interface**             | `domain/{aggregate}.go`    | `domain/service/`           | N/A (concrete)              |
| **Implementation**        | `adapter/repository/`      | `usecase/{name}/service.go` | `usecase/{name}/service.go` |
| **Contains I/O?**         | YES (DB queries)           | NO (pure logic)             | YES (DB, HTTP, messaging)   |
| **Returns**               | Data (entities, lists)     | Decisions/computations      | Operation results           |
| **JOINs/multi-table?**    | YES — SQL is impl detail   | NO — receives data as args  | Uses repos to get data      |
| **Called by**             | Use cases                  | Use cases                   | Handlers (via assembler)    |
| **Called from handlers?** | NEVER directly             | NEVER directly              | YES                         |

> **Rule of thumb**:
>
> - **Returns data** (even with JOINs across 10 tables) → **repository**
> - **Returns a decision/computation** using multiple entities, no I/O → **domain service**
> - **Orchestrates** steps with side effects (DB writes, events, emails) → **use case**

## useraudit — record each event once

The worker-side usecase has one method, `RecordUserCreated`, and one subtlety: the broker delivers at least once. The producer mints `eventId` per event ([08](08-adapter-layer.md#adapterpubsub--the-event-publisher)), the row stores it under a unique index, and the repository turns a conflicting insert into `domain.ErrEventAlreadyRecorded`. The service treats that as success and emits the one log line it owns, `audit: duplicate delivery ignored` with the `event_id`, so a redelivery storm is visible without any row being written twice. Anything else is wrapped with the event id and returned, which the worker's policy retries and then parks ([07](07-transport-layer.md#one-policy-three-outcomes)).
