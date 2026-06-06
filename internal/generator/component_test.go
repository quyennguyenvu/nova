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
	for _, f := range []string{"service.go", "dto.go", "errors.go"} {
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
	// HTTP-only di package: no WorkerApp, no InitializeWorker yet.
	writeDIPkg(t, dir, map[string]string{
		"app.go":  "package di\n\ntype HTTPApp struct{}\n",
		"wire.go": "//go:build wireinject\n\npackage di\n\nfunc InitializeHTTPServer() {}\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := "internal/infrastructure/di"
	assertFileContains(t, filepath.Join(dir, di, "worker_app.go"), "type WorkerApp struct")
	w := filepath.Join(dir, di, "worker_wire.go")
	assertFileContains(t, w, "//go:build wireinject")
	assertFileContains(t, w, "func InitializeWorker(ctx context.Context) (*WorkerApp, func(), error)")
	assertFileContains(t, w, "wire.Struct(new(WorkerApp), \"*\")")
	// fx variant must not be emitted for a wire project.
	if _, err := os.Stat(filepath.Join(dir, di, "worker_fx.go")); err == nil {
		t.Error("worker_fx.go should not exist for a wire project")
	}
}

func TestGenerateWorkerScaffoldsFxDI(t *testing.T) {
	m := manifest.Default()
	m.Stack.DI = "fx"
	gen, dir := newComponentGen(t, m)
	writeDIPkg(t, dir, map[string]string{
		"fx.go": "package di\n\nconst stopTimeout = 0\n\nfunc InitializeHTTPServer() {}\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := "internal/infrastructure/di"
	assertFileContains(t, filepath.Join(dir, di, "worker_app.go"), "type WorkerApp struct")
	f := filepath.Join(dir, di, "worker_fx.go")
	assertFileContains(t, f, "func InitializeWorker(ctx context.Context) (*WorkerApp, func(), error)")
	assertFileContains(t, f, "fx.Populate(&log, &w, &tracer)")
	if _, err := os.Stat(filepath.Join(dir, di, "worker_wire.go")); err == nil {
		t.Error("worker_wire.go should not exist for an fx project")
	}
}

func TestGenerateWorkerSkipsExistingWorkerDI(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	// Project already generated WITH the worker — WorkerApp + InitializeWorker
	// exist. Scaffolding must not duplicate either symbol.
	writeDIPkg(t, dir, map[string]string{
		"app.go": "package di\n\ntype WorkerApp struct{}\n",
		"wire.go": "//go:build wireinject\n\npackage di\n\n" +
			"func InitializeWorker() {}\n",
	})
	if err := gen.GenerateWorker("Order"); err != nil {
		t.Fatalf("GenerateWorker: %v", err)
	}
	di := filepath.Join(dir, "internal/infrastructure/di")
	for _, f := range []string{"worker_app.go", "worker_wire.go", "worker_fx.go"} {
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
