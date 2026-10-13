# Phase 5: Worker Reliability and Messaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`) run alone from the main thread; see Global Constraints.

**Goal:** A `nova new --transport=worker` project, on Kafka or RabbitMQ, handles every message under one policy (per-attempt deadline, panic guard, bounded retries with backoff, then parking on a dead-letter topic or queue), settles each delivery identically on both brokers, exits non-zero when the broker side disappears, drains its in-flight message before closing the consumer, records each event exactly once, and publishes to the exchange the worker actually binds.

**Architecture:** `transport/worker` gains `policy.go`: a `Policy` (handler timeout, max attempts, retry backoff, defaults for the zero value), the sentinels `ErrPoison`/`ErrParked`/`ErrConsumerClosed`/`ErrHandlerPanic`, `Poison(err)` for handlers, and `Worker.run`/`Worker.attempt` implementing the retry loop. `Worker.dispatch` returns nil (ack), `ErrParked` (dead-letter then ack) or the ctx error (leave for redelivery); the Kafka consumer copies parked messages to `<topic>.dlq` through the shared sync producer and never ends the session on a handler error, the RabbitMQ consumer declares `<exchange>.dlx`/`<queue>.dlq`, sets `Qos`, watches `NotifyClose`, and maps the three outcomes to `Ack`, `Nack(requeue=false)` and `Nack(requeue=true)`. `RunWorker` treats a consume loop that returns while ctx is live as fatal and waits (bounded) for it before `Stop()`. Exchange, queue and prefetch flow from `RabbitMQConfig` into both publisher and consumer; `WorkerConfig` feeds the policy through `provideWorkerPolicy`; dead config fields are deleted. The RabbitMQ publisher publishes persistent messages in confirm mode and the connection and channel get cleanups. `UserCreatedMessage` carries a producer-minted `eventId`, stored in `user_audit_log.event_id` under a unique index; the repository reports a redelivery as `domain.ErrEventAlreadyRecorded`, which the usecase treats as done. The generated project ships broker-less unit tests for the policy and both consumers; the bootstrap runs them, and a sixth bootstrap (mysql, rabbitmq, fx) gives the RabbitMQ path a real toolchain run.

**Tech Stack:** `github.com/IBM/sarama` (+ `sarama/mocks` in tests), `github.com/rabbitmq/amqp091-go`, `github.com/google/uuid`, sqlc (`:execrows`), `ON CONFLICT DO NOTHING` / `ON DUPLICATE KEY UPDATE`.

**Status (2026-10-13):** all five tasks implemented and verified (`make test` green; `make lint` 0 issues; markdownlint 0 issues on every edited file; `make test-all` green at ~5 min with 36 vet rows and 6 bootstraps, the two worker bootstraps also running the generated worker tests); commit steps pending the user's `/grimoire-core:commit`. The roadmap's manual docker-compose run of the README demo was not performed.

**Deviations from the plan as written:**

- Task 1: the RabbitMQ publisher also sets the `x-event-id` header (the plan had it only as `MessageId`), so both brokers carry the id the same way; the contract test asserts the header for both.
- Task 1: fx.go imported the publisher adapter for every message queue; it is now kafka-only there, since the RabbitMQ publisher is reached through `providePublisher` in fx_provider.go.
- Task 3: the Kafka `ConsumeClaim` re-checks the session ctx in the loop condition (not an `if` after `settle`), and `settle` marks inside its two settled cases, because the generated project's `nilerr` linter rejects returning nil from an error branch.
- Several generated lines were wrapped to stay under golines' 120 columns (publisher, `RunWorker`, `provideRabbitMQConnection`, the policy retry log line, the config summary, one test).
- The Kafka test builds the request-id header in a variable: a `{{Key:` composite literal is parsed as a template action.

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 3 and 5.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`.
- `templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` stay byte-identical, likewise `cmd/worker.go.tmpl` and `skel/worker/cmd_worker.go.tmpl`; the new `policy.go.tmpl` is pinned identical the same way (`TestWorkerTemplatesIdentical`).
- Transport templates depend on `pkg/observability.Logger`, never on `infrastructure/logger`; the `skel/` copies keep `*logger.Logger` as today.
- Every consumer change is applied to both the Kafka and RabbitMQ variants in both trees (`templates/transport/worker` and `skel/worker`).
- Generated test files must pass the generated project's `golangci-lint run`: no named returns, `t.Context()` instead of `context.Background()`, comments end with a period, no `t.Parallel()`.
- Wire-format names are fixed: topic `user.created`, header `x-event-id`, dead-letter headers `x-dead-letter-reason` and `x-dead-letter-source`, suffixes `.dlq` (Kafka topic, RabbitMQ queue) and `.dlx` (RabbitMQ exchange); JSON field `eventId`.
- Markdown edits pass markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: Names and durability flow from config

**Files:**

- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (`KafkaConfig`, `RabbitMQConfig`, new `WorkerConfig`, `Config.Worker`, `Summary`)
- Modify: `internal/generator/templates/infrastructure/config/base.yaml.tmpl`, `internal/generator/templates/env.example.tmpl`
- Modify: `internal/generator/templates/infrastructure/pubsub/rabbitmq.go.tmpl` (`NewRabbitMQConnection` returns a cleanup)
- Modify: `internal/generator/templates/adapter/pubsub/publisher.go.tmpl`, `user_publisher.go.tmpl`, `user_message.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/di/fx.go.tmpl`, `fx_provider.go.tmpl` (`provideRabbitMQConnection`, `providePublisher`)
- Modify: `internal/generator/generator_test.go` (`TestGenerateMatrix` gains two HTTP+RabbitMQ rows; `TestWorkerContract` first assertions)
- Modify: `docs/08-adapter-layer.md`, `docs/09-infrastructure-layer.md`

**Interfaces:**

- Produces: `config.RabbitMQConfig{URL, Exchange, Queue string; Prefetch int}`; `config.KafkaConfig{Brokers []string; ProducerRetryMax int; ConsumerGroupID string}` (no `UserCreatedTopic`); `config.WorkerConfig{HandlerTimeout time.Duration; MaxAttempts int; RetryBackoff time.Duration}` on `Config.Worker` (worker projects only); `infraPubsub.NewRabbitMQConnection(cfg config.RabbitMQConfig, log *logger.Logger) (*amqp.Connection, func(), error)`; `pubsub.NewPublisher(conn *amqp.Connection, cfg config.RabbitMQConfig) (*Publisher, func(), error)` (RabbitMQ) and `pubsub.NewPublisher(producer sarama.SyncProducer) *Publisher` (Kafka, no `Close`); `publish(ctx, topic, key, id string, payload any) error`; `UserCreatedMessage.EventID string \`json:"eventId"\``.

- [x] **Step 1: Write the failing contract test**

Add to `internal/generator/generator_test.go`, after `TestObservabilityContract`:

```go
// TestWorkerContract pins the phase 5 messaging contract per broker: names
// flow from config into publisher and consumer, every message is handled
// under one policy, both consumers settle the three outcomes the same way,
// shutdown drains before closing, and the audit log is idempotent.
func TestWorkerContract(t *testing.T) {
	t.Parallel()
	t.Run("kafka", func(t *testing.T) {
		t.Parallel()
		assertWorkerContract(t, renderProject(t, workerMatrixConfig("postgres", "kafka", "wire")), "kafka")
	})
	t.Run("rabbitmq", func(t *testing.T) {
		t.Parallel()
		assertWorkerContract(t, renderProject(t, workerMatrixConfig("mysql", "rabbitmq", "fx")), "rabbitmq")
	})
}

func assertWorkerContract(t *testing.T, dir, broker string) {
	t.Helper()
	cfgSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go"))
	mustContainAll(t, cfgSrc, "type WorkerConfig struct", `"worker"`)
	mustNotContain(t, cfgSrc, "UserCreatedTopic", "UserCreatedQueue")
	base := readFile(t, filepath.Join(dir, "internal/infrastructure/config/base.yaml"))
	mustContainAll(t, base, "handler_timeout: 30s", "max_attempts: 3", "retry_backoff: 500ms")
	mustNotContain(t, base, "user_created_topic", "user_created_queue")
	pub := readFile(t, filepath.Join(dir, "internal/adapter/pubsub/publisher.go"))
	mustContainAll(t, pub, `"x-event-id"`)
	mustNotContain(t, pub, "func (p *Publisher) Close()", `"events"`)
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/pubsub/user_publisher.go")), "uuid.NewString()")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/pubsub/user_message.go")), `json:"eventId"`)
	if broker == "rabbitmq" {
		mustContainAll(t, cfgSrc, "Queue    string", "Prefetch int")
		mustContainAll(t, pub, "cfg.Exchange", "amqp.Persistent", "PublishWithDeferredConfirmWithContext(", "ch.Confirm(false)")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/pubsub/rabbitmq.go")),
			"func NewRabbitMQConnection(cfg config.RabbitMQConfig, log *logger.Logger) (*amqp.Connection, func(), error)")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx_provider.go")),
			"func provideRabbitMQConnection(", "func providePublisher(")
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestWorkerContract ./internal/generator/`
Expected: FAIL on `type WorkerConfig struct`.

- [x] **Step 3: Config**

In `config.go.tmpl`, inside the `Config` struct after the `RabbitMQ` field block add:

```go
{{- if .HasWorker}}
	Worker     WorkerConfig   `{{.ConfigFormat}}:"worker"`
{{- end}}
```

Replace the `KafkaConfig` and `RabbitMQConfig` declarations and append `WorkerConfig`:

```go
{{- if eq .MessageQueue "kafka"}}

// KafkaConfig holds Kafka configuration.
//
// ConsumerGroupID is only read in worker mode (Transport=worker); HTTP-only
// deployments can leave it empty. Topic names are wire contracts owned by the
// handlers and the publisher, not configuration.
type KafkaConfig struct {
	Brokers          []string `{{.ConfigFormat}}:"brokers"            env:"KAFKA_BROKERS" env-separator:","`
	ProducerRetryMax int      `{{.ConfigFormat}}:"producer_retry_max" env:"KAFKA_PRODUCER_RETRY_MAX"`
	ConsumerGroupID  string   `{{.ConfigFormat}}:"consumer_group_id"  env:"KAFKA_CONSUMER_GROUP_ID"`
}
{{- end}}
{{- if eq .MessageQueue "rabbitmq"}}

// RabbitMQConfig holds RabbitMQ configuration. Exchange is shared by the
// publisher and the worker, so a message published here is routable to the
// queue declared there; Queue and Prefetch are only read in worker mode.
type RabbitMQConfig struct {
	URL      string `{{.ConfigFormat}}:"url"      env:"RABBITMQ_URL"`
	Exchange string `{{.ConfigFormat}}:"exchange" env:"RABBITMQ_EXCHANGE"`
	Queue    string `{{.ConfigFormat}}:"queue"    env:"RABBITMQ_QUEUE"`
	Prefetch int    `{{.ConfigFormat}}:"prefetch" env:"RABBITMQ_PREFETCH"`
}
{{- end}}
{{- if .HasWorker}}

// WorkerConfig bounds how the consumer worker handles one message; see
// transport/worker.Policy for what each field means.
type WorkerConfig struct {
	HandlerTimeout time.Duration `{{.ConfigFormat}}:"handler_timeout" env:"WORKER_HANDLER_TIMEOUT"`
	MaxAttempts    int           `{{.ConfigFormat}}:"max_attempts"    env:"WORKER_MAX_ATTEMPTS"`
	RetryBackoff   time.Duration `{{.ConfigFormat}}:"retry_backoff"   env:"WORKER_RETRY_BACKOFF"`
}
{{- end}}
```

In `Summary`, the rabbitmq line becomes `fields = append(fields, "rabbitmq_exchange", c.RabbitMQ.Exchange, "rabbitmq_queue", c.RabbitMQ.Queue)` and after it:

```go
{{- if .HasWorker}}
	fields = append(fields, "worker_max_attempts", c.Worker.MaxAttempts, "worker_handler_timeout", c.Worker.HandlerTimeout)
{{- end}}
```

`base.yaml.tmpl`: delete `user_created_topic` from the kafka block; the rabbitmq block becomes:

```yaml
rabbitmq:
  url: "amqp://guest:guest@localhost:5672/" # override via RABBITMQ_URL in production
  # The publisher and the worker share this topic exchange: a message is
  # routed by its topic name to the worker's queue.
  exchange: "{{.ProjectName}}.events"
  # Worker mode only: one durable queue bound to every handled topic so
  # replicas share it; "<queue>.dlq" collects parked messages.
  queue: "{{.ProjectName}}.worker"
  prefetch: 10
```

and after it:

```yaml
{{- if .HasWorker}}

worker:
  # Each delivery gets max_attempts tries, retry_backoff apart (doubling),
  # each bounded by handler_timeout; after that it is parked on the
  # dead-letter topic/queue so the stream keeps moving.
  handler_timeout: 30s
  max_attempts: 3
  retry_backoff: 500ms
{{- end}}
```

`env.example.tmpl`: the Kafka comment becomes `# consumer_group_id defaults in base.yaml.`; the RabbitMQ comment becomes `# exchange, queue and prefetch default in base.yaml.`

- [x] **Step 4: Connection cleanup and the publisher**

`infrastructure/pubsub/rabbitmq.go.tmpl`, replace `NewRabbitMQConnection`:

```go
// NewRabbitMQConnection dials RabbitMQ and returns the cleanup that closes the
// connection (which closes every channel opened on it). Infrastructure —
// connection setup only, no business logic.
func NewRabbitMQConnection(cfg config.RabbitMQConfig, log *logger.Logger) (*amqp.Connection, func(), error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	cleanup := func() {
		log.Info("[rabbitmq] closing connection ...")
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, amqp.ErrClosed) {
			log.Error("[rabbitmq] failed to close connection", "err", closeErr)
		}
	}
	return conn, cleanup, nil
}
```

(add `"errors"` to the imports.)

`adapter/pubsub/publisher.go.tmpl`: imports gain `"time"` and `"{{.ModuleName}}/internal/infrastructure/config"` under the rabbitmq condition. The Kafka block: `publish` takes `(ctx context.Context, topic, key, id string, payload any)`, appends `sarama.RecordHeader{Key: []byte("x-event-id"), Value: []byte(id)}` to `recordHeaders`, and the `Close` method is deleted (the producer is owned by `infrastructure/pubsub.NewKafkaProducer`'s cleanup). The RabbitMQ block becomes:

```go
{{- if eq .MessageQueue "rabbitmq"}}

var errPublishNacked = errors.New("publish nacked by broker")

// Publisher implements domain.UserPublisher using RabbitMQ. It owns one
// channel in confirm mode on the shared connection; the exchange name comes
// from config so the worker's queue binds to the same one.
type Publisher struct {
	ch       *amqp.Channel
	exchange string
}

// NewPublisher opens the channel, declares the exchange and returns the
// cleanup that closes the channel; the connection has its own cleanup.
func NewPublisher(conn *amqp.Connection, cfg config.RabbitMQConfig) (*Publisher, func(), error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, errors.Wrap(err, "open rabbitmq channel")
	}
	if declErr := ch.ExchangeDeclare(cfg.Exchange, amqp.ExchangeTopic, true, false, false, false, nil); declErr != nil {
		_ = ch.Close()
		return nil, nil, errors.Wrapf(declErr, "declare exchange %s", cfg.Exchange)
	}
	// Confirm mode: a publish that returns nil has been accepted by the
	// broker, not just written to the socket.
	if confErr := ch.Confirm(false); confErr != nil {
		_ = ch.Close()
		return nil, nil, errors.Wrap(confErr, "enable publisher confirms")
	}
	cleanup := func() { _ = ch.Close() }
	return &Publisher{ch: ch, exchange: cfg.Exchange}, cleanup, nil
}

// Ensure interface compliance at compile time
var _ domain.UserPublisher = (*Publisher)(nil)

func (p *Publisher) publish(ctx context.Context, routingKey, _, id string, payload any) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Wrapf(ctxErr, "publish to %s: context done", routingKey)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrapf(err, "publish to %s: marshal event", routingKey)
	}
	headers := amqp.Table{}
	for k, v := range correlationHeaders(ctx) {
		headers[k] = v
	}
	conf, pubErr := p.ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // survives a broker restart, like the durable queue it lands in
		MessageId:    id,
		Timestamp:    time.Now(),
		Headers:      headers,
		Body:         data,
	})
	if pubErr != nil {
		return errors.Wrapf(pubErr, "publish to %s", routingKey)
	}
	acked, waitErr := conf.WaitContext(ctx)
	if waitErr != nil {
		return errors.Wrapf(waitErr, "publish to %s: await confirm", routingKey)
	}
	if !acked {
		return errors.Wrapf(errPublishNacked, "publish to %s", routingKey)
	}
	return nil
}

{{- end}}
```

The NATS stub: `publish(ctx context.Context, subject, _, _ string, payload any)` and delete its `Close`.

`user_message.go.tmpl`: add `EventID string \`json:"eventId"\`` as the first field. `user_publisher.go.tmpl`:

```go
import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"{{.ModuleName}}/internal/domain"
)

func (p *Publisher) PublishUserCreated(ctx context.Context, evt domain.UserCreated) error {
	msg := UserCreatedMessage{
		// EventID is the consumer's idempotency key: a redelivery carries the
		// same id and is recorded once.
		EventID:   uuid.NewString(),
		UserID:    evt.UserID,
		Email:     evt.Email,
		Name:      evt.Name,
		CreatedAt: evt.CreatedAt,
	}
	return p.publish(ctx, "user.created", strconv.FormatInt(evt.UserID, 10), msg.EventID, msg)
}
```

- [x] **Step 5: DI**

Wire needs no change: `wire.FieldsOf(new(*config.Config), "RabbitMQ")` already provides `config.RabbitMQConfig`, and wire composes `(T, func(), error)` cleanups. fx: in `fx.go.tmpl` replace `infraPubsub.NewRabbitMQConnection,` with `provideRabbitMQConnection,` and delete the `infraPubsub` import block (lines 29–31); in `adapterModule` the publisher line becomes:

```go
{{- if eq .MessageQueue "kafka"}}
		fx.Annotate(pubsub.NewPublisher, fx.As(new(domain.UserPublisher))),
{{- end}}
{{- if eq .MessageQueue "rabbitmq"}}
		fx.Annotate(providePublisher, fx.As(new(domain.UserPublisher))),
{{- end}}
```

In `fx_provider.go.tmpl`: the `infraPubsub` import condition becomes `{{- if .HasMessageQueue}}`, the `amqp` import condition `{{- if eq .MessageQueue "rabbitmq"}}`, and add `"{{.ModuleName}}/internal/adapter/pubsub"` under the same rabbitmq condition. Replace the `provideRabbitMQChannel` block with:

```go
{{- if eq .MessageQueue "rabbitmq"}}

func provideRabbitMQConnection(lc fx.Lifecycle, cfg config.RabbitMQConfig, log *logger.Logger) (*amqp.Connection, error) {
	conn, cleanup, err := infraPubsub.NewRabbitMQConnection(cfg, log)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cleanup(); return nil }})
	return conn, nil
}

func providePublisher(lc fx.Lifecycle, conn *amqp.Connection, cfg config.RabbitMQConfig) (*pubsub.Publisher, error) {
	p, cleanup, err := pubsub.NewPublisher(conn, cfg)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cleanup(); return nil }})
	return p, nil
}
{{- if .HasWorker}}

func provideRabbitMQChannel(lc fx.Lifecycle, conn *amqp.Connection, log *logger.Logger) (*amqp.Channel, error) {
	ch, cleanup, err := infraPubsub.NewRabbitMQChannel(conn, log)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cleanup(); return nil }})
	return ch, nil
}
{{- end}}
{{- end}}
```

- [x] **Step 6: Matrix rows for HTTP + RabbitMQ**

No vet row renders an HTTP project with a message queue, so the fx wrappers above would be unproven. In `TestGenerateMatrix`, inside the `for _, di := range dis` loop after the worker rows, add:

```go
		// HTTP projects that also publish: the only rows that compile the
		// publisher next to an HTTP transport (fx wraps the RabbitMQ
		// connection and publisher cleanups in lifecycle hooks).
		t.Run("http_"+di+"_fiber_postgres_rabbitmq", func(t *testing.T) {
			t.Parallel()
			cfg := httpMatrixConfig("fiber", "postgres", di)
			cfg.MessageQueue = "rabbitmq"
			renderAndVet(t, cfg, wireGenHTTPStub)
		})
```

- [x] **Step 7: Docs**

`docs/08-adapter-layer.md`: in the three-file table, `publisher.go` holds "`publish()` transport + constructor cleanup"; the rabbitmq bullet becomes: opens a channel in confirm mode on the shared connection, declares the topic exchange named by `rabbitmq.exchange` (the same one the worker binds its queue to), publishes persistent messages carrying `MessageId` and waits for the broker's confirm, and returns the cleanup that closes the channel. Add after the `PublishUserCreated` snippet that the publisher mints `eventId` with `uuid.NewString()` as the consumer's idempotency key and carries it as the `x-event-id` header. Update the `PublishUserCreated` snippet to show the `EventID` field and the four-argument `publish` call.

`docs/09-infrastructure-layer.md`: in the config section list `Worker` among conditional sections; in `pubsub/` add that `NewRabbitMQConnection` returns the cleanup closing the connection, and that `RabbitMQConfig` carries `exchange` (shared), `queue` and `prefetch` (worker only), while Kafka topic names are not configuration.

- [x] **Step 8: Verify**

Run: `go test -run 'TestWorkerContract|TestWorkerBrokerConfigProvider|TestObservabilityContract|TestRenderedGoIsGofmtClean' ./internal/generator/`
Expected: `TestWorkerContract` still fails on later assertions only if tasks 2+ are pending; the rest PASS. Then `make lint` → 0 issues; `npx --no-install markdownlint-cli2 docs/08-adapter-layer.md docs/09-infrastructure-layer.md` → 0 issues.

- [ ] **Step 9: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): route messaging names and durability through config`).

---

### Task 2: One policy for every message

**Files:**

- Create: `internal/generator/templates/transport/worker/policy.go.tmpl`, `internal/generator/skel/worker/policy.go.tmpl` (identical)
- Create: `internal/generator/templates/transport/worker/worker_test.go.tmpl`
- Modify: `internal/generator/templates/transport/worker/worker.go.tmpl`, `internal/generator/skel/worker/worker.go.tmpl`
- Modify: `internal/generator/templates/transport/worker/v1/user/handler.go.tmpl`, `internal/generator/skel/worker/feature_handler.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/di/wire_provider.go.tmpl` (`provideWorkerPolicy`), `wire.go.tmpl`, `fx.go.tmpl`
- Modify: `internal/generator/skel/worker/di_worker_provider.go.tmpl`, `di_worker_wire.go.tmpl`
- Modify: `internal/generator/generator.go` (`workerFiles`: `policy.go`, `worker_test.go`), `internal/generator/component.go` (`policy.go` spec, `tmplData.WorkerImport`)
- Modify: `internal/generator/component_test.go` (`TestWorkerAppTemplatesIdentical` → `TestWorkerTemplatesIdentical` table), `internal/generator/generator_test.go`
- Modify: `docs/07-transport-layer.md`, `docs/13-cross-cutting.md`

**Interfaces:**

- Produces: `worker.Policy{HandlerTimeout time.Duration; MaxAttempts int; RetryBackoff time.Duration}`; `worker.NewWorker(c Consumer, log observability.Logger, handlers []Handler, policy Policy) *Worker`; `worker.ErrPoison`, `worker.ErrParked`, `worker.ErrConsumerClosed`, `worker.ErrHandlerPanic`; `worker.Poison(err error) error`; `dispatch` returns nil, an error wrapping `ErrParked`, or `ctx.Err()`; `di.provideWorkerPolicy(cfg *config.Config) worker.Policy`.
- Consumes: `config.WorkerConfig` from task 1.

- [x] **Step 1: Extend the contract test**

Add to `assertWorkerContract` (before the `if broker == "rabbitmq"` block):

```go
	policy := readFile(t, filepath.Join(dir, "internal/transport/worker/policy.go"))
	mustContainAll(t, policy, "func Poison(", "ErrPoison", "ErrParked", "ErrConsumerClosed", "ErrHandlerPanic",
		"recover()", "context.WithTimeout(", "withDefaults()")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker.go")),
		"policy Policy", `"attempts"`, "worker: message parked", "worker: message abandoned")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker_test.go")), "func TestDispatchOutcomes(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/v1/user/handler.go")),
		"worker.Poison(", "msg.EventID")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/provider.go")), "func provideWorkerPolicy(")
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestWorkerContract ./internal/generator/`
Expected: FAIL reading `policy.go`.

- [x] **Step 3: policy.go (both trees, identical)**

```go
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"{{.ModuleName}}/pkg/logctx"
)

// Policy bounds how one message is handled. The zero value takes the
// defaults below, so a worker built without configuration still has a
// deadline, a retry budget and a parking rule.
type Policy struct {
	// HandlerTimeout bounds each attempt; the handler's ctx expires at it.
	HandlerTimeout time.Duration
	// MaxAttempts is how many times a message is tried before it is parked.
	MaxAttempts int
	// RetryBackoff is the wait before the second attempt; it doubles after
	// every further failure.
	RetryBackoff time.Duration
}

const (
	defaultHandlerTimeout = 30 * time.Second
	defaultMaxAttempts    = 3
	defaultRetryBackoff   = 500 * time.Millisecond
	backoffFactor         = 2
)

func (p Policy) withDefaults() Policy {
	if p.HandlerTimeout <= 0 {
		p.HandlerTimeout = defaultHandlerTimeout
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaultMaxAttempts
	}
	if p.RetryBackoff <= 0 {
		p.RetryBackoff = defaultRetryBackoff
	}
	return p
}

var (
	// ErrPoison marks a message that can never succeed (malformed payload,
	// unknown schema). The worker parks it without retrying.
	ErrPoison = errors.New("poison message")
	// ErrParked is what dispatch returns once a message is given up on: it
	// was poison or exhausted MaxAttempts. Consumers copy it to the
	// dead-letter topic/queue and ack the original so the stream keeps moving.
	ErrParked = errors.New("message parked")
	// ErrConsumerClosed is returned by Subscribe when the broker side ended
	// the subscription while ctx was still live; RunWorker exits non-zero so
	// the orchestrator restarts the process.
	ErrConsumerClosed = errors.New("consumer closed")
	// ErrHandlerPanic wraps a recovered handler panic so one bad message
	// cannot take the process down.
	ErrHandlerPanic = errors.New("handler panic")
)

// Poison wraps err so the worker parks the message without retrying.
func Poison(err error) error {
	return fmt.Errorf("%w: %w", ErrPoison, err)
}

// run applies the policy to one message: each attempt gets its own deadline
// and panic guard, transient failures retry with doubling backoff, and poison
// or an exhausted budget comes back wrapped in ErrParked. A cancelled ctx
// returns ctx.Err() so the consumer leaves the message for redelivery.
func (w *Worker) run(ctx context.Context, h Handler, payload []byte) (int, error) {
	backoff := w.policy.RetryBackoff
	for attempt := 1; ; attempt++ {
		err := w.attempt(ctx, h, payload)
		switch {
		case err == nil:
			return attempt, nil
		case errors.Is(err, ErrPoison):
			return attempt, fmt.Errorf("%w: %w", ErrParked, err)
		case ctx.Err() != nil:
			return attempt, ctx.Err()
		case attempt >= w.policy.MaxAttempts:
			return attempt, fmt.Errorf("%w after %d attempts: %w", ErrParked, attempt, err)
		}
		logctx.From(ctx).Warn("worker: attempt failed", "attempt", attempt, "retry_in_ms", backoff.Milliseconds(), "err", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return attempt, ctx.Err()
		}
		backoff *= backoffFactor
	}
}

// attempt runs one handler call under the per-attempt deadline and turns a
// panic into ErrHandlerPanic.
func (w *Worker) attempt(ctx context.Context, h Handler, payload []byte) error {
	attemptCtx, cancel := context.WithTimeout(ctx, w.policy.HandlerTimeout)
	defer cancel()
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
			}
		}()
		err = h.Handle(attemptCtx, payload)
	}()
	return err
}
```

- [x] **Step 4: worker.go (both trees)**

Add `policy Policy` to the `Worker` struct; `NewWorker` gains the fourth parameter and sets `policy: policy.withDefaults()`; the struct doc says "Policy bounds every dispatch (see policy.go)". Replace the tail of `dispatch` (from `start := time.Now()`) with:

```go
	start := time.Now()
	attempts, err := w.run(ctx, h, msg.Payload)
	fields := []any{"duration_ms", time.Since(start).Milliseconds(), "attempts", attempts}
	switch {
	case err == nil:
		scoped.Info("worker: message handled", fields...)
	case errors.Is(err, ErrParked):
		scoped.Error("worker: message parked", append(fields, errors.LogFields(err)...)...)
	default:
		// Shutdown mid-handling: the consumer leaves the message unacked so
		// the broker redelivers it to the next session.
		scoped.Warn("worker: message abandoned", append(fields, errors.LogFields(err)...)...)
	}
	return err
```

The `Consumer` doc comment's last two sentences become: "dispatch returns nil to ack, an error wrapping ErrParked to dead-letter and ack, or the ctx error to leave the message for redelivery."

- [x] **Step 5: worker_test.go.tmpl (template tree only)**

```go
package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"{{.ModuleName}}/pkg/observability"
)

var errTransient = errors.New("transient")

// scriptedHandler answers each attempt from results in order, repeating the
// last entry; an empty script succeeds. block waits for ctx on every attempt.
type scriptedHandler struct {
	results []error
	block   bool
	panics  bool
	calls   int
}

func (*scriptedHandler) Topic() string { return "t" }

func (h *scriptedHandler) Handle(ctx context.Context, _ []byte) error {
	h.calls++
	if h.panics {
		panic("boom")
	}
	if h.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if len(h.results) == 0 {
		return nil
	}
	return h.results[min(h.calls-1, len(h.results)-1)]
}

// cancelOnceHandler cancels the dispatch ctx from inside the first attempt
// and fails it, the shape of a shutdown arriving mid-message.
type cancelOnceHandler struct {
	cancel context.CancelFunc
	calls  int
}

func (*cancelOnceHandler) Topic() string { return "t" }

func (h *cancelOnceHandler) Handle(context.Context, []byte) error {
	h.calls++
	h.cancel()
	return errTransient
}

func fastPolicy() Policy {
	return Policy{HandlerTimeout: 50 * time.Millisecond, MaxAttempts: 3, RetryBackoff: time.Millisecond}
}

func TestDispatchOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		handler   *scriptedHandler
		wantErr   error // sentinel the result must wrap; nil for success
		wantCalls int
	}{
		{"success is handled first time", &scriptedHandler{}, nil, 1},
		{"transient failure retries then succeeds", &scriptedHandler{results: []error{errTransient, nil}}, nil, 2},
		{"exhausted attempts park", &scriptedHandler{results: []error{errTransient}}, ErrParked, 3},
		{"poison parks without retry", &scriptedHandler{results: []error{Poison(errTransient)}}, ErrParked, 1},
		{"timeout counts as a failed attempt", &scriptedHandler{block: true}, context.DeadlineExceeded, 3},
		{"panic is recovered and retried", &scriptedHandler{panics: true}, ErrHandlerPanic, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWorker(nil, observability.Nop(), []Handler{tc.handler}, fastPolicy())
			err := w.dispatch(t.Context(), Message{Topic: "t", Partition: -1})
			if tc.wantErr == nil && err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if tc.wantErr != nil && (!errors.Is(err, tc.wantErr) || !errors.Is(err, ErrParked)) {
				t.Fatalf("dispatch error = %v, want ErrParked wrapping %v", err, tc.wantErr)
			}
			if tc.handler.calls != tc.wantCalls {
				t.Errorf("handler called %d times, want %d", tc.handler.calls, tc.wantCalls)
			}
		})
	}
}

func TestDispatchAbandonsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h := &cancelOnceHandler{cancel: cancel}
	w := NewWorker(nil, observability.Nop(), []Handler{h}, Policy{MaxAttempts: 5, RetryBackoff: time.Hour})
	err := w.dispatch(ctx, Message{Topic: "t", Partition: -1})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrParked) {
		t.Fatalf("dispatch error = %v, want context.Canceled and not parked", err)
	}
	if h.calls != 1 {
		t.Errorf("handler called %d times, want 1 (no retry after shutdown)", h.calls)
	}
}

func TestDispatchDropsUnknownTopic(t *testing.T) {
	h := &scriptedHandler{}
	w := NewWorker(nil, observability.Nop(), []Handler{h}, Policy{})
	if err := w.dispatch(t.Context(), Message{Topic: "other", Partition: -1}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if h.calls != 0 {
		t.Errorf("handler called %d times for an unknown topic, want 0", h.calls)
	}
}

func TestPolicyZeroValueTakesDefaults(t *testing.T) {
	got := NewWorker(nil, observability.Nop(), nil, Policy{}).policy
	want := Policy{HandlerTimeout: defaultHandlerTimeout, MaxAttempts: defaultMaxAttempts, RetryBackoff: defaultRetryBackoff}
	if got != want {
		t.Errorf("policy = %+v, want %+v", got, want)
	}
}
```

- [x] **Step 6: Handlers use Poison**

`templates/transport/worker/v1/user/handler.go.tmpl`: imports gain `"{{.ModuleName}}/internal/transport/worker"` and drop `logctx`; the body of `Handle` starts:

```go
	var msg UserCreatedMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		// Malformed JSON can never succeed: park it on the dead-letter
		// topic/queue instead of retrying it or silently dropping it.
		return worker.Poison(errors.Wrap(err, "user.created: malformed payload"))
	}
	if msg.EventID == "" {
		return worker.Poison(errors.New("user.created: missing eventId"))
	}
```

and `UserCreatedInput` gets `EventID: msg.EventID` (the field itself is added in task 5; add the DTO field `EventID string \`json:"eventId"\`` to `v1/user/dto.go.tmpl` now and the usecase input field now too, so the tree compiles after this task). The doc comment "Poison message … Log and ack" is deleted.

`skel/worker/feature_handler.go.tmpl`: imports `"fmt"` and `worker "{{.WorkerImport}}"`; the unmarshal branch returns `worker.Poison(fmt.Errorf("{{.Lower}}: malformed payload: %w", err))` with the comment "Malformed JSON can never succeed: park it on the dead-letter topic/queue instead of retrying it forever." In `component.go` add `WorkerImport string // worker: import path of the shared transport/worker package` to `tmplData` and set `WorkerImport: g.manifest.Module + "/" + root` in `GenerateWorker` (compute `root` before `data`). Add `{"skel/worker/policy.go.tmpl", filepath.Join(root, "policy.go"), true}` after the `worker.go` spec.

- [x] **Step 7: DI**

`wire_provider.go.tmpl`, in the `{{- if .HasWorker}}` block after `provideWorkerHandlers`:

```go

// provideWorkerPolicy projects the worker config onto the transport's Policy
// so transport/worker stays free of the config package.
func provideWorkerPolicy(cfg *config.Config) worker.Policy {
	return worker.Policy{
		HandlerTimeout: cfg.Worker.HandlerTimeout,
		MaxAttempts:    cfg.Worker.MaxAttempts,
		RetryBackoff:   cfg.Worker.RetryBackoff,
	}
}
```

`wire.go.tmpl` `workerInfraSet` and `fx.go.tmpl` `workerModule` each add `provideWorkerPolicy,` after `provideWorkerHandlers,`.

`skel/worker/di_worker_provider.go.tmpl` appends (outside the rabbitmq condition):

```go

// provideWorkerPolicy bounds each message's handling. The zero value takes
// the transport's defaults; map a worker config section onto it when you
// add one.
func provideWorkerPolicy() worker.Policy {
	return worker.Policy{}
}
```

and `di_worker_wire.go.tmpl` lists `provideWorkerPolicy,` after `provideWorkerHandlers,`.

- [x] **Step 8: Generator and tests**

`generator.go` `workerFiles`: after the `worker.go` entry add `{"templates/transport/worker/policy.go.tmpl", "internal/transport/worker/policy.go", true}` and `{"templates/transport/worker/worker_test.go.tmpl", "internal/transport/worker/worker_test.go", true}`.

`component_test.go`: rename `TestWorkerAppTemplatesIdentical` to `TestWorkerTemplatesIdentical` and loop over `[]struct{skel, tmpl string}{{"skel/worker/app_worker.go.tmpl", "templates/app/worker.go.tmpl"}, {"skel/worker/cmd_worker.go.tmpl", "templates/cmd/worker.go.tmpl"}, {"skel/worker/policy.go.tmpl", "templates/transport/worker/policy.go.tmpl"}}`, one `t.Run` per pair. `TestGenerateWorkerFullTransport` asserts `policy.go` contains `func Poison(` and the feature handler contains `worker.Poison(`; `TestGenerateWorkerScaffoldsWireDI` asserts `provideWorkerPolicy` in provider.go. Update the roadmap/CLAUDE.md sentence naming `TestWorkerAppTemplatesIdentical`.

- [x] **Step 9: Docs**

`docs/07-transport-layer.md` worker section: replace "The ack decision is the interesting part" with a section "One policy, three outcomes" showing `Policy`, the retry loop summary, the handler rule (return `worker.Poison(err)` for input that can never succeed, a plain error for anything transient, never swallow), and the three dispatch results. `docs/13-cross-cutting.md` line about the worker boundary: `worker: message handled|parked|abandoned` plus `attempts`, and the per-attempt `worker: attempt failed` warn line.

- [x] **Step 10: Verify**

Run: `go test -run 'TestWorkerContract|TestWorkerTemplatesIdentical|TestGenerateWorker|TestRenderedGoIsGofmtClean' ./internal/generator/` then `make lint` and markdownlint on docs 07 and 13.
Expected: PASS except `TestWorkerContract` assertions that belong to tasks 3–5; 0 lint issues.

- [ ] **Step 11: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): bound every worker message by one retry-and-park policy`).

---

### Task 3: Consumers settle identically and never go zombie

**Files:**

- Rewrite: `internal/generator/templates/transport/worker/kafka_consumer.go.tmpl`, `rabbitmq_consumer.go.tmpl`; `internal/generator/skel/worker/kafka_consumer.go.tmpl`, `rabbitmq_consumer.go.tmpl`
- Create: `internal/generator/templates/transport/worker/kafka_consumer_test.go.tmpl`, `rabbitmq_consumer_test.go.tmpl` (rendered as `consumer_test.go`)
- Modify: `internal/generator/skel/worker/di_worker_provider.go.tmpl` (`provideRabbitMQConsumer` reads config)
- Modify: `internal/generator/generator.go` (`workerFiles`), `internal/generator/generator_test.go` (`bootstrapProject` runs the worker tests; sixth bootstrap case)
- Modify: `CLAUDE.md`, `Makefile` (36 rows, 6 bootstraps), `docs/07-transport-layer.md`, `docs/12-build-and-deploy.md`

**Interfaces:**

- Produces: `worker.NewKafkaConsumer(group sarama.ConsumerGroup, producer sarama.SyncProducer, log observability.Logger) Consumer`; `worker.NewRabbitMQConsumer(ch *amqp.Channel, cfg *config.RabbitMQConfig, log observability.Logger) Consumer` (template) and `worker.NewRabbitMQConsumer(ch *amqp.Channel, opts RabbitMQOptions, log *logger.Logger) Consumer` with `RabbitMQOptions{Exchange, Queue string; Prefetch int}` (skel); `Subscribe` returns `ctx.Err()` on shutdown and `ErrConsumerClosed` (possibly wrapping the broker's close error) when the subscription ends while ctx is live.
- Consumes: `ErrParked`, `ErrConsumerClosed` from task 2; `RabbitMQConfig.Queue/Prefetch` from task 1.

- [x] **Step 1: Extend the contract test**

Add to `assertWorkerContract`:

```go
	consumer := readFile(t, filepath.Join(dir, "internal/transport/worker/consumer.go"))
	mustContainAll(t, consumer, "ErrConsumerClosed", "errors.Is(err, ErrParked)")
	consumerTest := readFile(t, filepath.Join(dir, "internal/transport/worker/consumer_test.go"))
	if broker == "kafka" {
		mustContainAll(t, consumer, "producer sarama.SyncProducer", "deadLetterSuffix", `sess.MarkMessage(m, "")`,
			"HeaderDeadLetterReason", "HeaderDeadLetterSource")
		mustContainAll(t, consumerTest, "func TestKafkaSettle(", "mocks.NewSyncProducer(")
	} else {
		mustContainAll(t, consumer, `"x-dead-letter-exchange"`, ".Qos(", "NotifyClose(", "d.Nack(false, false)",
			"d.Nack(false, true)", "cfg.Queue", "cfg.Prefetch")
		mustContainAll(t, consumerTest, "func TestRabbitMQHandle(")
	}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestWorkerContract ./internal/generator/`
Expected: FAIL on `ErrConsumerClosed` in consumer.go.

- [x] **Step 3: Kafka consumer (template; the skel copy differs only in the logger type and the constructor doc)**

```go
package worker

import (
	"context"
	"fmt"
	"strconv"

	"github.com/IBM/sarama"

	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/observability"
)

// Dead-letter copies land on <topic>.dlq with the original headers plus the
// reason and source position, so a parked message can be inspected or replayed.
const (
	deadLetterSuffix       = ".dlq"
	HeaderDeadLetterReason = "x-dead-letter-reason"
	HeaderDeadLetterSource = "x-dead-letter-source" // "<topic>/<partition>/<offset>"
)

// kafkaConsumer adapts sarama.ConsumerGroup to the worker.Consumer port.
// One goroutine per partition is owned by sarama; our ConsumeClaim hook
// pulls messages off the partition channel and calls dispatch sequentially.
// Partition order is preserved within a single claim; cross-partition
// ordering is NOT — handlers must tolerate out-of-order delivery if your
// topic has more than one partition.
type kafkaConsumer struct {
	group    sarama.ConsumerGroup
	producer sarama.SyncProducer
	log      observability.Logger
}

// NewKafkaConsumer wraps the consumer group constructed in
// infrastructure/pubsub.NewKafkaConsumerGroup. producer (the same sync
// producer the publisher uses) writes parked messages to their dead-letter
// topic.
func NewKafkaConsumer(group sarama.ConsumerGroup, producer sarama.SyncProducer, log observability.Logger) Consumer {
	return &kafkaConsumer{group: group, producer: producer, log: log}
}

func (c *kafkaConsumer) Subscribe(ctx context.Context, topics []string, dispatch DispatchFunc) error {
	handler := &kafkaSessionHandler{dispatch: dispatch, deadLetter: c.deadLetter}

	// sarama reports partition/offset/broker errors here only because
	// Return.Errors is set (infrastructure/pubsub); undrained they are lost
	// and the channel eventually blocks. The goroutine ends when the group
	// is closed.
	go func() {
		for err := range c.group.Errors() {
			c.log.Error("kafka: consumer group error", "err", err)
		}
	}()

	// Consume returns after every rebalance; loop until ctx ends. A closed
	// group while ctx is live means the broker side is gone: report it so
	// RunWorker exits instead of idling as a zombie.
	for {
		err := c.group.Consume(ctx, topics, handler)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, sarama.ErrClosedConsumerGroup):
			return ErrConsumerClosed
		case err != nil:
			return fmt.Errorf("kafka: consume: %w", err)
		}
	}
}

func (c *kafkaConsumer) Close() error {
	return c.group.Close()
}

// deadLetter copies m to its dead-letter topic. The original key keeps the
// per-aggregate ordering of parked messages when they are replayed.
func (c *kafkaConsumer) deadLetter(m *sarama.ConsumerMessage, cause error) error {
	headers := make([]sarama.RecordHeader, 0, len(m.Headers)+2)
	for _, h := range m.Headers {
		if h != nil {
			headers = append(headers, *h)
		}
	}
	source := m.Topic + "/" + strconv.FormatInt(int64(m.Partition), 10) + "/" + strconv.FormatInt(m.Offset, 10)
	headers = append(headers,
		sarama.RecordHeader{Key: []byte(HeaderDeadLetterReason), Value: []byte(cause.Error())},
		sarama.RecordHeader{Key: []byte(HeaderDeadLetterSource), Value: []byte(source)},
	)
	var key sarama.Encoder
	if len(m.Key) > 0 {
		key = sarama.ByteEncoder(m.Key)
	}
	topic := m.Topic + deadLetterSuffix
	_, _, err := c.producer.SendMessage(&sarama.ProducerMessage{
		Topic:   topic,
		Key:     key,
		Value:   sarama.ByteEncoder(m.Value),
		Headers: headers,
	})
	if err != nil {
		return fmt.Errorf("kafka: dead-letter to %s: %w", topic, err)
	}
	c.log.Warn("kafka: message parked", "topic", m.Topic, "partition", m.Partition, "offset", m.Offset, "dlq", topic)
	return nil
}

// kafkaSessionHandler is sarama's per-session callback bundle. Setup runs
// once at session start, Cleanup once at session end, ConsumeClaim once per
// claimed partition. Returning from ConsumeClaim ends the session and
// triggers a rebalance, so it returns only on shutdown or when the
// dead-letter write itself fails — never because a handler failed.
type kafkaSessionHandler struct {
	dispatch   DispatchFunc
	deadLetter func(*sarama.ConsumerMessage, error) error
}

func (*kafkaSessionHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (*kafkaSessionHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaSessionHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case m, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			if err := h.settle(sess, m); err != nil {
				return err
			}
			// A message abandoned on shutdown stays unmarked; stop before a
			// later mark could commit past it.
			if sess.Context().Err() != nil {
				return nil
			}
		case <-sess.Context().Done():
			return nil
		}
	}
}

// settle dispatches one message and advances the offset only once it is
// done with: handled, or parked on the dead-letter topic. A message
// abandoned because the session ended stays unmarked so the next session
// redelivers it.
func (h *kafkaSessionHandler) settle(sess sarama.ConsumerGroupSession, m *sarama.ConsumerMessage) error {
	err := h.dispatch(sess.Context(), Message{
		Topic:     m.Topic,
		Payload:   m.Value,
		Key:       string(m.Key),
		Headers:   kafkaHeaders(m.Headers),
		Partition: m.Partition,
		Offset:    m.Offset,
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrParked):
		if dlqErr := h.deadLetter(m, err); dlqErr != nil {
			return dlqErr
		}
	default:
		return nil
	}
	sess.MarkMessage(m, "")
	return nil
}

func kafkaHeaders(hs []*sarama.RecordHeader) map[string]string {
	out := make(map[string]string, len(hs))
	for _, h := range hs {
		if h != nil {
			out[string(h.Key)] = string(h.Value)
		}
	}
	return out
}
```

(The skel copy imports `"errors"` from the standard library instead of `pkg/errors`, and `"{{.ModuleName}}/internal/infrastructure/logger"` with `log *logger.Logger`.)

- [x] **Step 4: RabbitMQ consumer (template)**

```go
package worker

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"{{.ModuleName}}/internal/infrastructure/config"
	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/observability"
)

const (
	deadLetterExchangeSuffix = ".dlx"
	deadLetterQueueSuffix    = ".dlq"
	defaultPrefetch          = 10
)

// rabbitmqConsumer adapts an AMQP channel to the worker.Consumer port.
//
// Wire shape: one durable topic exchange (cfg.Exchange, shared with the
// publisher) + one durable queue (cfg.Queue) bound to every handled topic,
// so replicas share it and the broker round-robins delivery. Parked messages
// are nacked without requeue and the queue's x-dead-letter-exchange routes
// them to <exchange>.dlx, whose only queue is <queue>.dlq.
type rabbitmqConsumer struct {
	ch       *amqp.Channel
	exchange string
	queue    string
	prefetch int
	log      observability.Logger
}

// NewRabbitMQConsumer wraps the channel opened by
// infrastructure/pubsub.NewRabbitMQChannel.
func NewRabbitMQConsumer(ch *amqp.Channel, cfg *config.RabbitMQConfig, log observability.Logger) Consumer {
	return &rabbitmqConsumer{ch: ch, exchange: cfg.Exchange, queue: cfg.Queue, prefetch: cfg.Prefetch, log: log}
}

func (c *rabbitmqConsumer) Subscribe(ctx context.Context, topics []string, dispatch DispatchFunc) error {
	if err := c.declare(topics); err != nil {
		return err
	}
	prefetch := c.prefetch
	if prefetch <= 0 {
		prefetch = defaultPrefetch
	}
	// Without Qos the broker pushes the whole backlog at once and a slow
	// handler holds every message unacked.
	if err := c.ch.Qos(prefetch, 0, false); err != nil {
		return fmt.Errorf("rabbitmq: qos: %w", err)
	}
	// A channel or connection error closes the channel and would otherwise
	// just close deliveries; surface it so RunWorker exits non-zero.
	closed := c.ch.NotifyClose(make(chan *amqp.Error, 1))
	deliveries, err := c.ch.ConsumeWithContext(ctx, c.queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq: consume %q: %w", c.queue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cerr := <-closed:
			if cerr == nil {
				return ErrConsumerClosed
			}
			return fmt.Errorf("%w: %w", ErrConsumerClosed, cerr)
		case d, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrConsumerClosed
			}
			c.handle(ctx, d, dispatch)
		}
	}
}

// declare sets up the topology so the worker boots cleanly even when the
// publisher has not run yet: the shared exchange, the dead-letter exchange
// and queue, and the work queue that dead-letters into them.
func (c *rabbitmqConsumer) declare(topics []string) error {
	dlx := c.exchange + deadLetterExchangeSuffix
	dlq := c.queue + deadLetterQueueSuffix
	if err := c.ch.ExchangeDeclare(c.exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare exchange %q: %w", c.exchange, err)
	}
	if err := c.ch.ExchangeDeclare(dlx, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare exchange %q: %w", dlx, err)
	}
	if _, err := c.ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare queue %q: %w", dlq, err)
	}
	if err := c.ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: bind %q→%q: %w", dlq, dlx, err)
	}
	// Queue arguments are fixed at creation: a queue declared before this
	// change must be deleted once so the dead-letter exchange takes effect.
	args := amqp.Table{"x-dead-letter-exchange": dlx}
	if _, err := c.ch.QueueDeclare(c.queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("rabbitmq: declare queue %q: %w", c.queue, err)
	}
	for _, t := range topics {
		if err := c.ch.QueueBind(c.queue, t, c.exchange, false, nil); err != nil {
			return fmt.Errorf("rabbitmq: bind %q→%q: %w", c.queue, t, err)
		}
	}
	return nil
}

// handle dispatches one delivery and settles it: ack when handled, nack
// without requeue (to the dead-letter exchange) when parked, nack with
// requeue when abandoned on shutdown so another replica gets it.
func (c *rabbitmqConsumer) handle(ctx context.Context, d amqp.Delivery, dispatch DispatchFunc) {
	msg := Message{
		Topic:       d.RoutingKey,
		Payload:     d.Body,
		Headers:     amqpHeaders(d.Headers),
		Partition:   -1,
		DeliveryTag: d.DeliveryTag,
		MessageID:   d.MessageId,
	}
	err := dispatch(ctx, msg)
	var settle error
	switch {
	case err == nil:
		settle = d.Ack(false)
	case errors.Is(err, ErrParked):
		settle = d.Nack(false, false)
	default:
		settle = d.Nack(false, true)
	}
	if settle != nil {
		c.log.Error("rabbitmq: settle failed", "delivery_tag", d.DeliveryTag, "err", settle)
	}
}

func amqpHeaders(t amqp.Table) map[string]string {
	out := make(map[string]string, len(t))
	for k, v := range t {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (c *rabbitmqConsumer) Close() error {
	// Channel lifecycle is owned by infrastructure/pubsub.NewRabbitMQChannel
	// cleanup — return nil so the worker layer doesn't double-close. The
	// connection-level cleanup runs after this in the DI cleanup chain.
	return nil
}
```

The skel copy takes `opts RabbitMQOptions` (`type RabbitMQOptions struct { Exchange, Queue string; Prefetch int }` declared above the consumer with a doc comment "RabbitMQOptions is the queue topology; pass your config's values.") and `*logger.Logger`, and uses the standard `errors` package. `skel/worker/di_worker_provider.go.tmpl`'s `provideRabbitMQConsumer` becomes:

```go
// provideRabbitMQConsumer constructs the worker consumer over the AMQP channel
// with the exchange, queue and prefetch from config, so the worker binds the
// same exchange the publisher writes to.
func provideRabbitMQConsumer(ch *amqp.Channel, cfg *config.RabbitMQConfig, log *logger.Logger) worker.Consumer {
	opts := worker.RabbitMQOptions{Exchange: cfg.Exchange, Queue: cfg.Queue, Prefetch: cfg.Prefetch}
	return worker.NewRabbitMQConsumer(ch, opts, log)
}
```

with `"{{.ModuleName}}/internal/infrastructure/config"` added to its imports.

- [x] **Step 5: Consumer tests (template tree)**

`kafka_consumer_test.go.tmpl`:

```go
package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"

	"{{.ModuleName}}/pkg/observability"
)

var errBroker = errors.New("broker down")

type fakeSession struct {
	ctx    context.Context
	marked []*sarama.ConsumerMessage
}

func (*fakeSession) Claims() map[string][]int32                    { return nil }
func (*fakeSession) MemberID() string                               { return "" }
func (*fakeSession) GenerationID() int32                            { return 0 }
func (*fakeSession) MarkOffset(string, int32, int64, string)        {}
func (*fakeSession) Commit()                                        {}
func (*fakeSession) ResetOffset(string, int32, int64, string)       {}
func (s *fakeSession) MarkMessage(m *sarama.ConsumerMessage, _ string) { s.marked = append(s.marked, m) }
func (s *fakeSession) Context() context.Context                       { return s.ctx }

func expectDeadLetter(t *testing.T, producer *mocks.SyncProducer, source string) {
	t.Helper()
	producer.ExpectSendMessageWithMessageCheckerFunctionAndSucceed(func(pm *sarama.ProducerMessage) error {
		if pm.Topic != "user.created"+deadLetterSuffix {
			return fmt.Errorf("dead-letter topic = %q", pm.Topic)
		}
		got := map[string]string{}
		for _, h := range pm.Headers {
			got[string(h.Key)] = string(h.Value)
		}
		if got[HeaderDeadLetterSource] != source || got[HeaderDeadLetterReason] == "" || got["x-request-id"] != "r1" {
			return fmt.Errorf("dead-letter headers = %v", got)
		}
		return nil
	})
}

// TestKafkaSettle pins the three outcomes a dispatch can have and how each is
// settled: handled → marked; parked → copied to the dead-letter topic, then
// marked; abandoned (ctx ended) → left unmarked for redelivery.
func TestKafkaSettle(t *testing.T) {
	cases := []struct {
		name        string
		dispatchErr error
		deadLetter  bool
		wantMarked  bool
	}{
		{"handled is marked", nil, false, true},
		{"parked is dead-lettered then marked", ErrParked, true, true},
		{"abandoned stays unmarked", context.Canceled, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			producer := mocks.NewSyncProducer(t, nil)
			if tc.deadLetter {
				expectDeadLetter(t, producer, "user.created/2/7")
			}
			c := &kafkaConsumer{producer: producer, log: observability.Nop()}
			h := &kafkaSessionHandler{
				dispatch:   func(context.Context, Message) error { return tc.dispatchErr },
				deadLetter: c.deadLetter,
			}
			sess := &fakeSession{ctx: t.Context()}
			m := &sarama.ConsumerMessage{
				Topic: "user.created", Partition: 2, Offset: 7, Key: []byte("1"), Value: []byte(`{}`),
				Headers: []*sarama.RecordHeader{{Key: []byte("x-request-id"), Value: []byte("r1")}},
			}
			if err := h.settle(sess, m); err != nil {
				t.Fatalf("settle: %v", err)
			}
			if marked := len(sess.marked) == 1; marked != tc.wantMarked {
				t.Errorf("marked = %v, want %v", marked, tc.wantMarked)
			}
			if err := producer.Close(); err != nil {
				t.Errorf("dead-letter expectations: %v", err)
			}
		})
	}
}

func TestKafkaSettleKeepsMessageWhenDeadLetterFails(t *testing.T) {
	producer := mocks.NewSyncProducer(t, nil)
	producer.ExpectSendMessageAndFail(errBroker)
	c := &kafkaConsumer{producer: producer, log: observability.Nop()}
	h := &kafkaSessionHandler{
		dispatch:   func(context.Context, Message) error { return ErrParked },
		deadLetter: c.deadLetter,
	}
	sess := &fakeSession{ctx: t.Context()}
	err := h.settle(sess, &sarama.ConsumerMessage{Topic: "user.created", Value: []byte(`{}`)})
	if !errors.Is(err, errBroker) {
		t.Fatalf("settle error = %v, want the producer error", err)
	}
	if len(sess.marked) != 0 {
		t.Error("message was marked although the dead-letter write failed")
	}
}
```

`rabbitmq_consumer_test.go.tmpl`:

```go
package worker

import (
	"context"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"{{.ModuleName}}/pkg/observability"
)

type fakeAcker struct {
	acks    int
	nacks   int
	requeue bool
}

func (a *fakeAcker) Ack(uint64, bool) error { a.acks++; return nil }

func (a *fakeAcker) Nack(_ uint64, _, requeue bool) error {
	a.nacks++
	a.requeue = requeue
	return nil
}

func (*fakeAcker) Reject(uint64, bool) error { return nil }

// TestRabbitMQHandle pins the three outcomes a dispatch can have and how each
// is settled: handled → ack; parked → nack without requeue (the queue's
// dead-letter exchange takes it); abandoned (ctx ended) → nack with requeue.
func TestRabbitMQHandle(t *testing.T) {
	cases := []struct {
		name        string
		dispatchErr error
		wantAcks    int
		wantNacks   int
		wantRequeue bool
	}{
		{"handled is acked", nil, 1, 0, false},
		{"parked is nacked to the dead-letter exchange", ErrParked, 0, 1, false},
		{"abandoned is requeued", context.Canceled, 0, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acker := &fakeAcker{}
			c := &rabbitmqConsumer{log: observability.Nop()}
			d := amqp.Delivery{Acknowledger: acker, RoutingKey: "user.created", DeliveryTag: 7, Body: []byte(`{}`)}
			c.handle(t.Context(), d, func(context.Context, Message) error { return tc.dispatchErr })
			if acker.acks != tc.wantAcks || acker.nacks != tc.wantNacks || acker.requeue != tc.wantRequeue {
				t.Errorf("acks=%d nacks=%d requeue=%v, want acks=%d nacks=%d requeue=%v",
					acker.acks, acker.nacks, acker.requeue, tc.wantAcks, tc.wantNacks, tc.wantRequeue)
			}
		})
	}
}
```

`generator.go` `workerFiles`: each broker case also appends `{"templates/transport/worker/<broker>_consumer_test.go.tmpl", "internal/transport/worker/consumer_test.go", true}`.

- [x] **Step 6: Bootstrap proves the tests**

In `bootstrapProject` after `run("go", "build", "./...")`:

```go
	if cfg.HasWorker() {
		// The worker ships broker-less unit tests (policy and consumer settle
		// semantics); run them so the rendered tests are proven, not just compiled.
		run("go", "test", "./internal/transport/worker/...")
	}
```

Add the sixth case `{"worker_mysql_rabbitmq_fx", workerMatrixConfig("mysql", "rabbitmq", "fx")}` with the comment "RabbitMQ, MySQL and fx had no real toolchain run; this also executes the RabbitMQ consumer tests." `CLAUDE.md` (principle 4: "six representative projects"; commands: "36-case vet matrix (2 at a time) + 6 real-toolchain bootstraps (~3 min warm cache; <2.5 GB)") and the `Makefile` help line get the same numbers.

- [x] **Step 7: Docs**

`docs/07-transport-layer.md`: a "Settling a delivery" subsection with the Kafka/RabbitMQ table (handled → `MarkMessage` / `Ack`; parked → copy to `<topic>.dlq` then `MarkMessage` / `Nack(requeue=false)` to `<exchange>.dlx` → `<queue>.dlq`; abandoned → unmarked / `Nack(requeue=true)`), the note that `ConsumeClaim` no longer returns on a handler error (so one poison message no longer rebalances the group), `Qos`/`NotifyClose`, and `ErrConsumerClosed`. `docs/12-build-and-deploy.md`: Kafka dead-letter topics `<topic>.dlq` are auto-created in compose; create them in production; a RabbitMQ queue declared before the dead-letter argument existed must be deleted once.

- [x] **Step 8: Verify**

Run: `go test -run 'TestWorkerContract|TestGenerateWorker' ./internal/generator/` then `make lint`, markdownlint on docs 07 and 12, then alone: `make test-all`.
Expected: all green; the kafka and rabbitmq bootstraps print the worker test package as `ok`.

- [ ] **Step 9: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): settle deliveries identically on kafka and rabbitmq, with dead-lettering`).

---

### Task 4: Shutdown drains, zombie exits

**Files:**

- Modify: `internal/generator/templates/app/worker.go.tmpl`, `internal/generator/skel/worker/app_worker.go.tmpl` (identical)
- Modify: `internal/generator/generator_test.go`, `docs/11-di-and-entrypoints.md`

**Interfaces:**

- Consumes: `worker.Start(ctx) error` returning `ctx.Err()` on shutdown or `ErrConsumerClosed`/another error otherwise; `worker.Stop() error`.
- Produces: `RunWorker()` exits non-zero when `Start` returns while ctx is live (including a nil return), and waits up to `drainTimeout` (30 s) for the consume loop before `Stop()`.

- [x] **Step 1: Extend the contract test**

```go
	appSrc := readFile(t, filepath.Join(dir, "internal/app/worker.go"))
	mustContainAll(t, appSrc, "drainTimeout", "errConsumerStopped", "consumer stopped before shutdown")
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestWorkerContract ./internal/generator/`
Expected: FAIL on `drainTimeout`.

- [x] **Step 3: RunWorker (both copies)**

```go
package app

import (
	"context"
	stderrors "errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"{{.ModuleName}}/internal/infrastructure/di"
	"{{.ModuleName}}/pkg/errors"
)

// drainTimeout bounds how long shutdown waits for the consume loop to finish
// the message it is on before the consumer is closed.
const drainTimeout = 30 * time.Second

// errConsumerStopped reports a consume loop that returned without an error
// before shutdown was requested: the subscription is gone and nothing
// restarts it in-process, so the process exits for the orchestrator to.
var errConsumerStopped = stderrors.New("worker: consumer stopped before shutdown")

// RunWorker boots the consumer worker, blocks until the OS signals shutdown
// or the consume loop ends, and returns any failure so the caller (cobra
// RunE) can set a non-zero exit code.
//
// Mirrors RunHTTPServer's lifecycle: signal.NotifyContext owns shutdown and
// DI cleanup runs in reverse construction order (broker first, then DB).
// The consume loop runs in a goroutine so we can select on ctx.Done, and
// shutdown waits for it to finish its in-flight message before closing the
// consumer, so the final ack or offset commit is not lost.
func RunWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, cleanup, err := di.InitializeWorker(ctx)
	if err != nil {
		return fmt.Errorf("initialize worker: %w", err)
	}

	log := app.Logger
	worker := app.Worker
	defer func() {
		start := time.Now()
		cleanup()
		log.Info("shutdown complete", "duration_ms", time.Since(start).Milliseconds())
	}()

	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()

	select {
	case <-ctx.Done():
		log.Info("worker: shutdown signal received")
		select {
		case <-done:
		case <-time.After(drainTimeout):
			log.Warn("worker: consume loop still running after drain timeout", "timeout_ms", drainTimeout.Milliseconds())
		}
	case startErr := <-done:
		if ctx.Err() == nil {
			// The loop ended while ctx was live: the broker side ended the
			// subscription and nothing resubscribes in-process. Exit non-zero
			// so the orchestrator restarts the process.
			if startErr == nil {
				startErr = errConsumerStopped
			}
			log.Error("worker: stopped with error", errors.LogFields(startErr)...)
			return startErr
		}
		log.Info("worker: shutdown signal received")
	}

	log.Info("worker: shutting down")
	if stopErr := worker.Stop(); stopErr != nil {
		log.Error("worker: stop failed", errors.LogFields(stopErr)...)
	}
	return nil
}
```

- [x] **Step 4: Docs**

`docs/11-di-and-entrypoints.md`: the `RunWorker` paragraph: it mirrors the HTTP lifecycle, but a consume loop that returns while ctx is live (nil or `worker.ErrConsumerClosed`) is fatal and exits non-zero, and shutdown waits up to `drainTimeout` for the loop before `Stop()` so the last ack or offset commit lands; the cleanup chain example gains the worker order (consumer group / channel → RabbitMQ connection or Kafka producer → DB → tracing).

- [x] **Step 5: Verify**

Run: `go test -run 'TestWorkerContract|TestWorkerTemplatesIdentical' ./internal/generator/`, `make lint`, markdownlint on docs 11.
Expected: PASS except the task-5 assertions; 0 issues.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): exit when the consumer dies and drain before closing it`).

---

### Task 5: Record each event once, and the docs sweep

**Files:**

- Modify: `internal/generator/templates/domain/user_audit.go.tmpl`, `usecase/useraudit/service.go.tmpl`, `usecase/useraudit/dto.go.tmpl`
- Modify: `internal/generator/templates/migrations/create_user_audit_log.up.sql.tmpl`, `sqlc/query/pg_user_audit.sql.tmpl`, `sqlc/query/mysql_user_audit.sql.tmpl`
- Modify: `internal/generator/templates/adapter/repository/{postgres,mysql}/user_audit_repository.go.tmpl`, `.../mapper/user_audit.go.tmpl`
- Modify: `internal/generator/generator_test.go` (dbgen stubs, contract assertions), `internal/generator/templates/README.md.tmpl`
- Modify: `docs/02-project-layout.md`, `docs/05-domain-layer.md`, `docs/06-usecase-layer.md`, `docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md`

**Interfaces:**

- Produces: `domain.UserAuditEntry.EventID string`; `domain.ErrEventAlreadyRecorded`; `useraudit.UserCreatedInput.EventID string`; sqlc `RecordUserAudit(ctx, RecordUserAuditParams) (int64, error)` on both engines (`:execrows`), `RecordUserAuditParams.EventID string`.
- Consumes: `UserCreatedMessage.EventID` from tasks 1–2.

- [x] **Step 1: Extend the contract test**

```go
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/user_audit.go")), "EventID", "ErrEventAlreadyRecorded")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/usecase/useraudit/service.go")),
		"domain.ErrEventAlreadyRecorded", "duplicate delivery ignored")
	migrations, _ := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*_create_user_audit_log.up.sql"))
	if len(migrations) != 1 {
		t.Fatalf("expected one audit migration, got %v", migrations)
	}
	mustContainAll(t, readFile(t, migrations[0]), "event_id VARCHAR(64) NOT NULL", "CREATE UNIQUE INDEX uq_user_audit_log_event_id")
	query := readFile(t, filepath.Join(dir, "sqlc/query/user_audit.sql"))
	mustContainAll(t, query, ":execrows", "event_id")
	if broker == "kafka" { // postgres
		mustContainAll(t, query, "ON CONFLICT (event_id) DO NOTHING")
	} else { // mysql
		mustContainAll(t, query, "ON DUPLICATE KEY UPDATE id = id")
	}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestWorkerContract ./internal/generator/`
Expected: FAIL on `ErrEventAlreadyRecorded`.

- [x] **Step 3: Domain, usecase, storage**

`domain/user_audit.go.tmpl`:

```go
package domain

import (
	"context"
	"errors"
	"time"
)

// ErrEventAlreadyRecorded reports a redelivered event whose id is already in
// the audit log; the usecase treats the delivery as done.
var ErrEventAlreadyRecorded = errors.New("event already recorded")

// UserAuditEntry is the audit log row written by the consumer worker every
// time a user.* event arrives. Payload is the raw event JSON — keep it
// opaque so future schema changes don't require backfills.
type UserAuditEntry struct {
	ID         int64
	EventID    string // producer-minted id, unique per event: the idempotency key
	UserID     int64
	EventType  string
	Payload    []byte // JSON; opaque to the domain
	OccurredAt time.Time
}

// UserAuditRepository persists audit rows. RecordCreated inserts the entry
// once per EventID and returns ErrEventAlreadyRecorded on a redelivery.
type UserAuditRepository interface {
	RecordCreated(ctx context.Context, entry *UserAuditEntry) error
}
```

`usecase/useraudit/dto.go.tmpl`: add `EventID string` (first field, doc "producer-minted; the row's idempotency key"). `service.go.tmpl`:

```go
// Service implements the worker usecase. It logs only the duplicate case —
// the worker's transport layer logs per message and the audit row IS the
// log for this code path.
type Service struct {
	repo domain.UserAuditRepository
}

func NewService(repo domain.UserAuditRepository) *Service {
	return &Service{repo: repo}
}

// RecordUserCreated writes the inbound user.created event to user_audit_log
// once: the broker delivers at least once, and a redelivery (same EventID)
// is a no-op.
func (s *Service) RecordUserCreated(ctx context.Context, input UserCreatedInput) error {
	entry := &domain.UserAuditEntry{
		EventID:    input.EventID,
		UserID:     input.UserID,
		EventType:  eventTypeUserCreated,
		Payload:    input.RawPayload,
		OccurredAt: input.OccurredAt,
	}
	err := s.repo.RecordCreated(ctx, entry)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrEventAlreadyRecorded):
		// Worth one line so a redelivery storm is visible, then done.
		logctx.From(ctx).Info("audit: duplicate delivery ignored", "event_id", input.EventID)
		return nil
	default:
		return errors.Wrapf(err, "audit: record user.created event_id=%s", input.EventID)
	}
}
```

(import `"{{.ModuleName}}/pkg/logctx"`.)

Migration (both engines): `event_id VARCHAR(64) NOT NULL,` after `id`, the header comment's last sentence replaced by "event_id is the producer's id for the event; the unique index turns a redelivery into a no-op insert.", and `CREATE UNIQUE INDEX uq_user_audit_log_event_id ON user_audit_log (event_id);` before the other indexes.

`pg_user_audit.sql.tmpl`:

```sql
-- name: RecordUserAudit :execrows
INSERT INTO user_audit_log (event_id, user_id, event_type, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (event_id) DO NOTHING;
```

`mysql_user_audit.sql.tmpl`:

```sql
-- name: RecordUserAudit :execrows
INSERT INTO user_audit_log (event_id, user_id, event_type, payload, occurred_at)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = id;
```

Repositories (both engines):

```go
func (r *UserAuditRepository) RecordCreated(ctx context.Context, entry *domain.UserAuditEntry) error {
	n, err := r.qx.Q(ctx).RecordUserAudit(ctx, mapper.UserAuditToCreateParams(entry))
	if err != nil {
		return errors.Wrapf(err, "user audit repo: insert event_id=%s", entry.EventID)
	}
	if n == 0 {
		return domain.ErrEventAlreadyRecorded
	}
	return nil
}
```

Mappers: `EventID: e.EventID` in `UserAuditToCreateParams` and `EventID: row.EventID` in `UserAuditToEntity`. Both dbgen stubs in `generator_test.go`: `EventID string` in `UserAuditLog` and `RecordUserAuditParams`; postgres `RecordUserAudit` returns `(int64, error)`.

- [x] **Step 4: Docs sweep**

- `README.md.tmpl` worker section: the handler table's side effect "Inserts a row into `user_audit_log` (once per `eventId`)"; after the table a short paragraph: every message gets `worker.max_attempts` tries with doubling `retry_backoff`, each under `handler_timeout`, then is parked on `<topic>.dlq` (Kafka) or `<queue>.dlq` (RabbitMQ) with the reason; return `worker.Poison(err)` from a handler for input that can never succeed. Demo step 6 adds "run it twice and the second insert is ignored".
- `docs/02-project-layout.md`: worker tree adds `policy.go`, `worker_test.go`, `consumer_test.go`.
- `docs/05-domain-layer.md`: where `UserAuditRepository` is mentioned, note `ErrEventAlreadyRecorded`.
- `docs/06-usecase-layer.md`: a `useraudit` paragraph: idempotent on `EventID`, the one log line it emits.
- Roadmap: Phase 4 header gains "(done 2026-10-13, commit `b02a3c0`)"; Phase 5 gains "Detailed plan: [2026-10-13-phase5-worker-reliability-messaging.md](2026-10-13-phase5-worker-reliability-messaging.md)."; replace `TestWorkerAppTemplatesIdentical` with `TestWorkerTemplatesIdentical` in Global Constraints.

- [x] **Step 5: Verify**

Run: `make test`, `make lint`, `npx --no-install markdownlint-cli2 docs/*.md docs/superpowers/plans/2026-10-13-phase5-worker-reliability-messaging.md internal/generator/templates/README.md.tmpl`, then alone `make test-all`.
Expected: all green; the sixth bootstrap runs real sqlc on the MySQL `ON DUPLICATE KEY UPDATE` query.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): record each event once and document the worker contract`).
