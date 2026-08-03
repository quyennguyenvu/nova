# 3. Architecture rules

The dependency rule the layout enforces, and the adapter-vs-infrastructure decision guide.
([index](README.md))

## Clean Architecture principles to enforce

1. **Dependency Rule**: Source code dependencies point inward only
   - Domain layer has ZERO external dependencies (no framework imports)
   - Use case layer depends only on domain
   - Adapter layer depends on domain and use case
   - Infrastructure depends on all inner layers

2. **Dependency Inversion**:
   - High-level modules (use cases) define interfaces
   - Low-level modules (repositories) implement them
   - Interfaces live in the domain layer

3. **Separation of Concerns**:
   - Entities contain enterprise-wide business rules
   - Use cases contain application-specific business rules
   - Adapters convert data between layers
   - Infrastructure handles external concerns

## 3.1 Adapter vs Infrastructure: the decision guide

This is the most confusing part of Clean Architecture. Here's the definitive distinction:

### ADAPTER layer — "implements business interfaces"

| Characteristic               | Description                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------ |
| **Purpose**                  | Implements domain ports by carrying out business operations on an external system          |
| **Question to ask**          | "Does this fulfil a domain port by talking to an external system?"                         |
| **Contains business logic?** | Yes - data transformation, mapping, business-aware error handling                          |
| **Examples**                 | Repository implementations, HTTP handlers, gRPC handlers, external API clients, assemblers |

**Adapter components:**

```bash
adapter/
├── repository/              # Implements domain.XxxRepository
│   ├── postgres/            # SQL implementation
│   ├── mysql/               # SQL implementation
│   ├── redis/               # Cache-aside decorator
│   └── elasticsearch/       # Search index
├── pubsub/                  # Event publishing implementations
│   └── user_publisher.go    # Implements domain.UserPublisher
└── external/                # Implements domain interfaces for external APIs
    └── stripe_payment.go    # Implements domain.PaymentGateway

transport/                   # Delivery mechanisms (separate from adapter)
├── http/
│   ├── middleware/          # HTTP-specific middleware (framework handlers)
│   ├── httpwriter/          # HTTP-specific bind + envelope writer
│   └── v1/
│       ├── user/            # Per-feature: handler, dto, assembler, registrar
│       └── order/
├── grpc/
│   ├── interceptor/         # gRPC-specific middleware (interceptors)
│   └── v1/seller/
└── cronjob/                 # Cron job handlers (no middleware)
```

`adapter/external/` and `transport/cronjob/` are patterns to grow into — `nova new` doesn't
scaffold them.

### INFRASTRUCTURE layer — "provides technical capabilities"

| Characteristic               | Description                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------- |
| **Purpose**                  | Provides technical capabilities; may satisfy a port, but does no business I/O    |
| **Question to ask**          | "Is this a technical capability rather than a business operation?"               |
| **Contains business logic?** | NO - only technical setup                                                        |
| **Examples**                 | Connection factories, server bootstrap, config loading, logging setup, DI wiring |

**Infrastructure components:**

```bash
infrastructure/
├── config/            # Config loading (returns *Config)
├── database/          # Connection factory (returns *pgxpool.Pool)
├── cache/             # Connection factory (returns *cache.Client over redis.UniversalClient)
├── pubsub/            # Connection factory (returns the producer / consumer group)
├── search/            # Connection factory (returns the ES client)
├── server/            # Server bootstrap and graceful shutdown
├── logger/            # Logger setup (zerolog behind observability.Logger)
├── jwt/               # TokenService — signs at login, verifies in middleware
├── security/          # bcrypt PasswordHasher (port impl, no business I/O)
├── tracing/           # OpenTelemetry provider + cleanup
└── di/                # Wires everything together
```

### Decision flowchart

```bash
┌─────────────────────────────────────────────────────────┐
│ Does it run a business operation on an external system? │
└─────────────────────────────────────────────────────────┘
                         │
            ┌────────────┴────────────┐
            │                         │
           YES                        NO
            │                         │
            ▼                         ▼
     ┌──────────┐          ┌──────────────────────────┐
     │ ADAPTER  │          │ Does it import anything  │
     └──────────┘          │     under internal/?     │
                           └──────────────────────────┘
                                      │
                         ┌────────────┴────────────┐
                         │                         │
                        YES                        NO
                         │                         │
                         ▼                         ▼
                  ┌──────────────┐          ┌──────────┐
                  │INFRASTRUCTURE│          │   pkg/   │
                  └──────────────┘          └──────────┘
```

### Concrete examples

| Component                                           | Where?              | Why?                                             |
| --------------------------------------------------- | ------------------- | ------------------------------------------------ |
| `database.NewPostgresDB()` → `*pgxpool.Pool`        | **infrastructure/** | Just creates the pool, no business logic         |
| `UserRepository.Create(user)` → runs INSERT         | **adapter/**        | Implements `domain.UserRepository`               |
| `cache.New()` → `redis.UniversalClient`             | **infrastructure/** | Just creates the client                          |
| `UserCache.GetByID()` → checks cache, falls through | **adapter/**        | Implements caching strategy for a domain port    |
| `config.Load()` → `*Config`                         | **infrastructure/** | Pure technical config loading                    |
| `HTTPServer.Start()` → listens on port              | **infrastructure/** | Server bootstrap                                 |
| `Handler.Create(c *fiber.Ctx)`                      | **transport/**      | Calls use case, transforms request/response      |
| `StripeClient.Charge()`                             | **adapter/**        | Implements `domain.PaymentGateway`               |
| `logger.New()` → `*logger.Logger`                   | **infrastructure/** | Logger setup                                     |
| `wire.Build()` → wires dependencies                 | **infrastructure/** | DI setup                                         |
| `Publisher.PublishUserCreated()`                    | **adapter/**        | Implements `domain.UserPublisher`                |
| `pubsub.NewKafkaProducer()` → `sarama.SyncProducer` | **infrastructure/** | Just creates the producer                        |
| `BcryptHasher.Hash(plain)` → returns a hash         | **infrastructure/** | Port impl, but no business I/O; pure computation |
| `logger.Logger.Info()` → writes a log line          | **infrastructure/** | Port impl too; cross-cutting, no business I/O    |

### The key insight

> **Adapter** = "I know how to talk TO the business" (fulfils domain ports against external systems)
>
> **Infrastructure** = "I know how to SET UP technical resources" (provides raw capabilities)

Think of it this way:

- Infrastructure gives you `*pgxpool.Pool`
- Adapter uses that pool to implement `domain.UserRepository`

### Ports implemented outside `adapter/`

Satisfying a port is necessary but not sufficient for `adapter/`. The implementation must also
_carry out a business operation against an external system_ — persist, publish, charge, notify.
Two ports in this layout are implemented in `infrastructure/`:

- `domain/security.PasswordHasher` → `infrastructure/security/bcrypt.go`.
  `Hash(plain string) (string, error)` is pure computation: no external system, no
  `context.Context`, no I/O, nothing to map. A technical capability the usecase consumes,
  like a connection pool.
- `pkg/observability.Logger` → `infrastructure/logger/zerolog.go`. It does write output, but
  logging is cross-cutting, not a business operation.
  [04](04-placement-rationale.md) documents this split.

Note the test is not "does the signature mention an entity" — `StripeClient.Charge(ctx,
orderID, amount)` takes only scalars and is still an adapter, because it runs a business
operation over the network and translates provider failures into domain errors. Likewise
`adapter/repository` maps `*entity.User` ↔ SQL rows and owns the transaction. Bcrypt does
none of that; its port exists purely for dependency inversion, so inner layers can name the
capability without importing `infrastructure/`.

`infrastructure/jwt` sits outside `adapter/` for the same reason (signing is computation, not
I/O) — and its verification path needs no port at all, because only middleware calls it. See
[04](04-placement-rationale.md#33-where-to-put-adapter-interfaces--consumer-owns-the-interface).
