# 5. Domain layer

Layer 1 — entities, value objects, ports. Zero external dependencies: no framework, no DB, no
`json` tags. ([index](README.md))

Sections marked **not scaffolded** are patterns to grow into; `nova new` doesn't emit them.

## internal/domain/entity/user.go

Domain entities have NO framework tags. Serialization is a transport concern. The stored
credential is the hash — plaintext passwords never live on the entity.

```go
package entity

import "time"

type User struct {
    ID           int64
    Email        string
    Name         string
    PasswordHash string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

The scaffold has no `NewUser` constructor: the only invariant at creation time is "the password
is hashed", and that hashing belongs to the usecase (it holds the `PasswordHasher` port). Add a
constructor when the entity gains invariants it can enforce alone.

## internal/domain/valueobject/ — what are value objects?

**Not scaffolded.**

Value objects represent **concepts that are defined by their attributes, not by identity**.
Two value objects with the same attributes are considered equal (unlike entities which have unique IDs).

Use value objects to:

- **Enforce invariants** at creation time (e.g. an Email must be valid)
- **Eliminate primitive obsession** (don't pass raw `string` for email, `int64` for money)
- **Make the domain self-documenting** (function signatures tell you what they expect)

```go
// ❌ Primitive obsession — what does this mean?
func CreateOrder(userID int64, amount int64, currency string, email string) error

// ✅ Value objects — self-documenting, valid by construction
func CreateOrder(userID int64, amount valueobject.Money, email valueobject.Email) error
```

### internal/domain/valueobject/email.go

```go
package valueobject

import (
    "fmt"
    "regexp"
    "strings"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Email is a value object — immutable, valid by construction.
type Email struct {
    value string
}

func NewEmail(raw string) (Email, error) {
    normalized := strings.TrimSpace(strings.ToLower(raw))
    if !emailRegex.MatchString(normalized) {
        return Email{}, fmt.Errorf("invalid email: %s", raw)
    }
    return Email{value: normalized}, nil
}

func (e Email) String() string { return e.value }
func (e Email) IsZero() bool   { return e.value == "" }
```

### internal/domain/valueobject/money.go

```go
package valueobject

import "fmt"

// Money represents a monetary amount in its smallest unit (e.g. cents, đồng).
// Value object: two Money values with same Amount+Currency are equal.
type Money struct {
    Amount   int64  // smallest unit (cents/đồng)
    Currency string // ISO 4217: "VND", "USD"
}

func NewMoney(amount int64, currency string) Money {
    return Money{Amount: amount, Currency: currency}
}

func (m Money) Add(other Money) (Money, error) {
    if m.Currency != other.Currency {
        return Money{}, fmt.Errorf("cannot add %s to %s", m.Currency, other.Currency)
    }
    return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}, nil
}

func (m Money) Multiply(quantity int) Money {
    return Money{Amount: m.Amount * int64(quantity), Currency: m.Currency}
}

func (m Money) GreaterThan(other Money) bool {
    return m.Amount > other.Amount
}

func (m Money) IsZero() bool { return m.Amount == 0 }
```

**When to use value objects vs plain types:**

| Scenario      | Use                     | Why                                              |
| ------------- | ----------------------- | ------------------------------------------------ |
| Email address | `valueobject.Email`     | Must validate format, normalize case             |
| Money/price   | `valueobject.Money`     | Must track currency, prevent mixed-currency math |
| User ID       | `int64` (plain)         | No invariants to enforce, just an identifier     |
| Date range    | `valueobject.DateRange` | Must enforce start < end                         |
| Status string | `string` or `const`     | Simple enum, no complex rules                    |

## internal/domain/user.go — repository port + filter (one file per aggregate)

Repository interfaces and their filter structs live **in the `domain` package** as one file per
aggregate. This avoids the `repository.UserRepository` stutter — the call site becomes
`domain.UserRepository`, which reads naturally.

**Why not a `domain/repository/` sub-package?**

- `repository.UserRepository` stutters — the package name repeats in the type name
- A single `port.go` file mixing all interfaces + filter structs gets messy fast
- One file per aggregate keeps each file focused: interface + filter together

```go
// internal/domain/user.go
package domain

import (
    "context"

    "{module}/internal/domain/entity"
)

// UserFilter defines filtering/pagination for user queries. Part of the
// repository contract — no framework tags. Zero values mean "no filter".
type UserFilter struct {
    Email  string
    Name   string
    Limit  int32
    Offset int32
}

// UserRepository defines the data access contract for users.
type UserRepository interface {
    Create(ctx context.Context, user *entity.User) error
    GetByID(ctx context.Context, id int64) (*entity.User, error)
    GetByEmail(ctx context.Context, email string) (*entity.User, error)
    Update(ctx context.Context, user *entity.User) error
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context, filter UserFilter) ([]*entity.User, error)
    // Count returns the total rows matching filter, ignoring its Limit/Offset.
    // The admin (page-based) List needs it to compute total pages/records.
    Count(ctx context.Context, filter UserFilter) (int64, error)
    // ListAfter returns up to limit users with id greater than cursor, ordered
    // by id — the keyset read behind the public (cursor-based) list. cursor 0
    // starts from the beginning. Pass limit+1 to detect whether more remain.
    ListAfter(ctx context.Context, cursor int64, limit int32) ([]*entity.User, error)
}
```

Two reads, two shapes: `List` + `Count` back the admin page-based listing, `ListAfter` backs the
public cursor (keyset) listing. Keeping them as separate methods means neither caller pays for
the other's cost — the cursor path never runs a `COUNT(*)`, and the admin path never fakes
totals. The usecase composes the pieces into its own output DTO
([06](06-usecase-layer.md)); the port deliberately returns no result struct.

**Call site examples:**

```go
// ✅ domain.UserRepository — clean, no stutter
func NewService(userRepo domain.UserRepository) *Service { ... }

// ✅ domain.UserFilter — filter lives next to the interface that uses it
users, err := s.userRepo.List(ctx, domain.UserFilter{Email: email, Limit: 20, Offset: 0})
```

For projects with many aggregates, each gets its own file:

```bash
domain/
├── entity/
│   ├── user.go
│   └── order.go
├── user.go          # UserRepository + UserFilter
├── order.go         # OrderRepository + OrderFilter
├── service/
├── event/
└── valueobject/
```

**Filter passthrough rule**: If the use case does zero transformation on the filter
(no validation, no business rules), skip the usecase-level DTO and let the transport
assembler map directly to `domain.UserFilter`. Only add a usecase DTO when the use case
**does something** (clamps pagination, applies authorization rules, combines data from
multiple repos). The shipped user service does clamp, so it has `ListInput`.

## internal/domain/service/pricing_service.go

**Not scaffolded.**

Domain services contain **cross-entity business rules** that don't naturally belong to a single entity.
They are INTERFACES in the domain layer, implemented in the usecase layer.

> **⚠️ Common misconception: "multiple tables" ≠ "domain service"**
>
> A query that JOINs multiple tables (e.g., "list users with role=admin") is **NOT** a domain service.
> It's a **repository method** — pure data retrieval with zero business logic.
> The fact that it requires a SQL JOIN is an implementation detail of how the data is stored.
>
> Domain services are for **business rules, computations, and decisions** — not data fetching.

**The mental test — ask yourself:**

```bash
"Does this method RETURN DATA or RETURN A DECISION/COMPUTATION?"

Return data       → Repository    (even if it requires JOINs across 10 tables)
Return a decision → Domain service (even if it only involves 1 entity)
```

**Concrete examples:**

| Operation                                               | Where?             | Why?                                         |
| ------------------------------------------------------- | ------------------ | -------------------------------------------- |
| "Get users where role=admin and status=active" (JOIN)   | **Repository**     | Pure data retrieval, no business logic       |
| "Get top 10 sellers by revenue" (JOIN orders + users)   | **Repository**     | Aggregation query, no decision               |
| "Calculate discount based on user tier + order history" | **Domain service** | Business computation using multiple entities |
| "Determine if order should be auto-approved"            | **Domain service** | Business decision based on rules             |

> **Key insight**: If you switched from PostgreSQL (JOINs) to MongoDB (embedded documents),
> the JOIN disappears but the repository interface stays the same. The caller says
> "give me admin users" — it doesn't care about JOINs. That's why it's a repository concern.

```go
package service

import (
    "context"

    "{module}/internal/domain/entity"
    "{module}/internal/domain/valueobject"
)

// PricingService defines cross-entity pricing rules.
// This belongs in domain because pricing is an enterprise-wide business rule
// that may involve multiple entities (Order, Product, Discount, Tax).
//
// NOTE: This is for COMPUTATION, not data retrieval. Cross-entity queries
// (even with JOINs) belong in the repository interface, not here.
type PricingService interface {
    CalculateOrderTotal(ctx context.Context, items []entity.OrderItem, discount *entity.Discount) (valueobject.Money, error)
    IsEligibleForDiscount(ctx context.Context, user *entity.User, order *entity.Order) (bool, error)
}
```

## internal/domain/event/ — domain events

**Not scaffolded as a package.** `nova new` puts one publisher port + its event struct per
aggregate directly in `domain/` — `domain.UserPublisher` and `domain.UserCreated` in
[domain/user_publisher.go](../internal/generator/templates/domain/user_publisher.go.tmpl) —
matching how `domain.UserRepository` lives in `domain/user.go`:

```go
// internal/domain/user_publisher.go — what ships today
package domain

import (
    "context"
    "time"
)

type UserCreated struct {
    UserID    int64
    Email     string
    Name      string
    CreatedAt time.Time
}

type UserPublisher interface {
    PublishUserCreated(ctx context.Context, evt UserCreated) error
}
```

The `event/` package below is the shape to grow into once several aggregates publish events.

### internal/domain/event/event.go

Domain events represent **something that happened** in the system.
They are published by use cases after successful business operations.

```go
package event

import "time"

// UserCreated is published when a new user registers.
type UserCreated struct {
    UserID    int64
    Email     string
    Name      string
    CreatedAt time.Time
}

// OrderPlaced is published when an order is successfully placed.
type OrderPlaced struct {
    OrderID  int64
    UserID   int64
    Total    int64
    PlacedAt time.Time
}
```

### internal/domain/event/publisher.go

The `EventPublisher` interface is a **port** — it defines WHAT can be done (publish events),
not HOW (Kafka, RabbitMQ, NATS). This allows swapping message brokers without changing business logic.

Note: the interface accepts domain event structs (no `json` tags). The **adapter** is responsible
for mapping domain events → message DTOs with `json` tags before serializing to the wire.

```go
package event

import "context"

// EventPublisher is a port for publishing domain events to a message broker.
// Implemented by adapter/pubsub/.
//
// The adapter implementation maps domain events → wire-format DTOs (with json tags)
// before publishing. Domain events stay free of serialization concerns.
//
// No Close() method — resource cleanup is owned by each component's constructor
// via the (instance, cleanup, error) return pattern. DI composes all cleanups.
type EventPublisher interface {
    PublishUserCreated(ctx context.Context, evt UserCreated) error
    PublishOrderPlaced(ctx context.Context, evt OrderPlaced) error
}
```

**Why typed methods instead of `Publish(topic string, payload any)`?**

- Compile-time safety: you can't accidentally publish the wrong event type
- Each method knows its topic name — callers don't pass raw strings
- The adapter maps each event to its own message DTO with correct `json` field names
