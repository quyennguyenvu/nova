# Cron transport (`nova new --transport cron`) design

Date: 2026-10-18. Status: approved in brainstorming, pending spec review.

## Scope

This is spec 1 of 2. It makes `cron` a fourth `nova new` transport so the next service can be a cron-only service that reaches the database, Redis, search or a broker through the usual Clean Architecture layers. Spec 2, written after this one ships, adds `nova add cron <name>` to append a job to such a project, reusing the layout defined here.

Non-goals:

- An in-process scheduler (`robfig/cron`), distributed locks or overlap protection inside the binary. The deploy layer owns the schedule.
- `nova add cron` (spec 2).
- Generated Kubernetes manifests. Nova ships none for the other transports; the generated README carries snippets instead.
- Any change to the `User` schema. The earlier "deactivate idle users" idea was only an illustration and is out of scope.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Where cron lives | A new transport value: `--transport cron` | A cron-only service is a project `nova new` creates; `HasCron()` and the `cron` config section already anticipate it. |
| Execution model | Run-once: `myapp cron <job>` boots, runs one job, exits 0 or 1 | The scheduler (k8s CronJob, crontab) gets retries, missed-run catch-up, overlap control and failure visibility for free; no lock, no long-running lifecycle, no new dependency. |
| Command shape | `cron` parent command; one cobra command per job in `cmd/cron/`, each with its own flags | Adopted from the scotch service: `myapp cron --help` lists jobs without booting, and per-job flags let ops scope or resume a run. |
| DI | One wire and one fx injector per job, returning a shared `CronApp` | Each run builds only its job's dependencies, so an outage in a dependency one job never touches cannot fail it; spec 2 only appends declarations. |
| Job input | The parsed flags are an injector argument (`fx.Supply` under fx) | Keeps `Job` a plain `Run(ctx) error` interface, so the lifecycle is fully generic. |
| Sample job | `user-scan`: keyset batch walk over users through a narrow `userscan` usecase | The shape most real jobs take; reuses the existing `ListAfter` query on every engine; never builds `usersvc`, so no JWT keys are needed. |

## Lessons taken from scotch

Kept: the `cron` parent command over a package of per-job commands, per-job flags, thin job structs in `internal/transport/cronjob/` that call one usecase method, and one injector per job.

Changed, because each one hurts a run-once job in production:

| scotch | nova |
| --- | --- |
| `Run(ctx, id)` returns nothing; a failed job exits 0 | `Run(ctx) error`; failure reaches cobra and exits 1 |
| `context.WithCancel(Background())`; SIGTERM kills mid-run with no cleanup | `signal.NotifyContext`; the job sees ctx cancel, DI cleanup runs |
| `panic(err)` on DI init failure | The error is returned with context |
| The lifecycle is copied into `app/<job>.go` per job | One shared lifecycle in `app/cron.go`; each job adds a three-line function |
| No timeout, outcome log or span | `cronjob.Runner` applies a timeout, one outcome line, a span and a panic guard |
| `logger.InitZerolog()` per command | The logger comes from DI, as in the other transports |

## Generated layout

```text
cmd/cron.go                          cronCommand(): parent "cron", AddCommand(cron.UserScan())
cmd/cron/user_scan.go                "user-scan" + --after-id → app.RunUserScan(afterID)
internal/app/cron.go                 runCron(initialize) lifecycle + RunUserScan(afterID)
internal/transport/cronjob/
  job.go                             type Job interface { Name() string; Run(ctx context.Context) error }
  runner.go                          Runner + Policy: correlation, span, deadline, panic guard, outcome log
  runner_test.go
  user/scan.go                       ScanFlags, ScanJob → userscansvc.Scan
internal/usecase/userscan/
  dto.go                             ScanInput, ScanResult
  service.go                         Service{users domain.UserRepository}.Scan
  service_test.go
internal/infrastructure/di/
  app.go                             CronApp{Logger, Tracer, Runner, Job}
  wire.go                            cronInfraSet + InitializeUserScanJob
  provider.go                        provideCronPolicy (shared by wire and fx)
  fx.go                              cronModule() + initializeCron helper + InitializeUserScanJob
```

Import aliases follow the existing convention: `userscansvc` for the usecase, `usercron` for `transport/cronjob/user`.

## Runner policy

`cronjob.NewRunner(log observability.Logger, tracer trace.Tracer, policy Policy) *Runner`. `Runner.Run(ctx, job) error` applies the policy to one run, in the shape of `worker.Worker.run`:

1. Correlation: mint a `run_id` (uuid), set it with `requestid.With`, and store a logger scoped with `job`, `run_id` and `trace_id` via `logctx.With`, so inner layers log with those fields.
2. Span: start a root span `cron <name>` from the injected `trace.Tracer` (DI's tracing provider; a noop tracer in tests); record the error and set the span status on failure.
3. Deadline: `context.WithTimeout(ctx, policy.Timeout)`. `Policy{Timeout}` takes a 30m default when zero (`withDefaults`, as in `worker.Policy`).
4. Panic guard: a recovered panic becomes an error wrapping `ErrJobPanic`, so cleanup still runs and the exit is non-zero.
5. One outcome line: `Info "cron: job finished"` with `duration_ms`; `Error "cron: job failed"` with `errors.LogFields(err)`; `Warn "cron: job interrupted"` when the parent ctx was cancelled.

There are no in-process retries; the scheduler's `backoffLimit` owns retrying.

## Process lifecycle

`app.runCron(initialize func(context.Context) (*di.CronApp, func(), error)) error` mirrors `RunWorker`:

- `signal.NotifyContext(SIGINT, SIGTERM)`, then `initialize(ctx)`; a failure returns `initialize cron job: <err>`.
- `defer cleanup()` with the existing `shutdown complete` and duration log.
- Returns `app.Runner.Run(ctx, app.Job)`; cobra `RunE` turns a non-nil error into `os.Exit(1)`.

Exit codes: 0 when the job completed; 1 for a job error, panic, timeout, SIGTERM or DI failure.

Overlap is the deploy layer's job (`concurrencyPolicy: Forbid`, `flock -n`). Every job must be idempotent, resumable, and return when ctx is done; the generated README states this contract.

## Sample job: `user-scan`

`userscan.Service` depends only on `domain.UserRepository`:

```go
const batchSize = 500

type ScanInput struct{ AfterID int64 } // 0 = from the start

type ScanResult struct{ Scanned, LastID int64 }

func (s *Service) Scan(ctx context.Context, in ScanInput) (*ScanResult, error)
```

`Scan` loops: check `ctx.Err()`, read `ListAfter(ctx, res.LastID, batchSize)`, advance `Scanned` and `LastID`, log progress at Debug, and stop on a short page. On any error it returns the result so far, so `LastID` is the resume point. A comment in the loop marks where a real job's per-batch business goes; that work must be idempotent, since a resumed run can see a page twice. There is no separate `process` hook: an always-nil method would fail the generated project's `unparam` lint.

`usercron.ScanJob`:

```go
type ScanFlags struct{ AfterID int64 } // named so DI can inject it

func NewScanJob(scan *userscansvc.Service, flags ScanFlags) *ScanJob
func (*ScanJob) Name() string { return "user-scan" }
func (j *ScanJob) Run(ctx context.Context) error
```

On failure `Run` returns `errors.Wrapf(err, "user-scan: resume with --after-id=%d", res.LastID)`; on success it logs `user-scan: done` with `scanned` and `last_id`.

`cmd/cron/user_scan.go` defines `UserScan() *cobra.Command`: `Use: "user-scan"`, `Args: cobra.NoArgs`, an `--after-id` int64 flag, and `RunE` calling `app.RunUserScan(afterID)`.

## DI

```go
// wire.go
var cronInfraSet = wire.NewSet(
	provideCronPolicy,
	cronjob.NewRunner,
)

func InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error) {
	panic(wire.Build(
		infraSet,
		cronInfraSet,
		adapterSet,
		usecaseSet,
		usercron.NewScanJob,
		wire.Bind(new(cronjob.Job), new(*usercron.ScanJob)),
		wire.Struct(new(CronApp), "*"),
	))
}
```

Under fx, `initializeCron(ctx, job ...fx.Option)` holds the shared `fx.New` (`infraModule`, `cronModule`, `adapterModule`, `usecaseModule`, `fx.Populate`, start, and the stop-budget cleanup used by the other injectors), and each job's injector is one call:

```go
func InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error) {
	return initializeCron(ctx,
		fx.Supply(flags),
		fx.Provide(fx.Annotate(usercron.NewScanJob, fx.As(new(cronjob.Job)))),
	)
}
```

`usecaseSet` and the fx usecase module gain `userscansvc.NewService` under `HasCron`. `provideCronPolicy(cfg)` maps `cfg.Cron.Timeout` onto `cronjob.Policy`.

Adding a job by hand, the steps spec 2 automates: a usecase method; `transport/cronjob/<feature>/<job>.go`; `cmd/cron/<job>.go` plus one `AddCommand` entry; `app.Run<Job>`; one wire injector and one fx injector.

## Options and validation

- `config.Transports` becomes `http, grpc, worker, cron`; the prompt and `--transport` help derive from it.
- No extra constraints: any database (including `none`, which uses the memory repo), cache, search or queue is valid. `validateHTTPFramework` already rejects `--http-framework` off HTTP.
- `cli` stays rejected; tests and doc examples that used `cron` as the invalid value switch to `cli`.
- `nova new`'s next steps end with `go run main.go cron user-scan`.
- `buildFileList`: the `HasHTTP() || HasGRPC() || HasWorker()` gates on `di/app.go`, `provider.go`, `fx.go` and `fx_provider.go` add `|| HasCron()` (`fx_provider.go` itself needs no cron provider); the new files are gated on `HasCron()`; the existing unrendered `cmd/cron.go.tmpl` is rewritten as the parent command.

## Generated project files

| File | Change |
| --- | --- |
| `Dockerfile` | `CMD ["cron", "user-scan"]`; no `EXPOSE` or `HEALTHCHECK` (already gated on HTTP/gRPC) |
| `docker-compose.yaml` | The app service runs the job once and exits; the comment names `docker compose run --rm app cron user-scan`; the "JWT keys" note is gated on `or .HasHTTP .HasGRPC` instead of `not .HasWorker` |
| `.env.example` | The JWT block is gated on `or .HasHTTP .HasGRPC`; `CRON_TIMEOUT` is not listed (not a secret or endpoint) |
| `base.yaml`, `config.go` | `cron:` / `timeout: 30m` and `CronConfig{Timeout time.Duration}` (env `CRON_TIMEOUT`) replace `spec`; `Summary()` logs `cron_timeout` |
| `README.md` | Title, tree and run lines branch on `HasCron`; a new "Scheduling" section: the job contract, exit codes, a k8s `CronJob` snippet (`schedule`, `concurrencyPolicy: Forbid`, `startingDeadlineSeconds`, `backoffLimit`, `activeDeadlineSeconds` above `cron.timeout`, `args: ["cron", "user-scan"]`), a crontab line with `flock -n`, resuming with `--after-id`, and the steps to add a job |
| `CLAUDE.md` | A transport bullet, the reference-feature sentence, the run command, and the DI hint for a new job |
| `Makefile`, CI | No change |

## Nova docs

Updated in the same change (working principle 5), with snippets quoted from a rendered project:

- `README.md`: drop `cron` from the rejected list; list it as a transport.
- `CLAUDE.md`: transports, matrix and bootstrap counts.
- `docs/01-cli-options.md`: `cron` valid; the invalid-transport example uses `cli`.
- `docs/02-project-layout.md`: `cmd/cron/`, `transport/cronjob/`, `usecase/userscan/`.
- `docs/03-architecture-rules.md`: `transport/cronjob/` is scaffolded, not a pattern to grow into.
- `docs/07-transport-layer.md`: the cronjob section describes the runner, job contract and sample from rendered output.
- `docs/13-cross-cutting.md`: drop the "not rendered" bullet.

Then grep for `cron.spec`, `RunCronJobScheduler`, `Not scaffolded`, `not rendered` and `transport "cron"` and fix every hit.

## Testing

Fast, in `make test`, written before the templates:

- `TestCronTransportFiles`: a cron render has the files in [Generated layout](#generated-layout) and no `transport/http|grpc|worker`, `useraudit` or `infrastructure/server` files; http, grpc and worker renders have no `cronjob`, `cmd/cron` or `userscan` files.
- Content pins: `Dockerfile` `CMD ["cron", "user-scan"]` without `EXPOSE`; no `JWT_` in `.env.example`; `base.yaml` has `timeout: 30m` under `cron:` and no `spec`; `app.go` declares `CronApp`; `wire.go` declares `InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags)` and the `wire.Bind`; `fx.go` calls `initializeCron`; `cmd/root.go` registers `cronCommand()`.
- `TestFxDISelection` gains a cron case requiring `InitializeUserScanJob`; `TestTransportLoggerPortDecoupling` adds `internal/transport/cronjob/runner.go`.
- `options_test.go`: `cron` validates with any database, cache and queue; the invalid-transport cases use `cli`.
- `TestDocsClaims` pins each changed doc claim.

Generated unit tests, run by the bootstrap:

- `runner_test.go` (fake `Job`, nop logger): success; error passthrough; a job outliving `Policy.Timeout` returns `context.DeadlineExceeded`; a panic wraps `ErrJobPanic`; a cancelled parent is reported as interrupted; a zero `Policy` uses the 30m default.
- `userscan/service_test.go` (fake repository): `2*batchSize+1` rows are scanned in three pages with `LastID` the maximum id; `AfterID` skips earlier rows; an error on page 2 returns page 1's `LastID`; a cancelled ctx stops before the next page.

Heavy, in `make test-all`, run alone:

- `TestGenerateMatrix` adds `cron_{wire,fx}_{postgres,none}` with a `wireGenCronStub` (36 → 40 cases). MySQL's `adapterSet` is already covered by the http and worker rows.
- `TestGeneratedProjectBootstrap` adds two cases (8 → 10) and runs `./internal/transport/cronjob/...` when `HasCron`:
  - `cron/postgres/sqlc/redis/wire`: real `make gen`, `go build`, `golangci-lint`, generated tests. This is the only check that real wire accepts the per-job injector, its argument and the `Bind`.
  - `cron/none/fx`: the same, then `go run . cron user-scan` must exit 0 and log `cron: job finished`, proving the fx graph, the runner and the exit path end to end.
- Counts and timings in nova's `CLAUDE.md`, `README.md` and `Makefile` help follow.

## Done when

`make lint` and `make test` are green, `make test-all` is green when run alone, every doc listed above matches rendered output, and the grep in [Nova docs](#nova-docs) finds nothing stale.
