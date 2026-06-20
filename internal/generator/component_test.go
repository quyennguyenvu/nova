package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/quyennguyenvu/nova/internal/manifest"
)

// newComponentGen builds a ComponentGenerator rooted at a temp dir with the
// given manifest (Root forced to that dir, Module defaulted for valid imports).
func newComponentGen(t *testing.T, m *manifest.Manifest) (*ComponentGenerator, string) {
	t.Helper()
	dir := t.TempDir()
	m.Root = dir
	if m.Module == "" {
		m.Module = "example.com/test"
	}
	return NewComponentGenerator(dir, m), dir
}

func TestGenerateEntityDefaultLayout(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatalf("GenerateEntity: %v", err)
	}
	// Entity lands in domain/entity; port (repo interface) in domain itself —
	// matching `nova new` (instruction.md §2), not the old stale paths.
	assertFileContains(t, filepath.Join(dir, "internal/domain/entity/order.go"), "package entity")
	assertFileContains(t, filepath.Join(dir, "internal/domain/entity/order.go"), "type Order struct")
	assertFileContains(t, filepath.Join(dir, "internal/domain/order.go"), "package domain")
	assertFileContains(t, filepath.Join(dir, "internal/domain/order.go"), "type OrderRepository interface")
	// The port is typed against the entity so a sqlc impl can satisfy it.
	assertFileContains(t, filepath.Join(dir, "internal/domain/order.go"), "*entity.Order")
}

func TestGenerateUseCaseDefaultLayout(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateUseCase("Order"); err != nil {
		t.Fatalf("GenerateUseCase: %v", err)
	}
	for _, f := range []string{"service.go", "dto.go"} {
		assertFileContains(t, filepath.Join(dir, "internal/usecase/order", f), "package order")
	}
}

func TestGenerateRepositorySQLCPostgres(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default()) // default stack = sqlc/postgres
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatalf("GenerateEntity: %v", err)
	}
	if err := gen.GenerateRepository("Order", "postgres"); err != nil {
		t.Fatalf("GenerateRepository: %v", err)
	}

	// Impl: typed, sqlc-wired, satisfies the domain port.
	impl := filepath.Join(dir, "internal/adapter/repository/postgres/order_repository.go")
	assertFileContains(t, impl, "package postgres")
	assertFileContains(t, impl, "var _ domain.OrderRepository = (*OrderRepository)(nil)")
	assertFileContains(t, impl, "mapper.OrderToCreateParams(e)")
	assertFileContains(t, impl, "pgx.ErrNoRows")

	// Query file with the named sqlc queries.
	q := filepath.Join(dir, "sqlc/query/order.sql")
	assertFileContains(t, q, "-- name: CreateOrder :one")
	assertFileContains(t, q, "-- name: GetOrderByID :one")
	assertFileContains(t, q, "RETURNING *")

	// Mapper: pg timestamps go through pgtype.
	mp := filepath.Join(dir, "internal/adapter/repository/postgres/mapper/order.go")
	assertFileContains(t, mp, "func OrderToEntity(row dbgen.Order) *entity.Order")
	assertFileContains(t, mp, "pgtype.Timestamptz{Time: e.CreatedAt, Valid: true}")
	assertFileContains(t, mp, "CreatedAt: row.CreatedAt.Time")

	// Migration created (timestamped filename).
	if matches, _ := filepath.Glob(
		filepath.Join(dir, "sqlc/migrations/*_create_orders_table.up.sql"),
	); len(
		matches,
	) != 1 {
		t.Errorf("expected one create_orders_table.up.sql migration, found %d", len(matches))
	} else {
		assertFileContains(t, matches[0], "id BIGSERIAL PRIMARY KEY")
	}
}

func TestGenerateRepositorySQLCMySQL(t *testing.T) {
	m := manifest.Default()
	m.Stack.Database = "mysql"
	gen, dir := newComponentGen(t, m)
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatalf("GenerateEntity: %v", err)
	}
	if err := gen.GenerateRepository("Order", "mysql"); err != nil {
		t.Fatalf("GenerateRepository: %v", err)
	}
	impl := filepath.Join(dir, "internal/adapter/repository/mysql/order_repository.go")
	assertFileContains(t, impl, "sql.ErrNoRows")
	// MySQL Create uses LastInsertId (:execlastid), not RETURNING.
	assertFileContains(t, filepath.Join(dir, "sqlc/query/order.sql"), ":execlastid")
	// MySQL maps time.Time directly (no pgtype).
	mp := filepath.Join(dir, "internal/adapter/repository/mysql/mapper/order.go")
	assertFileContains(t, mp, "CreatedAt: e.CreatedAt")
}

func TestGenerateRepositoryGenericStubForRawStack(t *testing.T) {
	m := manifest.Default()
	m.Stack.QueryGen = "raw" // non-sqlc → generic stub
	gen, dir := newComponentGen(t, m)
	if err := gen.GenerateEntity("Order"); err != nil { // entity is required on every path
		t.Fatalf("GenerateEntity: %v", err)
	}
	if err := gen.GenerateRepository("Order", "postgres"); err != nil {
		t.Fatalf("GenerateRepository: %v", err)
	}
	p := filepath.Join(dir, "internal/adapter/repository/postgres/order_repository.go")
	assertFileContains(t, p, "type OrderRepository struct")
	assertFileContains(t, p, "not implemented")
}

func TestGenerateRepositoryRequiresEntityOnEveryStack(t *testing.T) {
	for _, qg := range []string{"sqlc", "raw"} {
		t.Run(qg, func(t *testing.T) {
			m := manifest.Default()
			m.Stack.QueryGen = qg
			gen, _ := newComponentGen(t, m)
			// No entity generated first → must full-stop on any stack.
			if err := gen.GenerateRepository("Ghost", "postgres"); err == nil {
				t.Fatalf("%s stack: expected error when entity is missing, got nil", qg)
			}
		})
	}
}

func TestMapFieldTypes(t *testing.T) {
	cases := []struct {
		engine, goType, wantSQL, wantRead, wantWrite string
	}{
		{"postgres", "string", "TEXT", "row.Name", "e.Name"},
		{"postgres", "int64", "BIGINT", "row.Name", "e.Name"},
		{
			"postgres",
			"time.Time",
			"TIMESTAMP WITH TIME ZONE",
			"row.Name.Time",
			"pgtype.Timestamptz{Time: e.Name, Valid: true}",
		},
		{"postgres", "json.RawMessage", "JSONB", "row.Name", "e.Name"},
		{"mysql", "string", "VARCHAR(255)", "row.Name", "e.Name"},
		{"mysql", "time.Time", "DATETIME", "row.Name", "e.Name"},
		{"mysql", "json.RawMessage", "JSON", "row.Name", "e.Name"},
	}
	for _, c := range cases {
		t.Run(c.engine+"/"+c.goType, func(t *testing.T) {
			fm, ok := mapField(c.engine, entityField{Name: "Name", GoType: c.goType, Column: "name"})
			if !ok {
				t.Fatalf("mapField(%s,%s) returned ok=false", c.engine, c.goType)
			}
			if fm.sqlType != c.wantSQL {
				t.Errorf("sqlType = %q, want %q", fm.sqlType, c.wantSQL)
			}
			if fm.readExpr != c.wantRead {
				t.Errorf("readExpr = %q, want %q", fm.readExpr, c.wantRead)
			}
			if fm.writeExpr != c.wantWrite {
				t.Errorf("writeExpr = %q, want %q", fm.writeExpr, c.wantWrite)
			}
		})
	}
	if _, ok := mapField("postgres", entityField{Name: "X", GoType: "chan int"}); ok {
		t.Error("unsupported type should return ok=false")
	}
}

func TestSnakeCaseAcronyms(t *testing.T) {
	cases := map[string]string{
		"ID": "id", "UserID": "user_id", "CreatedAt": "created_at",
		"HTTPServer": "http_server", "Name": "name",
	}
	for in, want := range cases {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateHandlerDefaultLayout(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateHandler("Order"); err != nil {
		t.Fatalf("GenerateHandler: %v", err)
	}
	// The key fix: handler goes to transport/http/v1/<lower>/handler.go with
	// package <lower> — not the old internal/adapter/handler/http/v1 path.
	p := filepath.Join(dir, "internal/transport/http/v1/order/handler.go")
	assertFileContains(t, p, "package order")
	assertFileContains(t, p, "type OrderHandler struct")

	// Handler ships alongside its request/response DTOs and assembler, same
	// three-file split as `nova new`.
	dtoPath := filepath.Join(dir, "internal/transport/http/v1/order/dto.go")
	assertFileContains(t, dtoPath, "type CreateOrderRequest struct")
	assertFileContains(t, dtoPath, "type OrderResponse struct")
	asmPath := filepath.Join(dir, "internal/transport/http/v1/order/assembler.go")
	assertFileContains(t, asmPath, "func ToOrderResponse(src any) *OrderResponse")
	regPath := filepath.Join(dir, "internal/transport/http/v1/order/registrar.go")
	assertFileContains(t, regPath, "func NewRegistrar(h *OrderHandler) *Registrar")
	assertFileContains(t, regPath, "group := router.Group(\"/orders\")")
}

// TestGenerateHandlerFrameworks proves the handler + registrar track the
// manifest's Stack.HTTPFramework: each framework gets its own router/context
// types, while dto + assembler stay framework-agnostic.
func TestGenerateHandlerFrameworks(t *testing.T) {
	cases := []struct {
		fw              string
		handlerContains string
		regContains     string
	}{
		{"fiber", "c *fiber.Ctx", "router fiber.Router"},
		{"gin", "c *gin.Context", "router gin.IRouter"},
		{"chi", "w http.ResponseWriter, r *http.Request", "router chi.Router"},
		{"echo", "c echo.Context", "router *echo.Group"},
	}
	for _, tc := range cases {
		t.Run(tc.fw, func(t *testing.T) {
			m := manifest.Default()
			m.Stack.HTTPFramework = tc.fw
			gen, dir := newComponentGen(t, m)
			if err := gen.GenerateHandler("Order"); err != nil {
				t.Fatalf("GenerateHandler(%s): %v", tc.fw, err)
			}
			base := "internal/transport/http/v1/order"
			assertFileContains(t, filepath.Join(dir, base, "handler.go"), tc.handlerContains)
			assertFileContains(t, filepath.Join(dir, base, "registrar.go"), tc.regContains)
			// Framework-agnostic files are identical regardless of framework.
			assertFileContains(t, filepath.Join(dir, base, "dto.go"), "type OrderResponse struct")
			assertFileContains(t, filepath.Join(dir, base, "assembler.go"), "func ToOrderResponse")
		})
	}
}

// TestGenerateHandlerUnsupportedFramework rejects a manifest naming a framework
// nova doesn't have skel templates for, rather than emitting a broken handler.
func TestGenerateHandlerUnsupportedFramework(t *testing.T) {
	m := manifest.Default()
	m.Stack.HTTPFramework = "express"
	gen, _ := newComponentGen(t, m)
	if err := gen.GenerateHandler("Order"); err == nil {
		t.Fatal("expected error for unsupported http_framework, got nil")
	}
}

func TestGenerateWorkerFullTransport(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default()) // MessageQueue "none" → kafka consumer
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	root := "internal/transport/worker"
	// Shared transport files.
	assertFileContains(t, filepath.Join(dir, root, "handler.go"), "type Handler interface")
	assertFileContains(t, filepath.Join(dir, root, "worker.go"), "type Worker struct")
	assertFileContains(t, filepath.Join(dir, root, "consumer.go"), "github.com/IBM/sarama") // kafka
	// Per-feature handler + DTO.
	h := filepath.Join(dir, root, "v1/order/handler.go")
	assertFileContains(t, h, "package order")
	assertFileContains(t, h, "func (*Handler) Topic() string")
	assertFileContains(t, filepath.Join(dir, root, "v1/order/dto.go"), "type OrderMessage struct")
	// Runnable entry points. app/worker.go boots through the DI graph, so it is
	// broker-agnostic and references no feature handler directly.
	assertFileContains(t, filepath.Join(dir, "cmd/worker.go"), "func workerCommand()")
	assertFileContains(t, filepath.Join(dir, "internal/app/worker.go"), "func RunWorker()")
	assertFileContains(t, filepath.Join(dir, "internal/app/worker.go"), "di.InitializeWorker(ctx)")
}

func TestGenerateWorkerRegistersRootCommand(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	// A minimal existing root.go (as an HTTP project would have).
	rootSrc := "package cmd\n\nfunc Execute() {\n\troot.AddCommand(\n\t\tapiCommand(),\n\t)\n}\n"
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd/root.go"), []byte(rootSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	got := filepath.Join(dir, "cmd/root.go")
	assertFileContains(t, got, "workerCommand()")
	assertFileContains(t, got, "apiCommand()") // existing registration preserved

	// Idempotent: a second feature must not double-register.
	if err := gen.GenerateWorker("Product"); err != nil {
		t.Fatalf("second GenerateWorker: %v", err)
	}
	data, _ := os.ReadFile(got)
	if n := strings.Count(string(data), "workerCommand()"); n != 1 {
		t.Errorf("workerCommand() registered %d times, want 1", n)
	}
}

func TestGenerateWorkerRabbitMQConsumer(t *testing.T) {
	m := manifest.Default()
	m.Stack.MessageQueue = "rabbitmq"
	gen, dir := newComponentGen(t, m)
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	assertFileContains(t, filepath.Join(dir, "internal/transport/worker/consumer.go"), "amqp091-go")
}

func TestGenerateWorkerSkipsExistingTransport(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("first GenerateWorker: %v", err)
	}
	// Hand-edit the shared orchestrator, then add a second feature.
	wpath := filepath.Join(dir, "internal/transport/worker/worker.go")
	if err := os.WriteFile(wpath, []byte("package worker // EDITED\n"), 0o600); err != nil {
		t.Fatalf("edit worker.go: %v", err)
	}
	if err := gen.GenerateWorker("Product"); err != nil {
		t.Fatalf("second GenerateWorker: %v", err)
	}
	// Shared file must NOT be clobbered; the new feature must be added.
	assertFileContains(t, wpath, "EDITED")
	assertFileContains(t, filepath.Join(dir, "internal/transport/worker/v1/product/handler.go"), "package product")
}

// writeDIPkg seeds a fake internal/infrastructure/di package under dir with the
// given files (name → contents), simulating a project generated by `nova new`.
func writeDIPkg(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	di := filepath.Join(dir, "internal/infrastructure/di")
	if err := os.MkdirAll(di, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(di, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGenerateWorkerScaffoldsWireDI(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default()) // DI defaults to wire
	// HTTP-only di package (nova-new layout): the worker DI symbols aren't present
	// yet, so the scaffold must merge WorkerApp into app.go, workerInfraSet +
	// InitializeWorker into wire.go, and provideWorkerHandlers into provider.go.
	writeDIPkg(t, dir, map[string]string{
		"app.go": "package di\n\nimport (\n" +
			"\t\"go.opentelemetry.io/otel/trace\"\n\n" +
			"\t\"example.com/test/internal/infrastructure/logger\"\n" +
			"\t\"example.com/test/internal/infrastructure/server\"\n)\n\n" +
			"type HTTPApp struct {\n\tLogger *logger.Logger\n\tServer *server.HTTPServer\n\tTracer trace.Tracer\n}\n",
		"wire.go": "//go:build wireinject\n\npackage di\n\nimport (\n" +
			"\t\"context\"\n\n\t\"github.com/google/wire\"\n)\n\n" +
			"func InitializeHTTPServer(ctx context.Context) (*HTTPApp, func(), error) {\n" +
			"\tpanic(wire.Build())\n}\n",
		"provider.go": "package di\n\nimport \"github.com/go-playground/validator/v10\"\n\n" +
			"func provideValidator() *validator.Validate { return validator.New() }\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := "internal/infrastructure/di"
	// WorkerApp merged into app.go; InitializeWorker merged into wire.go.
	app := filepath.Join(dir, di, "app.go")
	assertFileContains(t, app, "type HTTPApp struct")
	assertFileContains(t, app, "type WorkerApp struct")
	assertFileContains(t, app, "example.com/test/internal/transport/worker")
	w := filepath.Join(dir, di, "wire.go")
	assertFileContains(t, w, "//go:build wireinject")
	assertFileContains(t, w, "func InitializeHTTPServer(")
	assertFileContains(t, w, "func InitializeWorker(ctx context.Context) (*WorkerApp, func(), error)")
	assertFileContains(t, w, "wire.Struct(new(WorkerApp), \"*\")")
	// workerInfraSet is defined (not just referenced) so the graph resolves.
	assertFileContains(t, w, "var workerInfraSet = wire.NewSet(")
	assertFileContains(t, w, "worker.NewKafkaConsumer")
	assertFileContains(t, w, "orderworker.NewHandler")
	// provideWorkerHandlers feeds the []worker.Handler slice from provider.go.
	p := filepath.Join(dir, di, "provider.go")
	assertFileContains(t, p, "func provideValidator()")
	assertFileContains(t, p, "func provideWorkerHandlers(orderHandler *orderworker.Handler) []worker.Handler")
	// No standalone worker_*.go files — everything was merged.
	for _, f := range []string{"worker_app.go", "worker_wire.go", "worker_fx.go", "worker_provider.go"} {
		if _, err := os.Stat(filepath.Join(dir, di, f)); err == nil {
			t.Errorf("%s should not exist — worker DI must merge into app.go/wire.go/provider.go", f)
		}
	}
}

func TestGenerateWorkerScaffoldsFxDI(t *testing.T) {
	m := manifest.Default()
	m.Stack.DI = "fx"
	gen, dir := newComponentGen(t, m)
	// fx layout: app.go holds the App bundles, fx.go the Initialize* injectors.
	writeDIPkg(t, dir, map[string]string{
		"app.go": "package di\n\nimport (\n" +
			"\t\"go.opentelemetry.io/otel/trace\"\n\n" +
			"\t\"example.com/test/internal/infrastructure/logger\"\n" +
			"\t\"example.com/test/internal/infrastructure/server\"\n)\n\n" +
			"type HTTPApp struct {\n\tLogger *logger.Logger\n\tServer *server.HTTPServer\n\tTracer trace.Tracer\n}\n",
		"fx.go": "package di\n\nimport (\n\t\"context\"\n\t\"time\"\n)\n\n" +
			"const stopTimeout = 15 * time.Second\n\n" +
			"func InitializeHTTPServer(ctx context.Context) {}\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := "internal/infrastructure/di"
	assertFileContains(t, filepath.Join(dir, di, "app.go"), "type WorkerApp struct")
	f := filepath.Join(dir, di, "fx.go")
	assertFileContains(t, f, "func InitializeHTTPServer(")
	assertFileContains(t, f, "func InitializeWorker(ctx context.Context) (*WorkerApp, func(), error)")
	assertFileContains(t, f, "fx.Populate(&log, &w, &tracer)")
	// fx populates via modules (TODO in the scaffold), so no wire-style
	// workerInfraSet/provideWorkerHandlers and no standalone worker_*.go files.
	for _, fn := range []string{"worker_app.go", "worker_wire.go", "worker_fx.go", "worker_provider.go"} {
		if _, err := os.Stat(filepath.Join(dir, di, fn)); err == nil {
			t.Errorf("%s should not exist — worker DI must merge into app.go/fx.go", fn)
		}
	}
}

// TestGenerateWorkerDIFallsBackToStandalone covers a di package that is not laid
// out like nova-new (none of app.go / wire.go / provider.go to merge into): the
// scaffold degrades to standalone worker_app.go / worker_wire.go /
// worker_provider.go files.
func TestGenerateWorkerDIFallsBackToStandalone(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	// di package exists but holds none of the canonical files.
	writeDIPkg(t, dir, map[string]string{
		"di.go": "package di\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := "internal/infrastructure/di"
	assertFileContains(t, filepath.Join(dir, di, "worker_app.go"), "type WorkerApp struct")
	assertFileContains(t, filepath.Join(dir, di, "worker_wire.go"),
		"func InitializeWorker(ctx context.Context) (*WorkerApp, func(), error)")
	assertFileContains(t, filepath.Join(dir, di, "worker_provider.go"),
		"func provideWorkerHandlers(orderHandler *orderworker.Handler) []worker.Handler")
}

func TestGenerateWorkerSkipsExistingWorkerDI(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	// Project already generated WITH the worker — WorkerApp, InitializeWorker,
	// and provideWorkerHandlers all exist. Scaffolding must not duplicate any.
	writeDIPkg(t, dir, map[string]string{
		"app.go": "package di\n\ntype WorkerApp struct{}\n",
		"wire.go": "//go:build wireinject\n\npackage di\n\n" +
			"func InitializeWorker() {}\n",
		"provider.go": "package di\n\nfunc provideWorkerHandlers() {}\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := filepath.Join(dir, "internal/infrastructure/di")
	for _, f := range []string{"worker_app.go", "worker_wire.go", "worker_fx.go", "worker_provider.go"} {
		if _, err := os.Stat(filepath.Join(di, f)); err == nil {
			t.Errorf("%s should not be written when worker DI already exists", f)
		}
	}
}

// TestWorkerAppTemplatesIdentical guards the app/worker.go bootstrap against
// drift between the two generators, same as the cmd_worker guard: `nova new`
// (templates/app/worker.go.tmpl) and `nova add` (skel/worker/app_worker.go.tmpl)
// both boot through di.InitializeWorker and must emit byte-identical files.
func TestWorkerAppTemplatesIdentical(t *testing.T) {
	data := struct{ ModuleName string }{ModuleName: "example.com/test"}
	render := func(fs interface{ ReadFile(string) ([]byte, error) }, path string) string {
		t.Helper()
		raw, err := fs.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		tmpl, err := template.New(filepath.Base(path)).Parse(string(raw))
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		var buf bytes.Buffer
		if exErr := tmpl.Execute(&buf, data); exErr != nil {
			t.Fatalf("execute %s: %v", path, exErr)
		}
		return buf.String()
	}
	got := render(skelFS, "skel/worker/app_worker.go.tmpl")
	want := render(templateFS, "templates/app/worker.go.tmpl")
	if got != want {
		t.Errorf("worker app templates diverged:\n--- skel ---\n%s\n--- templates ---\n%s", got, want)
	}
}

func TestGenerateHandlerCustomManifest(t *testing.T) {
	// A project with a non-nova layout described entirely by its manifest.
	m := manifest.Default()
	m.Layout["handler"] = manifest.Target{Dir: "api/http/{lower}", Package: "{lower}"}
	gen, dir := newComponentGen(t, m)

	if err := gen.GenerateHandler("Order"); err != nil {
		t.Fatalf("GenerateHandler: %v", err)
	}
	assertFileContains(t, filepath.Join(dir, "api/http/order/handler.go"), "package order")
}

// TestWorkerCmdTemplatesIdentical guards against drift between the two
// independent generators' worker-command templates. `nova new`
// (templates/cmd/worker.go.tmpl, rendered from templateFS) and `nova add`
// (skel/worker/cmd_worker.go.tmpl, rendered from skelFS) deliberately keep
// separate template trees, so the cmd/worker.go they emit must be byte-identical
// — same data field (.ModuleName), same output. If you change one, change both.
func TestWorkerCmdTemplatesIdentical(t *testing.T) {
	data := struct{ ModuleName string }{ModuleName: "example.com/test"}

	render := func(fs interface{ ReadFile(string) ([]byte, error) }, path string) string {
		t.Helper()
		raw, err := fs.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		tmpl, err := template.New(filepath.Base(path)).Parse(string(raw))
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		var buf bytes.Buffer
		if exErr := tmpl.Execute(&buf, data); exErr != nil {
			t.Fatalf("execute %s: %v", path, exErr)
		}
		return buf.String()
	}

	got := render(skelFS, "skel/worker/cmd_worker.go.tmpl")
	want := render(templateFS, "templates/cmd/worker.go.tmpl")
	if got != want {
		t.Errorf(
			"worker cmd templates diverged:\n--- skel (nova add) ---\n%s\n--- templates (nova new) ---\n%s",
			got,
			want,
		)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("file %s does not contain %q", path, want)
	}
}
