# 8. Adapter layer

Layer 3 — implementations of domain ports that carry out business operations against external
systems. Repositories, caches, search indexes, event publishers.
([index](README.md))

An adapter answers "does this fulfil a domain port by talking to an external system?" with yes.
Anything that merely *creates* the client is infrastructure — see the decision guide in
[03](03-architecture-rules.md#31-adapter-vs-infrastructure-the-decision-guide).

```bash
adapter/
├── repository/
│   ├── postgres/            # [--database=postgres]
│   │   ├── tx_manager.go    # implements domain.TxManager
│   │   ├── qx.go            # TX-aware query executor
│   │   ├── user_repository.go
│   │   ├── dbgen/           # sqlc output — generated, not templated
│   │   └── mapper/          # row ↔ entity, pure functions
│   ├── mysql/               # [--database=mysql] same shape
│   ├── redis/               # [--cache=redis] cache-aside decorator
│   └── elasticsearch/       # [--search=elasticsearch] derived read model
└── pubsub/                  # [--queue]
    ├── publisher.go         # broker transport (the kafka/rabbitmq variant)
    ├── user_publisher.go    # port methods per aggregate
    └── user_message.go      # wire payloads
```

## The three-file SQL repository

A SQL repository is deliberately split across three concerns, so no file does two jobs:

```bash
┌──────────────────────────────────────────────────────────────────────┐
│ infrastructure/database/postgres.go                                  │
│   NewPostgresPool() → (*pgxpool.Pool, cleanup, error)                │
└────────────────────────────┬─────────────────────────────────────────┘
                             │ injected into
                ┌────────────┴────────────┐
                ▼                         ▼
┌──────────────────────────┐ ┌──────────────────────────────┐
│ tx_manager.go            │ │ qx.go                        │
│   TxManager.WithinTx()   │ │   qx.Q(ctx) → *dbgen.Queries │
│   • begins/commits tx    │ │   • auto-detects tx in ctx   │
│   • stores tx in ctx     │ │   • falls back to the pool   │
└──────────────────────────┘ └──────────────────────────────┘
                                          │ held as a field
                                          ▼
                              ┌──────────────────────────┐
                              │ user_repository.go       │
                              │   r.qx.Q(ctx).Method(…)  │
                              │   maps via mapper/       │
                              └──────────────────────────┘
```

The pair exists to solve one problem: **a repository method must join an ambient transaction
without knowing whether one is running.** The usecase says `txManager.WithinTx(ctx, fn)`; every
repository call inside `fn` silently uses that transaction.

### tx_manager.go — implements domain.TxManager

```go
var _ domain.TxManager = (*TxManager)(nil)

type txKey struct{}
type TxManager struct{ pool *pgxpool.Pool }

// WithinTx runs fn inside a db transaction. Nested calls reuse the ambient tx.
func (t *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
    if _, ok := txFromCtx(ctx); ok {
        return fn(ctx)
    }

    tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return errors.Wrap(err, "begin tx")
    }
    defer func() { _ = tx.Rollback(ctx) }()

    if err = fn(withTx(ctx, tx)); err != nil {
        return errors.Wrap(err, "tx body")
    }
    if commitErr := tx.Commit(ctx); commitErr != nil {
        return errors.Wrap(commitErr, "commit tx")
    }
    return nil
}
```

Three things to preserve if you touch this:

- **`defer Rollback` is unconditional.** After a successful `Commit`, the rollback is a no-op
  error that we deliberately discard. This is what guarantees no path leaks a connection — not
  even a panic in `fn`.
- **Nesting reuses, never nests.** `WithinTx` inside `WithinTx` runs `fn` on the existing
  transaction rather than opening a second one, so composing two usecase methods can't deadlock
  against itself. If you need genuine nesting, that's savepoints — a different method.
- **`txKey struct{}`** is an unexported zero-size type, so no other package can collide with the
  context key or read the transaction out.

The interface lives in `domain/`, the implementation here: the usecase injects
`domain.TxManager` and never learns which database is wired in
([02](02-project-layout.md#notes-on-placement-that-differ-from-older-versions-of-this-spec)).

### qx.go — the TX-aware query executor

```go
type qx struct {
    pool *pgxpool.Pool
    base *dbgen.Queries // bound to pool (non-tx)
}

// Q returns a *dbgen.Queries bound to the current tx if present; otherwise to the pool.
func (q *qx) Q(ctx context.Context) *dbgen.Queries {
    if tx, ok := txFromCtx(ctx); ok {
        return dbgen.New(tx)
    }
    return q.base
}
```

`base` is built once in `newQX`, so the common (non-transactional) path allocates nothing. The
type and constructor are **unexported** — `qx` is an implementation detail of this package, not
something a usecase should see.

### user_repository.go

```go
var _ domain.UserRepository = (*UserRepository)(nil)

// UserRepository implements domain.UserRepository using sqlc-generated queries.
// It never calls row.Scan directly — all hydration goes through the mapper package.
type UserRepository struct {
    qx *qx
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
    return &UserRepository{qx: newQX(pool)}
}
```

Note the constructor takes the **pool**, not a `*qx`: the executor is an internal detail, so DI
never has to know it exists. `qx` is held as a field rather than embedded — embedding would
promote `Q` onto the repository's public method set.

Every method is three lines: call sqlc, wrap the error with context, map the row.

```go
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*entity.User, error) {
    row, err := r.qx.Q(ctx).GetUserByID(ctx, id)
    if err != nil {
        if stderrors.Is(err, pgx.ErrNoRows) {
            return nil, errors.Wrapf(errors.ErrNotFound, "user repo: select id=%d", id)
        }
        return nil, errors.Wrapf(err, "user repo: select id=%d", id)
    }
    return mapper.UserToEntity(row), nil
}
```

**Translating `pgx.ErrNoRows` → `errors.ErrNotFound` is the adapter's job, and it is the whole
reason this is an adapter and not infrastructure.** A driver sentinel is a storage detail; the
usecase checks `errors.Is(err, errors.ErrNotFound)` and stays portable. Swap postgres for mysql
and `sql.ErrNoRows` is translated to the same sentinel by the mysql variant.

`stderrors` is the aliased standard library `errors` — the project's own `pkg/errors` owns the
unqualified name. Both are needed here: `stderrors.Is` against the driver sentinel, `errors.Wrapf`
for the app error.

### mapper/ — pure row ↔ entity functions

```go
// Package mapper converts between domain entities and sqlc-generated DB rows.
// One file per entity. All functions are pure — no DB handles, no context.
```

Four functions per entity: `UserToEntity`, `UsersToEntities`, `UserToCreateParams`,
`UserToUpdateParams`. Splitting them out keeps the repository file readable at a glance and makes
the mapping unit-testable with no database.

This is also where the storage-vs-domain naming gap is absorbed:

```go
func UserToEntity(row dbgen.User) *entity.User {
    return &entity.User{
        …
        PasswordHash: row.Password,          // column is `password`, field is PasswordHash
        CreatedAt:    row.CreatedAt.Time,    // pgtype.Timestamptz → time.Time
        …
    }
}

func UserToCreateParams(u *entity.User) dbgen.CreateUserParams {
    return dbgen.CreateUserParams{
        …
        CreatedAt: pgtype.Timestamptz{Time: u.CreatedAt, Valid: true},
        …
    }
}
```

`pgtype.Timestamptz` never escapes this package — that's the point. `UserToUpdateParams` carries
only `ID` and `Name`, matching what `UpdateUser` actually sets.

### The mysql variant

Same shape, same file names, different types: `*sql.DB` instead of `*pgxpool.Pool`, `*sql.Tx`
instead of `pgx.Tx`, `sql.ErrNoRows` instead of `pgx.ErrNoRows`, and `CreateUser` is
`:execlastid` (returning the insert ID) rather than `:one … RETURNING *`. The generator picks the
directory from `--database`; both trees are always in the template tree.

## sqlc — configuration, migrations, queries

```bash
sqlc/
├── sqlc.yaml            # rendered from pg_sqlc.yaml.tmpl or mysql_sqlc.yaml.tmpl
├── query/
│   └── user.sql         # annotated SQL — sqlc generates Go from these
└── migrations/
    ├── {ts}_create_users_table.up.sql
    └── {ts}_create_users_table.down.sql
```

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "query"
    schema:
      - "migrations"
    gen:
      go:
        package: "dbgen"
        out: "../internal/adapter/repository/postgres/dbgen"
        sql_package: "pgx/v5"
        overrides:
          - db_type: jsonb
            go_type:
              import: "encoding/json"
              type: "RawMessage"
            nullable: true
```

Two things this config does deliberately:

- **`schema: ["migrations"]`** — sqlc reads the *migration* files as its schema source, so there
  is exactly one definition of the tables. A separate `schema.sql` is a second source of truth
  that drifts.
- **The `jsonb` → `json.RawMessage` override** keeps the audit log's payload column as raw bytes
  instead of a driver-specific JSON type, so the mapper doesn't have to re-encode it.

The mysql config differs in engine, output path, `sql_package: "database/sql"`, the `json`
db_type, and adds `emit_prepared_queries: true`.

### Migration

```sql
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users (email);
```

golang-migrate format (`.up.sql`/`.down.sql`, timestamp-prefixed). `TIMESTAMP WITH TIME ZONE`
rather than plain `TIMESTAMP` — storing wall-clock time without a zone is the classic source of
one-hour bugs. The `UNIQUE` on `email` is the real uniqueness guarantee; the usecase's
`GetByEmail` check only exists to produce a friendlier message
([06](06-usecase-layer.md#register--check-hash-create-publish)).

> **Known bug:** `nova new`'s `create_users_table` migration emits postgres DDL (`BIGSERIAL`,
> `TIMESTAMP WITH TIME ZONE`) even for a `--database=mysql` project. `nova add repository` does
> emit correct per-engine DDL — see [internal/generator/repository_sqlc.go](../internal/generator/repository_sqlc.go).

### Queries

```sql
-- name: ListUsers :many
SELECT *
FROM users
WHERE (sqlc.arg('email')::text = '' OR email = sqlc.arg('email')::text)
ORDER BY id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;

-- name: CountUsers :one
SELECT COUNT(*)
FROM users
WHERE (sqlc.arg('email')::text = '' OR email = sqlc.arg('email')::text);

-- name: ListUsersAfter :many
SELECT *
FROM users
WHERE id > sqlc.arg('cursor')
ORDER BY id
LIMIT sqlc.arg('limit')::int;
```

The `sqlc.arg('email') = '' OR email = …` idiom is how an optional filter stays a **single
prepared statement** — no string concatenation, no SQL injection surface, one query plan. Named
args (rather than `$1`) mean `ListUsersParams` has readable field names, and `CountUsers` can
share the `email` parameter with `ListUsers` while ignoring limit/offset, exactly as the port
contract promises ([05](05-domain-layer.md)).

`ListUsersAfter` is the keyset read behind `PublicList`: `id > cursor ORDER BY id LIMIT n`. No
`OFFSET`, so cost doesn't grow with page depth.

Every read is `SELECT *`, which is safe *because* sqlc regenerates the row struct from the
migration — add a column and `dbgen.User` grows a field, and the mapper (not the repository) is
the only thing that needs touching.

## adapter/pubsub — the event publisher

Three files, split so that swapping brokers touches exactly one:

| File                | Broker-specific? | Contents                                   |
| ------------------- | ---------------- | ------------------------------------------ |
| `user_message.go`   | No               | Wire DTOs with `json` tags                 |
| `user_publisher.go` | No               | One port method per event, maps → DTO      |
| `publisher.go`      | **Yes**          | `publish()` transport + `Close()`          |

```go
// Wire-format message DTOs — these own json tags.
// Domain events have NO json tags. The publisher maps domain → message DTO before serializing.
type UserCreatedMessage struct {
    UserID    int64     `json:"userId"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"createdAt"`
}
```

```go
func (p *Publisher) PublishUserCreated(ctx context.Context, evt domain.UserCreated) error {
    msg := UserCreatedMessage{
        UserID: evt.UserID, Email: evt.Email, Name: evt.Name, CreatedAt: evt.CreatedAt,
    }
    return p.publish(ctx, "user.created", msg)
}
```

```bash
┌─────────────────────┐      ┌────────────────────────┐      ┌──────────────────────┐
│  Use Case           │      │  Adapter (publisher)   │      │  Kafka               │
│  domain.UserCreated │ ───▶ │  UserCreatedMessage    │ ───▶ │ {"userId": 1, …}     │
│  (no json tags)     │      │  (has json tags)       │      │  (wire format)       │
└─────────────────────┘      └────────────────────────┘      └──────────────────────┘
```

The domain event never carries serialization concerns, so renaming a JSON field is a wire-format
change that cannot reach the domain. The topic string lives here and is duplicated (with a
pointer comment) in the worker's handler — see
[07](07-transport-layer.md#the-ack-decision-is-the-interesting-part) for why that duplication is
intentional.

### The broker variant

`publisher.go` is rendered per `--queue`, and the exported surface is identical either way, so DI
never changes:

```go
// wire.go
pubsub.NewPublisher,
wire.Bind(new(domain.UserPublisher), new(*pubsub.Publisher)),
```

- **kafka** — wraps `sarama.SyncProducer`. Checks `ctx.Err()` before marshalling, so a
  cancelled request doesn't publish.
- **rabbitmq** — opens a channel and declares a durable `events` topic exchange in the
  constructor (returning an error, so a failed declare fails startup rather than the first
  publish). The topic name becomes the routing key.
- **nats** — a compiling stub returning `errors.L(locale.Unimplemented)`.

Switching brokers in an existing project means re-rendering `publisher.go` and the
`infrastructure/pubsub` factory. Usecases, handlers, and the other two files are untouched.

## adapter/repository/redis — cache-aside decorator

```go
// Package redis is the cache-aside decorator over UserRepository.
//
// Cache hits return an entity.User WITHOUT the Password/PasswordHash fields —
// credentials are never serialized into Redis. Callers MUST NOT re-persist
// the returned entity into the underlying store; pull a fresh row via
// GetByEmail (which bypasses cache) when credentials are required.
```

`UserCacheRepository` implements `domain.UserRepository` by wrapping another
`domain.UserRepository`, so the usecase is unaware it exists. Per-method behaviour:

| Method                        | Behaviour                                                  |
| ----------------------------- | ---------------------------------------------------------- |
| `GetByID`                     | Read-through, 15-min TTL                                   |
| `GetByEmail`                  | **Bypasses cache** — it's the login path, needs the hash    |
| `Update`, `Delete`            | Write through, then `DEL` the key                          |
| `Create`                      | Passthrough (nothing to invalidate)                        |
| `List`, `Count`, `ListAfter`  | Passthrough                                                |

Three asymmetries that are all deliberate:

- **Read failures degrade, write failures don't.** A cache read error or a decode error logs a
  warning and falls through to the database; a refill failure logs and returns the row anyway.
  But a failed `DEL` after a write **returns an error**, because silently leaving a stale entry
  is worse than a failed request the caller can retry.
- **`GetByEmail` never caches.** It is the only read that needs `PasswordHash`, and the cached
  projection deliberately omits credentials.
- **List/Count/ListAfter don't cache.** Any write to the table invalidates every cached page, so
  the hit rate rarely pays for the staleness.

Enable it by binding `domain.UserRepository` to
`NewUserCacheRepository(<concrete repo>, cacheClient)` in `infrastructure/di`. It is **not** wired
by default — where caching is correct is a domain decision the generator can't make.

## adapter/repository/elasticsearch — derived read model

```go
// Package elasticsearch is the full-text search index over users, backed by
// Elasticsearch. It is a standalone read/write adapter: the SQL UserRepository
// stays the system of record while this index is derived state you keep in sync
// on writes.
```

Not a decorator — a separate adapter with `Index`, `Delete`, `Search`. The indexed projection
(`userDoc`) is id + name + email: only the fields a query matches on, never credentials. `Search`
returns matching **IDs**, which you hydrate through the SQL repository, so the index can never
serve stale field values.

Also not wired by default. Enabling it means calling `Index`/`Delete` on every user write —
which is precisely the consistency burden you're opting into, and why the generator won't decide
it for you.

## Ports implemented elsewhere

Two ports are satisfied outside this layer, because satisfying a port is necessary but not
sufficient for `adapter/`:

- `domain/security.PasswordHasher` → `infrastructure/security/bcrypt.go` (pure computation)
- `pkg/observability.Logger` → `infrastructure/logger/zerolog.go` (cross-cutting)

[03](03-architecture-rules.md#ports-implemented-outside-adapter) has the reasoning.
