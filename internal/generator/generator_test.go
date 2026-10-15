package generator

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	// Aliased: the package-level const `yaml` (generator.go) shadows the
	// import name.
	yamlv3 "gopkg.in/yaml.v3"

	"github.com/quyennguyenvu/nova/internal/config"
	"github.com/quyennguyenvu/nova/internal/manifest"
)

// postgresDBGenStub is written into the rendered postgres adapter so go vet
// can resolve sqlc symbols without a live database. Includes both the user
// table queries AND the user_audit_log queries — worker-mode renders both.
const postgresDBGenStub = `package dbgen

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type DBTX any
type Queries struct{}

func New(DBTX) *Queries { return &Queries{} }

type User struct {
	ID                   int64
	Email, Name, Password string
	CreatedAt, UpdatedAt pgtype.Timestamptz
}

type CreateUserParams struct {
	Email, Name, Password string
	CreatedAt, UpdatedAt  pgtype.Timestamptz
}
type UpdateUserParams struct {
	ID        int64
	Name      string
	UpdatedAt pgtype.Timestamptz
}
type ListUsersParams struct {
	Email         string
	Limit, Offset int32
}
type ListUsersAfterParams struct {
	Cursor int64
	Limit  int32
}

func (*Queries) CreateUser(context.Context, CreateUserParams) (User, error)             { return User{}, nil }
func (*Queries) GetUserByID(context.Context, int64) (User, error)                       { return User{}, nil }
func (*Queries) GetUserByEmail(context.Context, string) (User, error)                   { return User{}, nil }
func (*Queries) UpdateUser(context.Context, UpdateUserParams) error                     { return nil }
func (*Queries) DeleteUser(context.Context, int64) error                                { return nil }
func (*Queries) ListUsers(context.Context, ListUsersParams) ([]User, error)             { return nil, nil }
func (*Queries) CountUsers(context.Context, string) (int64, error)                      { return 0, nil }
func (*Queries) ListUsersAfter(context.Context, ListUsersAfterParams) ([]User, error)   { return nil, nil }

// user_audit_log
type UserAuditLog struct {
	ID         int64
	EventID    string
	UserID     int64
	EventType  string
	Payload    []byte
	OccurredAt pgtype.Timestamptz
}
type RecordUserAuditParams struct {
	EventID    string
	UserID     int64
	EventType  string
	Payload    []byte
	OccurredAt pgtype.Timestamptz
}

func (*Queries) RecordUserAudit(context.Context, RecordUserAuditParams) (int64, error) { return 0, nil }
`

// MySQL's sqlc emits CreateUser → (int64, error) (LastInsertID) rather than
// the Postgres-style RETURNING row. RecordUserAudit is :execrows on both.
const mysqlDBGenStub = `package dbgen

import (
	"context"
	"encoding/json"
	"time"
)

type DBTX any
type Queries struct{}

func New(DBTX) *Queries { return &Queries{} }

type User struct {
	ID                   int64
	Email, Name, Password string
	CreatedAt, UpdatedAt time.Time
}

type CreateUserParams struct {
	Email, Name, Password string
	CreatedAt, UpdatedAt  time.Time
}
type UpdateUserParams struct {
	ID        int64
	Name      string
	UpdatedAt time.Time
}
type ListUsersParams struct {
	Email         string
	Limit, Offset int32
}
type ListUsersAfterParams struct {
	Cursor int64
	Limit  int32
}

// MySQL's sqlc wraps CountUsers in a Params struct (the named arg appears twice
// in positional ? form); Postgres collapses the repeated $1 to a scalar string.
type CountUsersParams struct {
	Email string
}

func (*Queries) CreateUser(context.Context, CreateUserParams) (int64, error)            { return 0, nil }
func (*Queries) GetUserByID(context.Context, int64) (User, error)                       { return User{}, nil }
func (*Queries) GetUserByEmail(context.Context, string) (User, error)                   { return User{}, nil }
func (*Queries) UpdateUser(context.Context, UpdateUserParams) error                     { return nil }
func (*Queries) DeleteUser(context.Context, int64) error                                { return nil }
func (*Queries) ListUsers(context.Context, ListUsersParams) ([]User, error)             { return nil, nil }
func (*Queries) CountUsers(context.Context, CountUsersParams) (int64, error)            { return 0, nil }
func (*Queries) ListUsersAfter(context.Context, ListUsersAfterParams) ([]User, error)   { return nil, nil }

// user_audit_log
type UserAuditLog struct {
	ID         int64
	EventID    string
	UserID     int64
	EventType  string
	Payload    json.RawMessage
	OccurredAt time.Time
}
type RecordUserAuditParams struct {
	EventID    string
	UserID     int64
	EventType  string
	Payload    json.RawMessage
	OccurredAt time.Time
}

func (*Queries) RecordUserAudit(context.Context, RecordUserAuditParams) (int64, error) { return 0, nil }
`

// wireGenStub satisfies the Initialize* symbol that wire.go declares behind
// a build tag. We don't run wire — only ensure the package compiles when
// wire_gen.go is missing.
const wireGenHTTPStub = `// Code generated by Wire. DO NOT EDIT.
//go:build !wireinject
// +build !wireinject

package di

import "context"

func InitializeHTTPServer(_ context.Context) (*HTTPApp, func(), error) {
	return &HTTPApp{}, func() {}, nil
}
`

const wireGenWorkerStub = `// Code generated by Wire. DO NOT EDIT.
//go:build !wireinject
// +build !wireinject

package di

import "context"

func InitializeWorker(_ context.Context) (*WorkerApp, func(), error) {
	return &WorkerApp{}, func() {}, nil
}
`

const wireGenGRPCStub = `// Code generated by Wire. DO NOT EDIT.
//go:build !wireinject
// +build !wireinject

package di

import "context"

func InitializeGRPCServer(_ context.Context) (*GRPCApp, func(), error) {
	return &GRPCApp{}, func() {}, nil
}
`

// TestGenerateMatrix ensures every transport × database (× broker for the
// worker case) combo renders to a project that at least `go vet`s. Drop a
// variant by accident and this fails. Skipped under -short because each
// combo runs `go mod tidy` + `go vet` (10–20s).
func TestGenerateMatrix(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping generator matrix in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable")
	}

	httpFrameworks := []string{"fiber", "gin", "chi", "echo"}
	dbs := []string{"postgres", "mysql"}
	brokers := []string{"kafka", "rabbitmq"}
	// Both DI engines render their own di package: Wire emits a build-tagged
	// wire.go (skipped by `go vet`, so we stub wire_gen.go) while fx emits a
	// plain fx.go that `go vet` actually compiles. Running every combo under
	// both proves fx's runtime wiring is as complete as Wire's generated graph.
	dis := []string{"wire", "fx"}

	for _, di := range dis {
		for _, fw := range httpFrameworks {
			for _, db := range dbs {
				name := "http_" + di + "_" + fw + "_" + db
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					runHTTPMatrixCase(t, fw, db, di)
				})
			}
		}
		for _, db := range dbs {
			for _, broker := range brokers {
				name := "worker_" + di + "_" + db + "_" + broker
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					runWorkerMatrixCase(t, db, broker, di)
				})
			}
		}
		// HTTP projects that also publish: the only rows that compile the
		// publisher next to an HTTP transport (fx wraps the RabbitMQ
		// connection and publisher cleanups in lifecycle hooks).
		t.Run("http_"+di+"_fiber_postgres_rabbitmq", func(t *testing.T) {
			t.Parallel()
			cfg := httpMatrixConfig("fiber", "postgres", di)
			cfg.MessageQueue = "rabbitmq"
			renderAndVet(t, cfg, wireGenHTTPStub)
		})
		for _, fw := range httpFrameworks {
			name := "http_" + di + "_" + fw + "_none"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				renderAndVet(t, noDBMatrixConfig(fw, di), wireGenHTTPStub)
			})
		}
		t.Run("grpc_"+di+"_postgres", func(t *testing.T) {
			t.Parallel()
			renderAndVet(t, grpcMatrixConfig(di), wireGenGRPCStub)
		})
	}
}

func runHTTPMatrixCase(t *testing.T, framework, database, di string) {
	t.Helper()
	cfg := httpMatrixConfig(framework, database, di)
	renderAndVet(t, cfg, wireGenHTTPStub)
}

func runWorkerMatrixCase(t *testing.T, database, broker, di string) {
	t.Helper()
	cfg := workerMatrixConfig(database, broker, di)
	renderAndVet(t, cfg, wireGenWorkerStub)
}

func renderAndVet(t *testing.T, cfg *config.ProjectConfig, wireStub string) {
	t.Helper()
	dir := t.TempDir()

	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gen.Out = io.Discard
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}

	if cfg.QueryGen == "sqlc" {
		writeDBGenStub(t, dir, cfg.Database)
	}
	if cfg.UseWire() {
		stubPath := filepath.Join(dir, "internal/infrastructure/di/wire_gen.go")
		if writeErr := os.WriteFile(stubPath, []byte(wireStub), 0o644); writeErr != nil {
			t.Fatalf("write wire_gen stub: %v", writeErr)
		}
	}
	if cfg.HasGRPC() {
		generateProtos(t, dir)
	}

	if tidyErr := runGo(t, dir, "mod", "tidy"); tidyErr != nil {
		t.Fatalf("go mod tidy: %v", tidyErr)
	}
	if vetErr := runGo(t, dir, "vet", "./..."); vetErr != nil {
		t.Fatalf("go vet: %v", vetErr)
	}
	// wire.go is build-tagged out of the vet above (the stub stands in for
	// wire_gen.go); vet it on its own so a provider set that references a
	// missing package or symbol fails here, not only in the bootstrap.
	if cfg.UseWire() {
		if vetErr := runGo(t, dir, "vet", "-tags", "wireinject", "./internal/infrastructure/di/"); vetErr != nil {
			t.Fatalf("go vet -tags wireinject: %v", vetErr)
		}
	}
}

// generateProtos runs the rendered project's own codegen for api/gen (buf
// with go run-pinned plugins) — no hand-written stub could stand in for the
// generated service interface. Skips when buf is missing, like the
// bootstraps skip on a missing tool.
func generateProtos(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf not on PATH")
	}
	cmd := exec.Command("buf", "generate")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("buf generate: %v\n%s", err, out)
	}
}

func writeDBGenStub(t *testing.T, dir, database string) {
	t.Helper()
	stubDir := filepath.Join(dir, "internal/adapter/repository", database, "dbgen")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatalf("mkdir dbgen: %v", err)
	}
	stub := postgresDBGenStub
	if database == "mysql" {
		stub = mysqlDBGenStub
	}
	if err := os.WriteFile(filepath.Join(stubDir, "stub.go"), []byte(stub), 0o644); err != nil {
		t.Fatalf("write dbgen stub: %v", err)
	}
}

func httpMatrixConfig(framework, database, di string) *config.ProjectConfig {
	cfg := baseMatrixConfig(database, di)
	cfg.Transport = "http"
	cfg.HTTPFramework = framework
	cfg.Cache = "redis"
	cfg.Search = "elasticsearch"
	cfg.MessageQueue = "none"
	return cfg
}

func workerMatrixConfig(database, broker, di string) *config.ProjectConfig {
	cfg := baseMatrixConfig(database, di)
	cfg.Transport = "worker"
	cfg.HTTPFramework = ""
	cfg.Cache = "none"
	cfg.MessageQueue = broker
	return cfg
}

func baseMatrixConfig(database, di string) *config.ProjectConfig {
	v := runtime.Version()
	cfg := &config.ProjectConfig{
		ProjectName:  "matrixtest",
		ModuleName:   "matrixtest",
		GoVersion:    strings.TrimPrefix(v, "go"),
		Database:     database,
		DBDriver:     "pgx",
		QueryGen:     "sqlc",
		ConfigFormat: "yaml",
		DI:           di,
		// Docker on: the Dockerfile + docker-compose.yaml templates then get
		// parse coverage across every transport × db × broker combo (their
		// content is asserted in TestDockerComposeStack).
		IncludeDocker: true,
		IncludeCI:     false,
	}
	if database == "mysql" {
		cfg.DBDriver = "database/sql"
	}
	return cfg
}

// noDBMatrixConfig is an HTTP project with no database, cache, search, or
// broker: the leanest supported shape and the one most likely to leave an
// unconditional import or variable dangling.
func noDBMatrixConfig(framework, di string) *config.ProjectConfig {
	cfg := baseMatrixConfig("none", di)
	cfg.Transport = "http"
	cfg.HTTPFramework = framework
	cfg.DBDriver = ""
	cfg.QueryGen = ""
	cfg.Cache = "none"
	cfg.Search = "none"
	cfg.MessageQueue = "none"
	return cfg
}

// grpcMatrixConfig is the gRPC transport over postgres; it has no cache,
// search or broker so the row stays cheap.
func grpcMatrixConfig(di string) *config.ProjectConfig {
	cfg := baseMatrixConfig("postgres", di)
	cfg.Transport = "grpc"
	cfg.Cache = "none"
	cfg.Search = "none"
	cfg.MessageQueue = "none"
	return cfg
}

// noDBGRPCConfig is the gRPC transport with no database: the shape the
// bootstrap boots for real (in-memory repository, fx graph, health probe).
func noDBGRPCConfig(di string) *config.ProjectConfig {
	cfg := noDBMatrixConfig("", di)
	cfg.Transport = "grpc"
	return cfg
}

// TestNovaYAMLRecordsStack pins the nova.yaml emitted by `nova new`: it must
// parse back through manifest.Load and record the project's ACTUAL stack, so
// `nova add` resolves the real {db}/DI/framework instead of falling back to
// manifest.Default() (fiber/postgres/redis/wire). Without the file, Load() only
// fills Module from go.mod and leaves Stack at the defaults — silently wrong for
// any non-default project.
func TestNovaYAMLRecordsStack(t *testing.T) {
	t.Parallel()
	cfg := httpMatrixConfig("gin", "mysql", "fx")
	cfg.ModuleName = "example.com/shop"
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	if m.Module != "example.com/shop" {
		t.Errorf("module = %q, want example.com/shop", m.Module)
	}
	if m.Stack.Database != "mysql" {
		t.Errorf("stack.database = %q, want mysql (nova.yaml not read?)", m.Stack.Database)
	}
	if m.Stack.HTTPFramework != "gin" {
		t.Errorf("stack.http_framework = %q, want gin", m.Stack.HTTPFramework)
	}
	if m.Stack.DI != "fx" {
		t.Errorf("stack.di = %q, want fx", m.Stack.DI)
	}

	// The {db} placeholder must resolve to the recorded engine.
	r, ok := m.Resolve("repository", "Order", "")
	if !ok {
		t.Fatal("repository missing from resolved layout")
	}
	if r.Dir != "internal/adapter/repository/mysql" {
		t.Errorf("repository dir = %q, want internal/adapter/repository/mysql", r.Dir)
	}
}

// TestNovaYAMLMessageQueueNone pins the none-fallback: a project with no cache
// or queue must still emit valid, parseable YAML (the `{{ if }}` guards render
// "none", not an empty value that breaks Resolve or the YAML scalar).
func TestNovaYAMLMessageQueueNone(t *testing.T) {
	t.Parallel()
	cfg := workerMatrixConfig("postgres", "kafka", "wire")
	cfg.Cache = "none"
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	if m.Stack.Cache != "none" {
		t.Errorf("stack.cache = %q, want none", m.Stack.Cache)
	}
	if m.Stack.HTTPFramework != "none" {
		t.Errorf("stack.http_framework = %q, want none (worker has no framework)", m.Stack.HTTPFramework)
	}
	if m.Stack.MessageQueue != "kafka" {
		t.Errorf("stack.message_queue = %q, want kafka", m.Stack.MessageQueue)
	}
}

// TestRegistrarEmbedsSectionPrefix pins the embedded-base routing layout: the
// registrar is feature-owned (package user, beside its handler) and gets its
// Prefix() by embedding the section base v1.Prefixed rather than declaring it,
// while the prefix is defined once in the section package (v1.APIPrefix). The
// matrix proves it compiles; this pins the shape.
func TestRegistrarEmbedsSectionPrefix(t *testing.T) {
	t.Parallel()
	cfg := httpMatrixConfig("fiber", "postgres", "wire")
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}

	reg, err := os.ReadFile(filepath.Join(dir, "internal/transport/http/v1/user/registrar.go"))
	if err != nil {
		t.Fatalf("registrar in feature package: %v", err)
	}
	regSrc := string(reg)
	for _, want := range []string{"package user", "func NewRegistrar(h *Handler", "v1.Prefixed"} {
		if !strings.Contains(regSrc, want) {
			t.Errorf("registrar.go missing %q", want)
		}
	}
	if strings.Contains(regSrc, "func (*Registrar) Prefix()") {
		t.Error("registrar should inherit Prefix() from embedded v1.Prefixed, not declare it")
	}

	// Section base: prefix declared once, Prefix() provided via the Prefixed embed.
	base, err := os.ReadFile(filepath.Join(dir, "internal/transport/http/v1/v1.go"))
	if err != nil {
		t.Fatalf("v1 base package: %v", err)
	}
	for _, want := range []string{"package v1", `APIPrefix = "/api/v1"`, "func (Prefixed) Prefix() string"} {
		if !strings.Contains(string(base), want) {
			t.Errorf("v1.go missing %q", want)
		}
	}
	// The old version-package registrar location must not exist.
	if _, statErr := os.Stat(filepath.Join(dir, "internal/transport/http/v1/registrar.go")); statErr == nil {
		t.Error("registrar.go should not exist at the v1 package root")
	}
}

// TestChiRouterGroupsSharedPrefix guards the chi-specific fix: chi.Router.Route
// panics on a duplicate mount path, so when features share a Prefix (the norm
// under the embedded v1.Prefixed base) NewRouter must group by Prefix and mount
// each once — not call Route per registrar. The matrix renders a single feature
// so it can't catch a regression here; this pins the grouping in the template.
func TestChiRouterGroupsSharedPrefix(t *testing.T) {
	t.Parallel()
	cfg := httpMatrixConfig("chi", "postgres", "wire")
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}
	src, err := os.ReadFile(filepath.Join(dir, "internal/transport/http/router.go"))
	if err != nil {
		t.Fatalf("router.go: %v", err)
	}
	s := string(src)
	if strings.Contains(s, "router.Route(r.Prefix(), r.Register)") {
		t.Error("chi NewRouter mounts per-registrar — features sharing a Prefix will panic on duplicate Route")
	}
	for _, want := range []string{"byPrefix", "router.Route(prefix"} {
		if !strings.Contains(s, want) {
			t.Errorf("chi router.go missing prefix-grouping marker %q", want)
		}
	}
}

// TestWorkerBrokerConfigProvider guards against a regression where the worker
// variants compiled fine but `wire gen` failed because no provider extracted
// the broker sub-config from *config.Config. The wire.go file is behind the
// `wireinject` build tag, so `go vet` in the matrix test silently skips it —
// this test asserts on the rendered text directly.
//
// The current shape uses wire.FieldsOf(new(*config.Config), "<Field>") to
// project the sub-config as a value, and the broker constructor takes the
// value type. Both halves of that contract are asserted here.
func TestWorkerBrokerConfigProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		broker        string
		fieldName     string
		ctorSymbol    string
		ctorSubConfig string
		pubsubFile    string
	}{
		{"kafka", "Kafka", "NewKafkaProducer", "cfg config.KafkaConfig", "kafka.go"},
		{"rabbitmq", "RabbitMQ", "NewRabbitMQConnection", "cfg config.RabbitMQConfig", "rabbitmq.go"},
	}

	for _, tc := range cases {
		t.Run(tc.broker, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfg := workerMatrixConfig("postgres", tc.broker, "wire")

			gen, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}

			wire := readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go"))
			expectFieldsOf := `wire.FieldsOf(new(*config.Config), "` + tc.fieldName + `")`
			if !strings.Contains(wire, expectFieldsOf) {
				t.Fatalf(
					"wire.go infraSet missing %q — broker constructor cannot satisfy its config dep",
					expectFieldsOf,
				)
			}

			pubsub := readFile(t, filepath.Join(dir, "internal/infrastructure/pubsub", tc.pubsubFile))
			if !strings.Contains(pubsub, tc.ctorSubConfig) {
				t.Fatalf(
					"%s: expected %s to take %s (value, not pointer)",
					tc.pubsubFile,
					tc.ctorSymbol,
					tc.ctorSubConfig,
				)
			}

			provider := readFile(t, filepath.Join(dir, "internal/infrastructure/di/provider.go"))
			if strings.Contains(provider, "provideKafkaConfig") || strings.Contains(provider, "provideRabbitMQConfig") {
				t.Fatalf("provider.go contains a stale broker config helper — should be replaced by wire.FieldsOf")
			}
		})
	}
}

// TestFxDISelection asserts the fx DI engine renders fx.go (and not wire.go),
// and that fx.go exposes the same Initialize* entry point as the Wire variant
// for each transport. The app layer calls these by name regardless of DI, so a
// missing/renamed entry point silently breaks the chosen transport.
func TestFxDISelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		cfg        *config.ProjectConfig
		entryPoint string
	}{
		{"http", httpMatrixConfig("fiber", "postgres", "fx"), "func InitializeHTTPServer("},
		{"worker", workerMatrixConfig("postgres", "kafka", "fx"), "func InitializeWorker("},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			gen, err := New(tc.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}

			diDir := filepath.Join(dir, "internal/infrastructure/di")
			if _, statErr := os.Stat(filepath.Join(diDir, "wire.go")); !os.IsNotExist(statErr) {
				t.Fatalf("fx project rendered wire.go — wire and fx must be mutually exclusive")
			}

			fxSrc := readFile(t, filepath.Join(diDir, "fx.go"))
			if !strings.Contains(fxSrc, tc.entryPoint) {
				t.Fatalf(
					"fx.go missing entry point %q — app layer cannot boot the %s transport",
					tc.entryPoint,
					tc.name,
				)
			}
		})
	}
}

// TestNewRejectsUnsupportedDI guards the DI option set: removed or
// typo'd DI strategies must fail fast in New() rather than render a project
// with no Initialize* functions. "manual" was removed; "" is never valid.
func TestNewRejectsUnsupportedDI(t *testing.T) {
	t.Parallel()
	for _, di := range []string{"manual", "", "spring"} {
		cfg := httpMatrixConfig("fiber", "postgres", di)
		if _, err := New(cfg); err == nil {
			t.Errorf("New() with DI=%q: want error, got nil", di)
		}
	}
	for _, di := range []string{"wire", "fx"} {
		cfg := httpMatrixConfig("fiber", "postgres", di)
		if _, err := New(cfg); err != nil {
			t.Errorf("New() with DI=%q: want nil, got %v", di, err)
		}
	}
}

// TestTransportLoggerPortDecoupling pins the transport→observability.Logger
// seam: transport-layer files must depend on the pkg/observability port, never
// the concrete infrastructure/logger, and the DI graph must bridge the port to
// the concrete *logger.Logger so the App bundle + infra consumers keep it.
// Neither half is caught by the matrix's `go vet` — wire.go is build-tagged
// (vet skips it) and fx resolves its graph at runtime (vet never calls Start) —
// so assert on the rendered text here.
func TestTransportLoggerPortDecoupling(t *testing.T) {
	t.Parallel()

	render := func(t *testing.T, cfg *config.ProjectConfig) string {
		t.Helper()
		dir := t.TempDir()
		gen, err := New(cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if genErr := gen.Generate(dir); genErr != nil {
			t.Fatalf("Generate: %v", genErr)
		}
		return dir
	}

	// Transport files must reference the port and not import the concrete impl.
	assertDecoupled := func(t *testing.T, dir string, files ...string) {
		t.Helper()
		for _, f := range files {
			src := readFile(t, filepath.Join(dir, f))
			if strings.Contains(src, "internal/infrastructure/logger") {
				t.Errorf("%s imports the concrete logger — transport must depend on the observability.Logger port", f)
			}
			if !strings.Contains(src, "observability.Logger") {
				t.Errorf("%s does not reference observability.Logger — logger port not wired through", f)
			}
		}
	}

	// fx matches by exact type, so the port consumers need a concrete→port
	// caster that is both defined and registered in the infra module.
	assertFxCaster := func(t *testing.T, dir string) {
		t.Helper()
		prov := readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx_provider.go"))
		if !strings.Contains(prov, "func provideObservabilityLogger(l *logger.Logger) observability.Logger") {
			t.Error("fx_provider.go missing provideObservabilityLogger caster — fx transport consumers fail at runtime")
		}
		fxSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go"))
		if !strings.Contains(fxSrc, "provideObservabilityLogger,") {
			t.Error("fx.go infraModule does not register the provideObservabilityLogger caster")
		}
	}

	httpFiles := []string{
		"internal/transport/http/router.go",
		"internal/transport/http/middleware/logging.go",
	}
	workerFiles := []string{
		"internal/transport/worker/worker.go",
		"internal/transport/worker/consumer.go",
	}

	t.Run("wire_http", func(t *testing.T) {
		t.Parallel()
		dir := render(t, httpMatrixConfig("fiber", "postgres", "wire"))
		assertDecoupled(t, dir, httpFiles...)
		wireSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go"))
		if !strings.Contains(wireSrc, "wire.Bind(new(observability.Logger), new(*logger.Logger))") {
			t.Error("wire.go infraSet missing the observability.Logger → *logger.Logger bind")
		}
	})

	t.Run("fx_http", func(t *testing.T) {
		t.Parallel()
		dir := render(t, httpMatrixConfig("fiber", "postgres", "fx"))
		assertDecoupled(t, dir, httpFiles...)
		assertFxCaster(t, dir)
	})

	t.Run("wire_worker", func(t *testing.T) {
		t.Parallel()
		dir := render(t, workerMatrixConfig("postgres", "kafka", "wire"))
		assertDecoupled(t, dir, workerFiles...)
	})

	t.Run("fx_worker", func(t *testing.T) {
		t.Parallel()
		dir := render(t, workerMatrixConfig("postgres", "kafka", "fx"))
		assertDecoupled(t, dir, workerFiles...)
		assertFxCaster(t, dir)
	})
}

// TestRequestIDInResponseEnvelope pins the request-ID correlation contract for
// every HTTP framework: the response envelope must expose a requestId field,
// the request-ID context plumbing must live in the transport-neutral
// pkg/requestid (so httpwriter, the worker and the gRPC interceptors can all
// reach it without importing transport/middleware — that would be an import
// cycle), the writer must populate resp.RequestID, and the RequestID middleware
// must thread the ID through requestid.With. None of this is provable by the
// matrix's `go vet` (it compiles fine either way), so assert on the text.
func TestRequestIDInResponseEnvelope(t *testing.T) {
	t.Parallel()

	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gen, err := New(httpMatrixConfig(fw, "postgres", "wire"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}
			assertRequestIDContract(t, dir)
		})
	}
}

// assertRequestIDContract checks one rendered project carries the full
// request-ID correlation seam: envelope field, requestid plumbing, writer
// stamp, and middleware wiring.
func assertRequestIDContract(t *testing.T, dir string) {
	t.Helper()

	// The envelope carries requestId and owns only the HTTP header name.
	env := readFile(t, filepath.Join(dir, "pkg/httputil/response.go"))
	mustContainAll(t, env, "`json:\"requestId,omitempty\"`", `const RequestIDHeader = "X-Request-ID"`)
	mustNotContain(t, env, "context.WithValue(", "func ValidRequestID(")

	// pkg/requestid owns the value: key, accessors and the inbound validator.
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/requestid/requestid.go")),
		"func With(ctx context.Context, id string) context.Context",
		"func From(ctx context.Context) string",
		"func Valid(id string) bool")

	// The writer stamps the ID onto the envelope.
	writer := readFile(t, filepath.Join(dir, "internal/transport/http/httpwriter/writer.go"))
	if !strings.Contains(writer, "resp.RequestID = requestid.From(") {
		t.Error("writer.go does not stamp resp.RequestID from requestid.From — responses won't carry the ID")
	}

	// The middleware threads the ID via the shared plumbing and no longer
	// declares a local copy (which httpwriter couldn't reach).
	rid := readFile(t, filepath.Join(dir, "internal/transport/http/middleware/requestid.go"))
	mustContainAll(t, rid, "httputil.RequestIDHeader", "requestid.Valid(", "requestid.With(")
	if strings.Contains(rid, "type requestIDKey struct{}") {
		t.Error("requestid.go still declares a local request-ID key — should use pkg/requestid")
	}
}

// TestPaginationResponseEnvelope pins the two reusable list-pagination forms in
// the response envelope: the user-facing cursor form (hasMore + nextCursor) and
// the admin offset form (page + totalPage + totalRecord), plus the writer helper
// that serializes either. The matrix's `go vet` only proves these compile (the
// helpers are unused in the scaffold); this asserts the contract is present.
func TestPaginationResponseEnvelope(t *testing.T) {
	t.Parallel()

	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gen, err := New(httpMatrixConfig(fw, "postgres", "wire"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}

			env := readFile(t, filepath.Join(dir, "pkg/httputil/response.go"))
			for _, want := range []string{
				"type CursorMeta struct",
				"`json:\"hasMore\"`",
				"`json:\"nextCursor,omitempty\"`",
				"type PageMeta struct",
				"`json:\"totalPage\"`",
				"`json:\"totalRecord\"`",
				"func NewPageMeta(page, perPage int, totalRecord int64) PageMeta",
				"func SuccessListResponse(data, meta any) Response",
			} {
				if !strings.Contains(env, want) {
					t.Errorf("pkg/httputil/response.go missing %q", want)
				}
			}

			writer := readFile(t, filepath.Join(dir, "internal/transport/http/httpwriter/writer.go"))
			if !strings.Contains(writer, "func WriteListJSON(") {
				t.Error("writer.go missing WriteListJSON — paginated lists can't be written")
			}
			if !strings.Contains(writer, "httputil.SuccessListResponse(") {
				t.Error("writer.go WriteListJSON does not call httputil.SuccessListResponse")
			}
		})
	}
}

// TestUserListAdminPagination pins the reference User List endpoint to the admin
// page form end to end: the sqlc COUNT query, the domain Count port, the
// service returning ListOutput, the assembler projecting page meta, and the
// handler writing data + meta via WriteListJSON. The matrix's `go vet` proves it
// compiles; this proves the layers are actually wired (a handler that dropped
// the meta would still vet).
func TestUserListAdminPagination(t *testing.T) {
	t.Parallel()

	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gen, err := New(httpMatrixConfig(fw, "postgres", "wire"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}

			checks := []struct{ file, want string }{
				{"sqlc/query/user.sql", "-- name: CountUsers :one"},
				{"internal/domain/user.go", "Count(ctx context.Context, filter UserFilter) (int64, error)"},
				{"internal/adapter/repository/postgres/user_repository.go", "func (r *UserRepository) Count("},
				{"internal/usecase/user/dto.go", "type ListOutput struct"},
				{"internal/usecase/user/service.go", ") (*ListOutput, error) {"},
				{"internal/usecase/user/service.go", "s.userRepo.Count(ctx, filter)"},
				{"internal/transport/http/v1/user/assembler.go", "httputil.NewPageMeta(out.Page"},
				{"internal/transport/http/v1/user/handler.go", "WriteListJSON("},
				{"internal/transport/http/v1/user/handler.go", "ToUserListMeta(out)"},
			}
			for _, c := range checks {
				body := readFile(t, filepath.Join(dir, c.file))
				if !strings.Contains(body, c.want) {
					t.Errorf("%s missing %q", c.file, c.want)
				}
			}
		})
	}
}

// TestUserPublicListCursorPagination pins the public, cursor-paginated User
// directory end to end: the keyset sqlc query, the domain ListAfter port, the
// service producing a cursor page (encodeCursor + limit+1 probe), the name-only
// public projection, the cursor meta, the handler, and — crucially — that the
// route is mounted on a NO-AUTH group (the user asked for no auth). The matrix's
// `go vet` proves it compiles; this proves it's wired and unauthenticated.
func TestUserPublicListCursorPagination(t *testing.T) {
	t.Parallel()

	// Exact public mount per framework — proves PublicList sits on the no-auth
	// group/route, never the Auth-gated /users subgroup.
	publicMount := map[string]string{
		"fiber": `pub.Get("/users", r.handler.PublicList)`,
		"gin":   `pub.GET("/users", r.handler.PublicList)`,
		"echo":  `pub.GET("/users", r.handler.PublicList)`,
		"chi":   `router.Get("/public/users", r.handler.PublicList)`,
	}

	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gen, err := New(httpMatrixConfig(fw, "postgres", "wire"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if genErr := gen.Generate(dir); genErr != nil {
				t.Fatalf("Generate: %v", genErr)
			}

			checks := []struct{ file, want string }{
				{"sqlc/query/user.sql", "-- name: ListUsersAfter :many"},
				{"internal/domain/user.go", "ListAfter(ctx context.Context, cursor int64, limit int32)"},
				{"internal/adapter/repository/postgres/user_repository.go", ") ListAfter("},
				{"internal/usecase/user/dto.go", "type PublicListOutput struct"},
				{"internal/usecase/user/dto.go", "type PublicOutput struct"},
				{"internal/usecase/user/service.go", "func (s *Service) PublicList("},
				{"internal/usecase/user/service.go", "encodeCursor("},
				{"internal/transport/http/v1/user/dto.go", "type PublicUserResponse struct"},
				{"internal/transport/http/v1/user/assembler.go", "httputil.CursorMeta{"},
				{"internal/transport/http/v1/user/handler.go", "func (h *Handler) PublicList("},
				{"internal/transport/http/v1/user/handler.go", "ToPublicUserListMeta(out)"},
			}
			for _, c := range checks {
				body := readFile(t, filepath.Join(dir, c.file))
				if !strings.Contains(body, c.want) {
					t.Errorf("%s missing %q", c.file, c.want)
				}
			}

			reg := readFile(t, filepath.Join(dir, "internal/transport/http/v1/user/registrar.go"))
			if !strings.Contains(reg, publicMount[fw]) {
				t.Errorf("registrar.go missing public (no-auth) mount %q", publicMount[fw])
			}
		})
	}
}

// TestClaudeMDGenerated pins the generated CLAUDE.md: it must render for every
// transport, inherit the working principles, and carry transport-specific
// guidance (the matrix only proves the template parses; this asserts content).
func TestClaudeMDGenerated(t *testing.T) {
	t.Parallel()

	t.Run("http_wire", func(t *testing.T) {
		t.Parallel()
		src := renderClaudeMD(t, httpMatrixConfig("fiber", "postgres", "wire"))
		mustContainAll(t, src,
			"## Working principles",
			"## Architecture",
			"## Conventions",
			"matrixtest", // ModuleName threaded through
			"dependency rule",
			"`wire_gen.go`",                  // wire-specific verification loop
			"provideRegistrars",              // HTTP-specific routing guidance
			"TxManager.WithinTx",             // transaction convention (SQL project)
			"**sqlfluff** (postgres dialect", // SQL-lint convention, parameterized dialect
			"markdownlint",                   // markdown convention (always present)
			// Pins the by-design exclusions .sqlfluff encodes
			// (exclude_rules = RF04,AM04,PG01). RF02/RF06/AL03 appear too, as
			// enforced rules — not exclusions.
			"Three rules are excluded by design",
			"**PG01**",
		)
		mustNotContain(t, src, "fx module") // wire project must not leak the fx variant
	})

	// no_sql proves the transaction + SQL-lint bullets are gated out when the
	// project has no SQL backend, while the markdown convention still renders.
	t.Run("no_sql", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.Database = "none"
		cfg.QueryGen = "none"
		src := renderClaudeMD(t, cfg)
		mustContainAll(t, src, "markdownlint", "repository/memory")                 // always present; no-DB adapter
		mustNotContain(t, src, "TxManager.WithinTx", "sqlfluff", "repository/none") // gated on a SQL/sqlc backend
	})

	t.Run("worker_fx", func(t *testing.T) {
		t.Parallel()
		src := renderClaudeMD(t, workerMatrixConfig("postgres", "kafka", "fx"))
		mustContainAll(t, src,
			"kafka consumer worker", // worker project type line
			"fx module",             // fx-specific verification loop
			"provideWorkerHandlers", // worker-specific DI guidance
		)
		mustNotContain(t, src, "provideRegistrars") // worker must not leak HTTP routing
	})
}

// renderClaudeMD generates a project from cfg and returns its CLAUDE.md text.
func renderClaudeMD(t *testing.T, cfg *config.ProjectConfig) string {
	t.Helper()
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}
	return readFile(t, filepath.Join(dir, "CLAUDE.md"))
}

// composeFile is the subset of docker-compose.yaml these tests assert on.
type composeFile struct {
	// Version pins the absence of the obsolete `version:` key — Compose V2
	// warns on it.
	Version  string                    `yaml:"version"`
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeService struct {
	Image       string         `yaml:"image"`
	Ports       []string       `yaml:"ports"`
	Environment map[string]any `yaml:"environment"`
	DependsOn   map[string]struct {
		Condition string `yaml:"condition"`
	} `yaml:"depends_on"`
	Deploy struct {
		Resources struct {
			Limits struct {
				CPUs   string `yaml:"cpus"`
				Memory string `yaml:"memory"`
			} `yaml:"limits"`
		} `yaml:"resources"`
	} `yaml:"deploy"`
}

// TestDockerComposeStack pins the generated docker-compose.yaml: it must render
// exactly the dependency services the stack selects, point the app at their
// in-network hostnames (not the localhost values .env carries), gate the app on
// each dependency's healthcheck, and cap every service with a CPU/memory limit
// so a benchmark run is reproducible.
func TestDockerComposeStack(t *testing.T) {
	t.Parallel()

	t.Run("http_postgres_redis_elasticsearch", func(t *testing.T) {
		t.Parallel()
		c := loadCompose(t, httpMatrixConfig("fiber", "postgres", "wire"))

		assertComposeServices(t, c,
			[]string{"app", "postgres", "redis", "elasticsearch"},
			[]string{"mysql", "kafka", "rabbitmq"})
		assertComposeEnv(t, c, map[string]string{
			"DB_HOST":                 "postgres",
			"DB_PORT":                 "5432",
			"REDIS_ADDRS":             "redis:6379",
			"ELASTICSEARCH_ADDRESSES": "http://elasticsearch:9200",
		})
		assertHealthyDeps(t, c, "postgres", "redis", "elasticsearch")
		assertComposeVolumes(t, c, "postgres_data", "redis_data", "elasticsearch_data")

		// HTTP transport publishes the app port; the worker case must not.
		if len(c.Services["app"].Ports) != 1 {
			t.Errorf("app ports = %v, want exactly one published port", c.Services["app"].Ports)
		}
	})

	t.Run("worker_mysql_rabbitmq", func(t *testing.T) {
		t.Parallel()
		c := loadCompose(t, workerMatrixConfig("mysql", "rabbitmq", "wire"))

		assertComposeServices(t, c,
			[]string{"app", "mysql", "rabbitmq"},
			[]string{"postgres", "redis", "elasticsearch", "kafka"})
		assertComposeEnv(t, c, map[string]string{
			"DB_HOST": "mysql",
			"DB_PORT": "3306",
		})
		assertHealthyDeps(t, c, "mysql", "rabbitmq")
		assertComposeVolumes(t, c, "mysql_data", "rabbitmq_data")

		if url, _ := c.Services["app"].Environment["RABBITMQ_URL"].(string); !strings.Contains(url, "@rabbitmq:5672") {
			t.Errorf("app RABBITMQ_URL = %q, want the in-network rabbitmq host", url)
		}
		if ports := c.Services["app"].Ports; len(ports) != 0 {
			t.Errorf("worker app publishes ports %v, want none", ports)
		}
	})

	t.Run("kafka_broker", func(t *testing.T) {
		t.Parallel()
		c := loadCompose(t, workerMatrixConfig("postgres", "kafka", "wire"))

		assertComposeServices(t, c, []string{"app", "postgres", "kafka"}, []string{"rabbitmq"})
		assertComposeEnv(t, c, map[string]string{"KAFKA_BROKERS": "kafka:29092"})
		// The host listener is the address .env.example and base.yaml use.
		if ports := c.Services["kafka"].Ports; len(ports) != 1 || ports[0] != "9092:9092" {
			t.Errorf("kafka publishes %v, want [9092:9092] for host runs", ports)
		}
		// Kafka 3.9 refuses to start when a listener bound to 0.0.0.0 would be
		// advertised, and the healthcheck must survive a remapped host port.
		raw := readFile(t, filepath.Join(renderProject(t, workerMatrixConfig("postgres", "kafka", "wire")),
			"docker-compose.yaml"))
		mustNotContain(t, raw, "://0.0.0.0:")
		mustContainAll(t, raw, "--bootstrap-server localhost:29092")
		assertHealthyDeps(t, c, "postgres", "kafka")
	})

	// IncludeDocker=false must emit neither half of the Docker setup.
	t.Run("docker_disabled", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.IncludeDocker = false
		dir := renderProject(t, cfg)
		for _, name := range []string{"docker-compose.yaml", "Dockerfile"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("%s rendered with IncludeDocker=false", name)
			}
		}
	})
}

// TestPreCommitHookExecutable pins the git hook: `make setup` points
// core.hooksPath at .githooks, and git silently skips a hook that isn't
// executable — so the file must be both rendered and +x.
func TestPreCommitHookExecutable(t *testing.T) {
	t.Parallel()
	dir := renderProject(t, httpMatrixConfig("fiber", "postgres", "wire"))
	path := filepath.Join(dir, ".githooks/pre-commit")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat .githooks/pre-commit: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf(".githooks/pre-commit mode = %v, want the owner exec bit set", info.Mode().Perm())
	}
	mustContainAll(t, readFile(t, path), "make fmt", "make lint", "go build")

	// The hook is dead weight unless the Makefile wires core.hooksPath to it.
	mustContainAll(t, readFile(t, filepath.Join(dir, "Makefile")), "core.hooksPath .githooks")
}

// loadCompose renders cfg and unmarshals the generated docker-compose.yaml.
func loadCompose(t *testing.T, cfg *config.ProjectConfig) composeFile {
	t.Helper()
	dir := renderProject(t, cfg)
	var c composeFile
	if err := yamlv3.Unmarshal([]byte(readFile(t, filepath.Join(dir, "docker-compose.yaml"))), &c); err != nil {
		t.Fatalf("unmarshal docker-compose.yaml: %v", err)
	}
	if c.Version != "" {
		t.Errorf("docker-compose.yaml declares obsolete version %q", c.Version)
	}
	return c
}

// assertComposeServices checks the exact service set and that each one carries
// a CPU + memory limit.
func assertComposeServices(t *testing.T, c composeFile, want, unwanted []string) {
	t.Helper()
	for _, name := range want {
		svc, ok := c.Services[name]
		if !ok {
			t.Errorf("missing service %q", name)
			continue
		}
		limits := svc.Deploy.Resources.Limits
		if limits.CPUs == "" || limits.Memory == "" {
			t.Errorf("service %q has no cpu/memory limit (%+v)", name, limits)
		}
	}
	for _, name := range unwanted {
		if _, ok := c.Services[name]; ok {
			t.Errorf("unexpected service %q for this stack", name)
		}
	}
}

// assertComposeEnv checks the app's endpoint overrides — .env points at
// localhost, so these must name the compose services instead.
func assertComposeEnv(t *testing.T, c composeFile, want map[string]string) {
	t.Helper()
	env := c.Services["app"].Environment
	for k, v := range want {
		if got := fmt.Sprint(env[k]); got != v {
			t.Errorf("app env %s = %q, want %q", k, got, v)
		}
	}
}

func assertHealthyDeps(t *testing.T, c composeFile, deps ...string) {
	t.Helper()
	got := c.Services["app"].DependsOn
	if len(got) != len(deps) {
		t.Errorf("app depends_on = %v, want %v", got, deps)
	}
	for _, dep := range deps {
		if got[dep].Condition != "service_healthy" {
			t.Errorf("app depends_on[%s].condition = %q, want service_healthy", dep, got[dep].Condition)
		}
	}
}

func assertComposeVolumes(t *testing.T, c composeFile, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, ok := c.Volumes[name]; !ok {
			t.Errorf("missing named volume %q", name)
		}
	}
}

// renderProject generates cfg into a temp dir and returns the project root.
func renderProject(t *testing.T, cfg *config.ProjectConfig) string {
	t.Helper()
	dir := t.TempDir()
	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gen.Out = io.Discard
	if genErr := gen.Generate(dir); genErr != nil {
		t.Fatalf("Generate: %v", genErr)
	}
	return dir
}

// mustContainAll fails the (sub)test for every substring missing from src.
func mustContainAll(t *testing.T, src string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(src, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

// mustNotContain fails the (sub)test for any substring that leaks into src.
func mustNotContain(t *testing.T, src string, banned ...string) {
	t.Helper()
	for _, b := range banned {
		if strings.Contains(src, b) {
			t.Errorf("CLAUDE.md unexpectedly contains %q", b)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func runGo(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	// -p=2 caps each child go command's compiler workers; with -parallel 2 the
	// matrix stays near 2.5 GB instead of 8 cases x 8 compilers swapping the machine.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("go %s output:\n%s", strings.Join(args, " "), out)
	}
	return err
}

// TestRenderedGoIsGofmtClean renders representative configs and asserts every
// .go file is already in gofmt form. Conditional template lines otherwise
// drift struct alignment and import order, and the generated project then
// fails its own `make lint` on the first commit.
func TestRenderedGoIsGofmtClean(t *testing.T) {
	t.Parallel()
	cfgs := []*config.ProjectConfig{
		httpMatrixConfig("fiber", "postgres", "wire"),
		httpMatrixConfig("gin", "mysql", "fx"),
		workerMatrixConfig("postgres", "kafka", "wire"),
		workerMatrixConfig("mysql", "rabbitmq", "fx"),
	}
	for _, cfg := range cfgs {
		dir := renderProject(t, cfg)
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			want, fmtErr := format.Source(src)
			if fmtErr != nil {
				return fmt.Errorf("%s: %w", path, fmtErr)
			}
			if !bytes.Equal(src, want) {
				rel, _ := filepath.Rel(dir, path)
				t.Errorf("%s/%s/%s/%s: %s is not gofmt-clean",
					cfg.Transport, cfg.HTTPFramework, cfg.Database, cfg.DI, rel)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}
}

// TestGeneratedProjectBootstrap runs the real first-run sequence a user
// follows on representative projects. The vet matrix stubs dbgen and
// wire_gen, so only this test can catch engine-specific DDL that sqlc
// rejects or an injector wire cannot build. It is sequential on purpose
// (each step peaks near 1 GB) and skips when a tool is missing, so a CI
// box without sqlc/wire still passes.
func TestGeneratedProjectBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-toolchain bootstrap in -short mode")
	}
	for _, tool := range []string{"go", "sqlc", "wire"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	// Search stays off here: golangci-lint's analyzers crawl the typed
	// go-elasticsearch client for minutes per project. The vet matrix still
	// renders and vets that adapter.
	noSearch := func(cfg *config.ProjectConfig) *config.ProjectConfig {
		cfg.Search = "none"
		return cfg
	}
	// The stacks nova's own `make gen-api`, `gen-worker` and `gen-grpc`
	// produce.
	with := func(cfg *config.ProjectConfig, cache, queue string) *config.ProjectConfig {
		cfg.Cache, cfg.MessageQueue = cache, queue
		return cfg
	}
	cases := []struct {
		name string
		cfg  *config.ProjectConfig
	}{
		{"http_fiber_postgres_wire", noSearch(with(httpMatrixConfig("fiber", "postgres", "wire"), "redis", "kafka"))},
		{"http_gin_mysql_wire", noSearch(httpMatrixConfig("gin", "mysql", "wire"))},
		{"worker_postgres_kafka_wire", with(workerMatrixConfig("postgres", "kafka", "wire"), "redis", "kafka")},
		// RabbitMQ, MySQL and fx had no real toolchain run; this also runs
		// sqlc on the MySQL upsert and executes the RabbitMQ consumer tests.
		{"worker_mysql_rabbitmq_fx", workerMatrixConfig("mysql", "rabbitmq", "fx")},
		{"http_echo_none_fx", noDBMatrixConfig("echo", "fx")},
		// chi carries the most hand-rolled transport code (client IP, CORS,
		// error handlers), so it gets a real lint run too.
		{"http_chi_postgres_fx", noSearch(httpMatrixConfig("chi", "postgres", "fx"))},
		// gRPC: real buf + wire over sqlc, and the fx graph booted for real
		// against the in-memory repository (the smoke test dials the port).
		{"grpc_postgres_wire", with(grpcMatrixConfig("wire"), "redis", "kafka")},
		{"grpc_none_fx", noDBGRPCConfig("fx")},
	}
	for _, tc := range cases {
		// No t.Parallel: each case compiles a full dependency tree.
		t.Run(tc.name, func(t *testing.T) { bootstrapProject(t, tc.cfg) })
	}
}

func bootstrapProject(t *testing.T, cfg *config.ProjectConfig) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	if _, err := exec.LookPath("buf"); cfg.HasGRPC() && err != nil {
		t.Skip("buf not on PATH")
	}
	dir := renderProject(t, cfg)
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2", "GOGC=50")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}
	// The generated Makefile owns the bootstrap order (sqlc → tidy → wire); a
	// user's first run is exactly this.
	run("make", "gen")
	if cfg.UseWire() {
		assertTracerBuiltFirst(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire_gen.go")))
	}
	run("go", "build", "./...")
	// Rendered unit tests (errors, usecase with fakes, adapter translation,
	// worker policy and settle): run them so they are proven, not just compiled.
	pkgs := []string{
		"./pkg/...",
		"./internal/usecase/...",
		"./internal/adapter/...",
		"./internal/infrastructure/config/...",
	}
	if cfg.HasWorker() {
		pkgs = append(pkgs, "./internal/transport/worker/...")
	}
	if cfg.HasGRPC() {
		pkgs = append(pkgs, "./internal/transport/grpc/...")
	}
	if cfg.HasHTTP() {
		pkgs = append(pkgs, "./internal/transport/http/...")
	}
	run("go", append([]string{"test"}, pkgs...)...)
	if !cfg.HasDatabase() && (cfg.HasHTTP() || cfg.HasGRPC()) {
		smokeBoot(t, dir, cfg)
	}
	// The generated project ships a strict golangci config plus a pre-commit
	// hook and CI job that run it, so a fresh render must already be clean.
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		run("golangci-lint", "run")
	} else {
		t.Log("golangci-lint not on PATH: lint step skipped")
	}
}

// TestUsersMigrationPerEngine pins the users DDL to the engine. sqlc derives
// the schema from this file, so Postgres-only syntax in a mysql project
// means dbgen is never generated and the project cannot build.
func TestUsersMigrationPerEngine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		db      string
		want    []string
		mustNot []string
	}{
		{
			"postgres",
			[]string{"BIGSERIAL", "TIMESTAMP WITH TIME ZONE"},
			[]string{"AUTO_INCREMENT", "DATETIME"},
		},
		{
			"mysql",
			[]string{"BIGINT AUTO_INCREMENT PRIMARY KEY", "DATETIME(6)"},
			[]string{"BIGSERIAL", "WITH TIME ZONE", "NOW()"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.db, func(t *testing.T) {
			t.Parallel()
			dir := renderProject(t, httpMatrixConfig("fiber", tc.db, "wire"))
			matches, err := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*_create_users_table.up.sql"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("expected one users migration, got %v (%v)", matches, err)
			}
			src := readFile(t, matches[0])
			mustContainAll(t, src, tc.want...)
			mustNotContain(t, src, tc.mustNot...)
		})
	}
}

// TestObservabilityContract pins the fields an on-call engineer relies on:
// every HTTP access line can answer who (user_id), where (route) and how
// long; inbound request ids are validated; tracing middleware is installed;
// readiness failures are logged; the logger renders errors and carries the
// service identity; the worker scopes broker position and correlation ids.
func TestObservabilityContract(t *testing.T) {
	t.Parallel()
	otelMiddleware := map[string]string{
		"fiber": "otelfiber.Middleware(", "gin": "otelgin.Middleware(",
		"chi": "otelchi.Middleware(", "echo": "otelecho.Middleware(",
	}
	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := renderProject(t, httpMatrixConfig(fw, "postgres", "wire"))
			mw := filepath.Join(dir, "internal/transport/http/middleware")
			mustContainAll(t, readFile(t, filepath.Join(mw, "logging.go")),
				`"request_id"`, `"trace_id"`, `"route"`, `"path"`, `"user_id"`, `"duration_ms"`, `"client_ip"`,
				"prepareAuthSlot(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "auth.go")), "rememberPrincipal(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "requestid.go")), "requestid.Valid(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "principal.go")), "func principalFromSlot(")
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/http/router.go")),
				"readiness check failed", otelMiddleware[fw], "func isProbe(")
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/logger/zerolog.go")),
				"AnErr(", "RFC3339Nano", `"service"`, `"version"`, `"env"`, `"config loaded"`)
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")),
				`level: "info"`)
			mustNotContain(t, readFile(t, filepath.Join(dir, "internal/app/api.go")), `"error",`)
		})
	}
	t.Run("worker", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, workerMatrixConfig("postgres", "kafka", "wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker.go")),
			"type Message struct", `"partition"`, `"offset"`, `"request_id"`, `"trace_id"`, `"duration_ms"`,
			"errors.LogFields(", "requestid.Valid(", "requestid.With(")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/consumer.go")), "group.Errors()")
		// A pure worker correlates through pkg/requestid and never pulls in the
		// HTTP envelope.
		mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/requestid/requestid.go")), "func Valid(")
		if _, err := os.Stat(filepath.Join(dir, "pkg/httputil")); !os.IsNotExist(err) {
			t.Error("pkg/httputil rendered for a worker-only project")
		}
	})
	t.Run("publisher", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		dir := renderProject(t, cfg)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/pubsub/publisher.go")),
			"correlationHeaders(", "propagation.MapCarrier", "sarama.StringEncoder(key)", "requestid.From(")
	})
	t.Run("grpc", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, grpcMatrixConfig("wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/interceptor/interceptor.go")),
			"func RequestID()", "func Logging(", "func Recovery()", `"duration_ms"`, "requestid.With(")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/server.go")),
			"otelgrpc.NewServerHandler()", "grpc.ChainUnaryInterceptor(")
		mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/grpc.go")), "RunE:")
	})
}

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
	cors := readFile(t, filepath.Join(mw, "cors.go"))
	logging := readFile(t, filepath.Join(mw, "logging.go"))
	writer := readFile(t, filepath.Join(tr, "httpwriter/writer.go"))
	registrar := readFile(t, filepath.Join(tr, "v1/user/registrar.go"))
	pkgErrors := readFile(t, filepath.Join(dir, "pkg/errors/errors.go"))

	// Client IP: X-Forwarded-For is honoured only from configured proxies.
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go")),
		`"trusted_proxies"`, `"request_timeout"`, "func (c HTTPConfig) TrustedProxyNets()", "http.trusted_proxies")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/base.yaml")),
		"trusted_proxies: []", "request_timeout:")
	switch fw {
	case "fiber":
		mustContainAll(t, router, "middleware.ClientIP(")
		mustNotContain(t, router, "ProxyHeader:", "EnableTrustedProxyCheck")
		mustContainAll(t, readFile(t, filepath.Join(mw, "clientip.go")), "func ClientIP(", "strings.Clone(")
		mustContainAll(t, limiter, "RemoteIP(c)")
	case "gin":
		mustContainAll(t, router, "SetTrustedProxies(")
	case "chi":
		mustContainAll(t, router, "middleware.ClientIP(")
		mustContainAll(t, readFile(t, filepath.Join(mw, "clientip.go")), "func ClientIP(", "X-Forwarded-For")
		mustNotContain(t, limiter, "X-Forwarded-For")
	case "echo":
		mustContainAll(t, router, "e.IPExtractor", "echo.ExtractIPDirect()", "echo.TrustPrivateNet(false)")
	}

	// CORS never widens to "*"; bodies and request time are bounded.
	mustContainAll(t, readFile(t, filepath.Join(mw, "timeout.go")), "func Timeout(", "context.WithTimeout(")
	mustContainAll(t, router, "middleware.Timeout(cfg.HTTP.RequestTimeout)")
	mustContainAll(t, pkgErrors, "context.DeadlineExceeded")
	if fw == "fiber" {
		mustContainAll(t, cors, "len(cfg.AllowOrigins) == 0")
		mustContainAll(t, router, "BodyLimit:")
	} else {
		mustContainAll(t, readFile(t, filepath.Join(mw, "bodylimit.go")), "func BodyLimit(", "http.MaxBytesReader(")
		mustContainAll(t, router, "middleware.BodyLimit(cfg.HTTP.BodyLimit)")
		mustContainAll(t, writer, "http.MaxBytesError", "locale.PayloadTooLarge")
	}
	switch fw {
	case "echo":
		mustContainAll(t, cors, "len(cfg.AllowOrigins) == 0")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/server/http.go")),
			"e.Server.ReadTimeout", "e.Server.WriteTimeout")
	case "gin", "chi":
		mustContainAll(t, cors, `"Vary"`)
	}

	// Framework 404/405/413 answer in the envelope; the logged status is the
	// one the client receives.
	mustContainAll(t, pkgErrors, "func FromStatus(status int) *AppError")
	switch fw {
	case "fiber":
		mustContainAll(t, router, "ErrorHandler:", "httpwriter.HandleError,")
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

	// Credential endpoints: RFC 6750 challenge, bounded limiter with
	// Retry-After on login and registration; ownership on mutations.
	mustContainAll(t, readFile(t, filepath.Join(mw, "auth.go")), "strings.EqualFold(", "WWW-Authenticate")
	mustContainAll(t, limiter, "Retry-After", "visitorTTL",
		"func (l *ipLimiter) allow(ip string) (bool, time.Duration)")
	if n := strings.Count(registrar, "middleware.LoginRateLimit("); n != 2 {
		t.Errorf("registrar applies LoginRateLimit %d times, want 2 (login and registration)", n)
	}
	usecase := readFile(t, filepath.Join(dir, "internal/usecase/user/service.go"))
	mustContainAll(t, usecase, "func requireSelf(", "security.MaxPasswordLen", "func (s *Service) timingHash()")
	mustNotContain(t, usecase, "dummyHashOnce")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/security/hasher.go")), "MaxPasswordLen = 72")

	// Readiness names the failing check without its error text; the OpenAPI
	// document matches the handlers.
	mustContainAll(t, readFile(t, filepath.Join(tr, "health/checker.go")),
		"func (c *Checker) Readiness(ctx context.Context) (Status, error)", `"fail"`)
	mustContainAll(t, router, `log.Warn("readiness check failed", "err", err)`)
	openapi := readFile(t, filepath.Join(dir, "api/openapi/openapi.yaml"))
	mustContainAll(t, openapi, "/api/v1/public/users:", "name: perPage", "name: cursor", "requestId:", "Retry-After:",
		"WWW-Authenticate:", "PageMeta:", "CursorMeta:", `"413":`)
	mustNotContain(t, openapi, "name: offset")
}

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

	policy := readFile(t, filepath.Join(dir, "internal/transport/worker/policy.go"))
	mustContainAll(t, policy, "func Poison(", "ErrPoison", "ErrParked", "ErrConsumerClosed", "ErrHandlerPanic",
		"recover()", "context.WithTimeout(", "withDefaults()")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker.go")),
		"policy Policy", `"attempts"`, "worker: message parked", "worker: message abandoned")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker_test.go")),
		"func TestDispatchOutcomes(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/v1/user/handler.go")),
		"worker.Poison(", "msg.EventID")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/provider.go")),
		"func provideWorkerPolicy(")

	consumer := readFile(t, filepath.Join(dir, "internal/transport/worker/consumer.go"))
	mustContainAll(t, consumer, "ErrConsumerClosed", "errors.Is(err, ErrParked)")
	consumerTest := readFile(t, filepath.Join(dir, "internal/transport/worker/consumer_test.go"))

	appSrc := readFile(t, filepath.Join(dir, "internal/app/worker.go"))
	mustContainAll(t, appSrc, "drainTimeout", "errConsumerStopped", "consumer stopped before shutdown")

	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/user_audit.go")),
		"EventID", "ErrEventAlreadyRecorded")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/usecase/useraudit/service.go")),
		"domain.ErrEventAlreadyRecorded", "duplicate delivery ignored")
	migrations, _ := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*_create_user_audit_log.up.sql"))
	if len(migrations) != 1 {
		t.Fatalf("expected one audit migration, got %v", migrations)
	}
	mustContainAll(t, readFile(t, migrations[0]),
		"event_id VARCHAR(64) NOT NULL", "CREATE UNIQUE INDEX uq_user_audit_log_event_id")
	query := readFile(t, filepath.Join(dir, "sqlc/query/user_audit.sql"))
	mustContainAll(t, query, ":execrows", "event_id")

	if broker == "kafka" {
		mustContainAll(t, consumer, "producer sarama.SyncProducer", "deadLetterSuffix", `sess.MarkMessage(m, "")`,
			"HeaderDeadLetterReason", "HeaderDeadLetterSource")
		mustContainAll(t, consumerTest, "func TestKafkaSettle(", "mocks.NewSyncProducer(")
		mustContainAll(t, query, "ON CONFLICT (event_id) DO NOTHING") // postgres
		return
	}
	mustContainAll(t, cfgSrc, "Queue    string", "Prefetch int")
	mustContainAll(t, pub,
		"cfg.Exchange", "amqp.Persistent", "PublishWithDeferredConfirmWithContext(", "ch.Confirm(false)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/pubsub/rabbitmq.go")),
		"func NewRabbitMQConnection(cfg config.RabbitMQConfig, log *logger.Logger) (*amqp.Connection, func(), error)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx_provider.go")),
		"func provideRabbitMQConnection(", "func providePublisher(")
	mustContainAll(t, consumer, `"x-dead-letter-exchange"`, ".Qos(", "NotifyClose(", "d.Nack(false, false)",
		"d.Nack(false, true)", "cfg.Queue", "cfg.Prefetch")
	mustContainAll(t, consumerTest, "func TestRabbitMQHandle(")
	mustContainAll(t, query, "ON DUPLICATE KEY UPDATE id = id") // mysql
}

// TestDependencyRule walks the rendered domain and usecase packages and fails
// on any import that points outward: domain may import only the standard
// library and other domain packages, usecase additionally pkg/. The vet matrix
// cannot catch this (an outward import compiles fine), so it is pinned here.
func TestDependencyRule(t *testing.T) {
	t.Parallel()
	cfg := httpMatrixConfig("fiber", "postgres", "wire")
	cfg.MessageQueue = "kafka"
	for _, c := range []*config.ProjectConfig{cfg, workerMatrixConfig("mysql", "rabbitmq", "fx")} {
		dir := renderProject(t, c)
		assertInwardImports(t, dir, "internal/domain", "internal/domain")
		assertInwardImports(t, dir, "internal/usecase", "internal/domain", "pkg")
	}
}

// assertInwardImports checks every .go file under root: an import of the
// project's own module must be one of the allowed packages or a subpackage.
func assertInwardImports(t *testing.T, dir, root string, allowed ...string) {
	t.Helper()
	const module = "matrixtest/"
	walkErr := filepath.WalkDir(filepath.Join(dir, root), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, module) {
				continue
			}
			ok := false
			for _, a := range allowed {
				ok = ok || p == module+a || strings.HasPrefix(p, module+a+"/")
			}
			if !ok {
				rel, _ := filepath.Rel(dir, path)
				t.Errorf("%s imports %s: %s may only import %v", rel, p, root, allowed)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}

// TestLayersContract pins the phase 6 contract: the usecase depends on ports
// only, Wrap cannot return a typed nil, transactions and unique-violation
// translation guard Register, the entity owns its invariants, and the
// infrastructure degrades instead of blocking.
func TestLayersContract(t *testing.T) {
	t.Parallel()
	t.Run("postgres_wire", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		assertLayersContract(t, renderProject(t, cfg), "postgres")
	})
	t.Run("mysql_fx", func(t *testing.T) {
		t.Parallel()
		assertLayersContract(t, renderProject(t, httpMatrixConfig("gin", "mysql", "fx")), "mysql")
	})
	t.Run("memory", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, noDBMatrixConfig("echo", "wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/memory/tx_manager.go")),
			"func NewTxManager() *TxManager")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go")),
			"memoryrepo.NewTxManager", "wire.Bind(new(domain.TxManager), new(*memoryrepo.TxManager))")
	})
}

func assertLayersContract(t *testing.T, dir, engine string) {
	t.Helper()
	assertUsecaseSeams(t, dir, engine)
	assertStorageSeams(t, dir, engine)
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/errors/errors.go")),
		"func Wrap(err error, message string) error", "func Wrapf(err error, format string, args ...any) error")
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/errors/errors_test.go")), "func TestWrapNilIsNil(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/locale/locale.go")), "sync.Once")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/cache/redis.go")), "starting degraded")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/redis/user_cache.go")),
		"userCacheJitter", "invalidate failed")
}

// assertUsecaseSeams: the usecase sees ports only, the token port is bound
// in DI, the JWT service is on v5, and the principal carries identity only.
func assertUsecaseSeams(t *testing.T, dir, engine string) {
	t.Helper()
	svc := readFile(t, filepath.Join(dir, "internal/usecase/user/service.go"))
	mustContainAll(t, svc, "security.TokenIssuer", "domain.TxManager", "entity.NewUser(", "user.Rename(",
		"errors.ErrAlreadyExists", "s.tx.WithinTx(")
	mustNotContain(t, svc, "jwtsec", "infrastructure/")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/usecase/user/service_test.go")),
		"func TestRegisterDuplicateEmailIs409(", "func TestUpdateReportsNewUpdatedAt(",
		"func TestGetMissingUserIsUserNotFound(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/security/token.go")),
		"type TokenIssuer interface")
	jwtSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/jwt/service.go"))
	mustContainAll(t, jwtSrc, "var _ security.TokenIssuer = (*TokenService)(nil)", "github.com/golang-jwt/jwt/v5",
		"jwt.WithIssuer(", "jwt.ErrTokenSignatureInvalid")
	mustNotContain(t, jwtSrc, "ValidationError")
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/domain/identity/principal.go")),
		"SessionID", "DeviceID", "Platform", "Phone")
	if engine == "postgres" {
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go")),
			"wire.Bind(new(domainsec.TokenIssuer), new(*jwtsec.TokenService))",
			"postgresrepo.NewTxManager", "wire.Bind(new(domain.TxManager), new(*postgresrepo.TxManager))")
		return
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx_provider.go")),
		"func provideTokenIssuer(ts *jwtsec.TokenService) domainsec.TokenIssuer")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go")),
		"provideTokenIssuer,", "fx.Annotate(mysqlrepo.NewTxManager, fx.As(new(domain.TxManager)))")
}

// assertStorageSeams: the adapter translates the unique violation, the entity
// owns its invariants, and the update query writes the entity's timestamp.
func assertStorageSeams(t *testing.T, dir, engine string) {
	t.Helper()
	repoDir := filepath.Join(dir, "internal/adapter/repository", engine)
	mustContainAll(t, readFile(t, filepath.Join(repoDir, "user_repository.go")), "translate(err)")
	mustContainAll(t, readFile(t, filepath.Join(repoDir, "errors.go")),
		"func translate(err error) error", "errors.ErrAlreadyExists")
	mustContainAll(t, readFile(t, filepath.Join(repoDir, "errors_test.go")), "func TestTranslateUniqueViolation(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/entity/user.go")),
		"func NewUser(email, name, passwordHash string) (*User, error)", "func (u *User) Rename(name string) error",
		"ErrEmailInvalid", "ErrNameInvalid")
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/domain/user.go")), "Name   string")
	query := readFile(t, filepath.Join(dir, "sqlc/query/user.sql"))
	if engine == "postgres" {
		mustContainAll(t, query, "UPDATE users SET name = $1, updated_at = $2")
		return
	}
	mustContainAll(t, query, "UPDATE users SET name = ?, updated_at = ?")
}

// smokeBoot proves the host boot path on the no-dependency project: build the
// binary, start it with a generated keypair and a free port the way the
// README does (.env in cwd), wait for /healthz, then SIGTERM it and expect a
// clean exit. It runs only for the no-database bootstrap, so no compose
// dependency is needed.
func smokeBoot(t *testing.T, dir string, cfg *config.ProjectConfig) {
	t.Helper()
	bin := filepath.Join(dir, "smoke-bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	pub, priv := testKeyPair(t)
	port := freePort(t)
	sub, portVar, startLine := "api", "SERVER_PORT", "starting HTTP server"
	if cfg.HasGRPC() {
		sub, portVar, startLine = "grpc", "GRPC_PORT", "starting gRPC server"
	}
	envFile := "APP_ENV=local\n" + portVar + "=" + port + "\nJWT_PUBLIC_KEY=" + pub + "\nJWT_PRIVATE_KEY=" + priv + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, sub)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	healthy := false
	for i := 0; i < 50 && !healthy; i++ {
		time.Sleep(100 * time.Millisecond)
		healthy = probe(port, cfg.HasGRPC())
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitErr := cmd.Wait()
	if !healthy {
		t.Fatalf("port %s never answered\n%s", port, output.String())
	}
	if waitErr != nil {
		t.Fatalf("exit after SIGTERM: %v\n%s", waitErr, output.String())
	}
	mustContainAll(t, output.String(), startLine, "shutdown complete")
}

// probe is the boot signal: HTTP needs a 200 from /healthz; for gRPC a TCP
// accept on the port is enough (the rendered tests cover the health RPC).
func probe(port string, grpc bool) bool {
	if grpc {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func testKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return base64.StdEncoding.EncodeToString(pubPEM), base64.StdEncoding.EncodeToString(privPEM)
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// TestDeployContract pins the phase 7 hygiene: host runs load .env, APP_ENV is
// whitelisted, MySQL is configured like Postgres, the image is reproducible
// and port-driven, CI is pinned and hermetic, shutdown budgets are config,
// and the OpenAPI document is served.
func TestDeployContract(t *testing.T) {
	t.Parallel()
	t.Run("postgres_wire", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		cfg.IncludeCI = true
		assertDeployContract(t, renderProject(t, cfg), "postgres")
	})
	t.Run("mysql_fx", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("gin", "mysql", "fx")
		cfg.IncludeCI = true
		assertDeployContract(t, renderProject(t, cfg), "mysql")
	})
}

func assertDeployContract(t *testing.T, dir, engine string) {
	t.Helper()
	cfgSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go"))
	mustContainAll(t, cfgSrc, "godotenv.Read(", "unknown APP_ENV", "DB_USER) is required", "normalizeLists(&cfg)")
	// The top-level budget is fx's Stop deadline; Wire's cleanup has none.
	baseYAML := readFile(t, filepath.Join(dir, "internal/infrastructure/config/base.yaml"))
	if engine == "postgres" { // wire
		mustNotContain(t, cfgSrc, `"shutdown_timeout" env:"APP_SHUTDOWN_TIMEOUT"`)
	} else {
		mustContainAll(t, cfgSrc, `"shutdown_timeout" env:"APP_SHUTDOWN_TIMEOUT"`)
		mustContainAll(t, baseYAML, "shutdown_timeout: 20s")
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/root.go")), "SilenceUsage: true")
	env := readFile(t, filepath.Join(dir, ".env.example"))
	mustNotContain(t, env, "<set-me", "<user>", "<password>")
	mustContainAll(t, env, "DB_USER=matrixtest", "JWT_PUBLIC_KEY=")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/cache/redis.go")), "addrs is empty")
	if engine == "mysql" {
		mustContainAll(t, cfgSrc, `"tls"`, `"max_conn_lifetime"`, `"max_conn_idle_time"`, `"connect_timeout"`)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/database/mysql.go")),
			"TLSConfig", "SetConnMaxLifetime(", "SetConnMaxIdleTime(", "cfg.ConnectTimeout")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")),
			`tls: "skip-verify"`)
	}
	assertImageAndCI(t, dir, engine)
	mustContainAll(t, readFile(t, filepath.Join(dir, "api/openapi/openapi.go")),
		"//go:embed openapi.yaml", "var Spec []byte")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/http/router.go")),
		`"/openapi.yaml"`, "openapi.Spec")
	mustContainAll(t, readFile(t, filepath.Join(dir, "README.md")), "sqlc v1.30.0", "migrate v4.19.0", "sqlfluff 4.2.2")
}

// assertImageAndCI: the Dockerfile, the dockerignore, CI, the Makefile and
// the DI engine's shutdown budget.
func assertImageAndCI(t *testing.T, dir, engine string) {
	t.Helper()
	docker := readFile(t, filepath.Join(dir, "Dockerfile"))
	mustContainAll(t, docker, "alpine:3.22", "-trimpath", "ARG SERVER_PORT=8080", "${SERVER_PORT}/readyz")
	mustNotContain(t, docker, "/app/config", "alpine:3.19")
	mustContainAll(t, readFile(t, filepath.Join(dir, ".dockerignore")), ".env", ".git", "bin/")
	ci := readFile(t, filepath.Join(dir, ".github/workflows/ci.yaml"))
	mustContainAll(t, ci, "golangci/golangci-lint-action@v8", "version: v2.11.4", "-trimpath")
	mustNotContain(t, ci, "services:", "JWT_PUBLIC_KEY", "DB_HOST")
	mk := readFile(t, filepath.Join(dir, "Makefile"))
	mustContainAll(
		t,
		mk,
		"down $(n)",
		"n ?= 1",
		"-database '$(migrate_url)'",
		"sed -n 's/^MIGRATE_DATABASE_URL=//p' .env",
	)
	assertMakefileHygiene(t, mk)
	assertEnvSourceable(t, dir)
	for _, f := range []string{".gitignore", ".dockerignore"} {
		mustContainAll(t, readFile(t, filepath.Join(dir, f)), "*.key", "jwt.pub")
	}
	if engine == "postgres" { // wire
		mustContainAll(t, mk, "go tool wire ./internal/infrastructure/di")
		mustContainAll(t, readFile(t, filepath.Join(dir, "go.mod")), "tool github.com/google/wire/cmd/wire")
		return
	}
	fxSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go"))
	mustContainAll(t, fxSrc, "cfg.ShutdownTimeout", `"timeout_ms"`)
	mustNotContain(t, fxSrc, "stopTimeout")
}

// TestGRPCContract pins the phase 8 gRPC transport: a proto contract with
// buf-driven, version-pinned codegen in make gen, a server with health,
// reflection and bearer auth, HTTP-equivalent error classification, and a
// DI graph that reaches the usecase in both engines.
func TestGRPCContract(t *testing.T) {
	t.Parallel()
	t.Run("postgres_wire", func(t *testing.T) {
		t.Parallel()
		assertGRPCContract(t, renderProject(t, grpcMatrixConfig("wire")), true)
	})
	t.Run("none_fx", func(t *testing.T) {
		t.Parallel()
		assertGRPCContract(t, renderProject(t, noDBGRPCConfig("fx")), false)
	})
}

func assertGRPCContract(t *testing.T, dir string, wire bool) {
	t.Helper()
	mustContainAll(t, readFile(t, filepath.Join(dir, "api/proto/user/v1/user.proto")),
		"package user.v1;", `option go_package = "matrixtest/api/gen/user/v1;userv1";`, "service UserService {",
		"rpc CreateUser(", "rpc Login(", "rpc GetUser(", "rpc UpdateUser(", "rpc DeleteUser(",
		"rpc ListUsers(", "rpc ListPublicUsers(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "buf.gen.yaml")),
		"protoc-gen-go@v1.36.12", "protoc-gen-go-grpc@v1.6.2", "paths=source_relative")
	mustContainAll(t, readFile(t, filepath.Join(dir, "buf.yaml")), "path: api/proto", "STANDARD")
	mk := readFile(t, filepath.Join(dir, "Makefile"))
	mustContainAll(t, mk, "rm -rf $(CURDIR)/api/gen; buf generate", "&& buf lint", "&& buf format -w")
	if wire {
		mustContainAll(t, mk, "gen: sqlc-gen proto-gen tidy wire-gen")
	} else {
		mustContainAll(t, mk, "gen: proto-gen tidy")
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "README.md")), "buf v1.70.0", "go run main.go grpc", "grpcurl")
	assertGRPCTransport(t, dir, wire)
}

// assertGRPCTransport: the server factory, the user service, the status
// mapping, the interceptors, the lifecycle wrapper, config and DI.
func assertGRPCTransport(t *testing.T, dir string, wire bool) {
	t.Helper()
	srv := readFile(t, filepath.Join(dir, "internal/transport/grpc/server.go"))
	mustContainAll(t, srv, "healthpb.RegisterHealthServer(", "reflection.Register(", "otelgrpc.NewServerHandler()",
		"interceptor.RequestID(),", "interceptor.Locale(),", "interceptor.Logging(log),", "interceptor.Recovery(),",
		"interceptor.Deadline(cfg.RequestTimeout),", "interceptor.Throttle(cfg.LoginRateLimit, throttled),",
		"interceptor.Auth(tokenSvc, public),", "healthpb.Health_Check_FullMethodName")
	handler := readFile(t, filepath.Join(dir, "internal/transport/grpc/user.go"))
	mustContainAll(t, handler, "userv1.UnimplementedUserServiceServer", "func (*UserHandler) Public()",
		"func (*UserHandler) Throttled()", "userv1.UserService_Login_FullMethodName", "rpcstatus.FromError(ctx, err)")
	mustNotContain(t, handler, "TODO", "placeholder")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/user_test.go")),
		"bufconn.Listen(", "func TestUserServiceEndToEnd(", "func TestHealthFollowsLifecycle(",
		"func TestEveryRPCIsProtectedUnlessPublic(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/interceptor/interceptor_test.go")),
		"func TestRecoveryAnswersLocalized(", "func TestThrottlePerPeer(", "func TestDeadlineBoundsCall(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/rpcstatus/status.go")),
		"codes.Unauthenticated", "codes.PermissionDenied", "codes.AlreadyExists", "errors.StashAppError(ctx, app)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/interceptor/auth.go")),
		`"authorization"`, "identity.WithUserPrincipal(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/interceptor/interceptor.go")),
		"errors.PrepareAppErrorContext(ctx)", "errors.AppErrorFromContext(ctx)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/server/grpc.go")),
		"srv.Health.Shutdown()", "srv.GRPC.GracefulStop()")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go")), `"reflection"`)
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")),
		"reflection: false")
	if wire {
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go")),
			"transportgrpc.New,", "adapterSet,\n\t\tusecaseSet,\n\t\ttransportSet,\n\t\twire.Struct(new(GRPCApp)")
		return
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go")),
		"transportgrpc.New,", "grpcModule(),\n\t\tadapterModule(),\n\t\tusecaseModule(),\n\t\ttransportModule(),")
}

// TestRegenerateKeepsMigrationNames: `nova new --force` over an existing
// project re-renders each migration under its existing timestamped name
// instead of adding a second copy that a migration runner would apply again.
func TestRegenerateKeepsMigrationNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for range 2 {
		gen, err := New(workerMatrixConfig("postgres", "kafka", "wire"))
		if err != nil {
			t.Fatal(err)
		}
		gen.Out = io.Discard
		if genErr := gen.Generate(dir); genErr != nil {
			t.Fatal(genErr)
		}
		time.Sleep(1100 * time.Millisecond) // a later second would mint new names
	}
	ups, err := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 2 {
		t.Errorf("migrations after a re-render = %v, want one users and one audit migration", ups)
	}
}

// assertMakefileHygiene: every target is listed by `make help` (it greps the
// "## " comments) and no recipe resolves paths against $(PWD), which is the
// caller's directory under `make -C`.
func assertMakefileHygiene(t *testing.T, mk string) {
	t.Helper()
	for _, line := range strings.Split(mk, "\n") {
		name, _, isRule := strings.Cut(line, ":")
		if !isRule || name == "" || strings.ContainsAny(name, " \t.$=#") {
			continue
		}
		if !strings.Contains(line, "## ") {
			t.Errorf("Makefile target %q has no ## description for make help", name)
		}
	}
	mustNotContain(t, mk, "$(PWD)")
}

// assertEnvSourceable runs `. ./.env.example` under sh: the README and
// docs/12 promise the file can be sourced as written.
func assertEnvSourceable(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "set -a; . ./.env.example")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf(".env.example is not sourceable: %v\n%s", err, out)
	}
}

// TestComposeParsesForEveryShape: compose rejects a service whose
// `environment:` key is present but empty, so a project with no dependencies
// must not render one. Parsed as a generic map to see the null that the typed
// compose struct would hide.
func TestComposeParsesForEveryShape(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]*config.ProjectConfig{
		"http_none":   noDBMatrixConfig("chi", "fx"),
		"grpc_none":   noDBGRPCConfig("fx"),
		"http_deps":   httpMatrixConfig("fiber", "postgres", "wire"),
		"worker_deps": workerMatrixConfig("mysql", "rabbitmq", "fx"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var doc map[string]any
			raw := readFile(t, filepath.Join(renderProject(t, cfg), "docker-compose.yaml"))
			if err := yamlv3.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatalf("compose YAML: %v", err)
			}
			services, _ := doc["services"].(map[string]any)
			for svc, v := range services {
				fields, _ := v.(map[string]any)
				if env, present := fields["environment"]; present && env == nil {
					t.Errorf("service %s has an empty environment key, which compose rejects", svc)
				}
			}
			if !cfg.HasDependencies() {
				mustNotContain(t, readFile(t, filepath.Join(renderProject(t, cfg), "README.md")),
					"Start infrastructure only")
			}
		})
	}
}

// assertTracerBuiltFirst: DI cleans up in reverse construction order, so the
// tracer must be built before the server or worker for its flush to run after
// they drain; otherwise the last spans of in-flight work are dropped.
func assertTracerBuiltFirst(t *testing.T, wireGen string) {
	t.Helper()
	tracer := strings.Index(wireGen, "tracing.Provider(")
	for _, ctor := range []string{"server.NewHTTPServer(", "server.NewGRPCServer(", "worker.NewWorker("} {
		if i := strings.Index(wireGen, ctor); i >= 0 && (tracer < 0 || tracer > i) {
			t.Errorf("wire_gen.go builds %s before tracing.Provider: spans would flush before the drain", ctor)
		}
	}
}
