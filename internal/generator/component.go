package generator

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/quyennguyenvu/nova/internal/manifest"
)

// ComponentGenerator generates individual components in an existing project.
// Output paths and package names come from the manifest, so it can target
// projects with non-standard layouts (see internal/manifest).
type ComponentGenerator struct {
	baseDir  string
	manifest *manifest.Manifest
}

// NewComponentGenerator creates a component generator rooted at baseDir, using
// m to resolve where each component's files go.
func NewComponentGenerator(baseDir string, m *manifest.Manifest) *ComponentGenerator {
	return &ComponentGenerator{baseDir: baseDir, manifest: m}
}

// GenerateEntity creates a new entity plus its repository-interface port. The
// port is typed against the entity (matching `nova new`) so a generated
// repository impl can satisfy it; this assumes the entity package is named
// "entity" (the nova default).
func (g *ComponentGenerator) GenerateEntity(name string) error {
	entityRes, err := g.target("entity", name, "")
	if err != nil {
		return err
	}
	portRes, err := g.target("port", name, "")
	if err != nil {
		return err
	}

	data := tmplData{
		ModuleName:   g.manifest.Module,
		Package:      entityRes.Package,
		Title:        toTitle(name),
		Lower:        strings.ToLower(name),
		PortPkg:      portRes.Package,
		EntityImport: g.manifest.Module + "/" + entityRes.Dir,
	}
	specs := []renderSpec{
		{"skel/entity/entity.go.tmpl", relOf(entityRes, ""), false},
		{"skel/entity/port.go.tmpl", relOf(portRes, ""), false},
	}
	if rErr := g.renderTemplates(specs, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated entity: %s\n", data.Title)
	return nil
}

// GenerateUseCase creates a new use case (service + DTO + errors).
func (g *ComponentGenerator) GenerateUseCase(name string) error {
	res, err := g.target("usecase", name, "")
	if err != nil {
		return err
	}
	data := tmplData{Package: res.Package, Title: toTitle(name), Lower: strings.ToLower(name)}
	specs := []renderSpec{
		{"skel/usecase/service.go.tmpl", filepath.Join(res.Dir, "service.go"), false},
		{"skel/usecase/dto.go.tmpl", filepath.Join(res.Dir, "dto.go"), false},
		{"skel/usecase/errors.go.tmpl", filepath.Join(res.Dir, "errors.go"), false},
	}
	if rErr := g.renderTemplates(specs, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated use case: %s\n", res.Package)
	return nil
}

// GenerateHandler creates a new HTTP handler.
func (g *ComponentGenerator) GenerateHandler(name string) error {
	res, err := g.target("handler", name, "")
	if err != nil {
		return err
	}
	data := tmplData{Package: res.Package, Title: toTitle(name), Lower: strings.ToLower(name)}
	if rErr := g.renderTemplates([]renderSpec{
		{"skel/handler/handler.go.tmpl", relOf(res, "handler.go"), false},
	}, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated handler: %sHandler\n", data.Title)
	return nil
}

// tmplData is the context passed to every skel/ template. Each template uses
// the subset of fields it needs; unused fields stay zero.
type tmplData struct {
	ModuleName    string
	Package       string // the target file's package name
	Title         string // PascalCase aggregate name, e.g. Order
	Lower         string // lowercase, e.g. order
	Topic         string // worker: the event topic, e.g. order.event
	PortPkg       string // entity: the repository-interface package (domain)
	EntityImport  string // entity: import path of the entity package
	FeatureImport string // worker: import path of the feature handler package
}

// GenerateWorker scaffolds the full worker transport — the Handler interface,
// the Worker orchestrator, the broker consumer (kafka/rabbitmq per the stack),
// and a per-feature handler + message DTO — rendered from the worker template
// directory. The entry point internal/app/worker.go boots through the DI graph
// (di.InitializeWorker), and scaffoldWorkerDI drops a minimal WorkerApp +
// InitializeWorker injector into the project's di package. The shared transport
// files are written only when absent, so adding a second feature just appends
// its handler.
func (g *ComponentGenerator) GenerateWorker(name string) error {
	res, err := g.target("worker", name, "")
	if err != nil {
		return err
	}
	// Shared transport files sit at the worker root — strip the trailing
	// /v1/<name> feature segment from the resolved feature dir.
	root := filepath.Dir(filepath.Dir(res.Dir))

	lower := strings.ToLower(name)
	data := tmplData{
		ModuleName:    g.manifest.Module,
		Package:       res.Package,
		Title:         toTitle(name),
		Lower:         lower,
		Topic:         lower + ".event",
		FeatureImport: g.manifest.Module + "/" + res.Dir,
	}

	rabbit := g.manifest.Stack.MessageQueue == rabbitmq
	consumer := pick(rabbit, "skel/worker/rabbitmq_consumer.go.tmpl", "skel/worker/kafka_consumer.go.tmpl")

	specs := []renderSpec{
		// Shared transport + entry points — written only when absent.
		{"skel/worker/handler.go.tmpl", filepath.Join(root, "handler.go"), true},
		{"skel/worker/worker.go.tmpl", filepath.Join(root, "worker.go"), true},
		{consumer, filepath.Join(root, "consumer.go"), true},
		{"skel/worker/cmd_worker.go.tmpl", "cmd/worker.go", true},
		// app/worker.go boots through the DI graph (di.InitializeWorker), so it
		// is broker-agnostic; the broker wiring lives in the di scaffold below.
		{"skel/worker/app_worker.go.tmpl", "internal/app/worker.go", true},
		// Per-feature handler + DTO — always written.
		{"skel/worker/feature_handler.go.tmpl", filepath.Join(res.Dir, "handler.go"), false},
		{"skel/worker/feature_dto.go.tmpl", filepath.Join(res.Dir, "dto.go"), false},
	}
	if rErr := g.renderTemplates(specs, data); rErr != nil {
		return rErr
	}
	if rErr := g.scaffoldWorkerDI(data); rErr != nil {
		return rErr
	}
	if rErr := g.registerWorkerCommand(); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated runnable worker service + %s feature handler\n", lower)
	fmt.Fprintf(
		os.Stdout,
		"   ▶ run `go mod tidy` then `go run main.go worker` (broker addr via env, e.g. KAFKA_BROKERS)\n",
	)
	fmt.Fprintf(os.Stdout, "   ▶ add more feature handlers to the slice in internal/app/worker.go\n")
	return nil
}

// registerWorkerCommand adds workerCommand() to cmd/root.go's root.AddCommand
// call so the worker subcommand is wired into the binary. It is idempotent and
// degrades to a printed hint when root.go is absent or unrecognised.
func (g *ComponentGenerator) registerWorkerCommand() error {
	rootPath := filepath.Join(g.baseDir, "cmd", "root.go")
	raw, err := os.ReadFile(rootPath)
	if err != nil {
		fmt.Fprintf(
			os.Stdout,
			"   ⚠️  cmd/root.go not found — register workerCommand() in your root command manually\n",
		)
		return nil //nolint:nilerr // absence is tolerated; the hint covers it
	}
	src := string(raw)
	if strings.Contains(src, "workerCommand()") {
		return nil // already registered
	}
	const marker = "root.AddCommand("
	idx := strings.Index(src, marker)
	if idx == -1 {
		fmt.Fprintf(
			os.Stdout,
			"   ⚠️  could not locate root.AddCommand( in cmd/root.go — register workerCommand() manually\n",
		)
		return nil
	}
	at := idx + len(marker)
	out := src[:at] + "\n\t\tworkerCommand()," + src[at:]
	if formatted, fmtErr := format.Source([]byte(out)); fmtErr == nil {
		out = string(formatted)
	}
	//nolint:gosec // rootPath = baseDir + fixed "cmd/root.go"; not user-tainted.
	if wErr := os.WriteFile(rootPath, []byte(out), 0o600); wErr != nil {
		return fmt.Errorf("update cmd/root.go: %w", wErr)
	}
	fmt.Fprintf(os.Stdout, "   ✏️  registered workerCommand() in cmd/root.go\n")
	return nil
}

// scaffoldWorkerDI drops the worker's DI wiring into the project's di package
// (internal/infrastructure/di): the WorkerApp bundle plus a minimal
// InitializeWorker injector for the project's DI engine (Stack.DI). The
// injector is a scaffold — it builds only WorkerApp; the user adds the provider
// sets the worker needs. Each symbol is written only when the package does not
// already declare it, so a project generated with the worker enabled is left
// untouched and a re-run never duplicates. When there is no di package (not a
// nova-new layout) it degrades to a printed hint.
func (g *ComponentGenerator) scaffoldWorkerDI(data tmplData) error {
	const diDir = "internal/infrastructure/di"
	abs := filepath.Join(g.baseDir, diDir)
	if _, err := os.Stat(abs); err != nil {
		fmt.Fprintf(
			os.Stdout,
			"   ⚠️  %s not found — wire RunWorker into your DI graph manually (InitializeWorker → *WorkerApp)\n",
			diDir,
		)
		return nil //nolint:nilerr // absence is tolerated; the hint covers it
	}

	var specs []renderSpec
	if !g.diPkgDeclares(abs, "type WorkerApp struct") {
		specs = append(
			specs,
			renderSpec{"skel/worker/di_worker_app.go.tmpl", filepath.Join(diDir, "worker_app.go"), true},
		)
	}
	if !g.diPkgDeclares(abs, "func InitializeWorker(") {
		tmpl, out := "skel/worker/di_worker_wire.go.tmpl", "worker_wire.go"
		if g.manifest.Stack.DI == "fx" {
			tmpl, out = "skel/worker/di_worker_fx.go.tmpl", "worker_fx.go"
		}
		specs = append(specs, renderSpec{tmpl, filepath.Join(diDir, out), true})
	}
	if len(specs) == 0 {
		return nil // worker DI already present
	}
	if err := g.renderTemplates(specs, data); err != nil {
		return err
	}
	if g.manifest.Stack.DI != "fx" {
		fmt.Fprintf(
			os.Stdout,
			"   ▶ wire scaffold written — add provider sets to InitializeWorker, then run `wire ./...`\n",
		)
	}
	return nil
}

// diPkgDeclares reports whether any .go file in dir contains needle — a cheap
// substring check (no parsing) that keeps DI scaffolding idempotent and avoids
// redeclaring symbols a worker-enabled project already has.
func (g *ComponentGenerator) diPkgDeclares(dir, needle string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, rErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rErr != nil {
			continue
		}
		if strings.Contains(string(raw), needle) {
			return true
		}
	}
	return false
}

// GenerateRepository creates a new repository implementation. repoType selects
// the database engine ({db} in the layout) — defaults to the manifest stack.
//
// When the project's stack uses sqlc with a SQL engine, it generates the full
// sqlc-backed slice (migration + query + impl + mapper) driven by the existing
// entity's fields; otherwise it emits a generic, hand-fillable stub.
func (g *ComponentGenerator) GenerateRepository(name, repoType string) error {
	// A repository implements an aggregate's domain port, so the entity (and
	// its port) must already exist — fail fast before writing anything,
	// regardless of stack.
	if err := g.requireEntity(name); err != nil {
		return err
	}

	if g.manifest.Stack.QueryGen == "sqlc" && (repoType == enginePostgres || repoType == engineMySQL) {
		return g.generateSQLCRepository(name, repoType)
	}

	res, err := g.target("repository", name, repoType)
	if err != nil {
		return err
	}
	data := tmplData{Package: res.Package, Title: toTitle(name), Lower: strings.ToLower(name)}
	if rErr := g.renderTemplates([]renderSpec{
		{"skel/repository/stub.go.tmpl", relOf(res, ""), false},
	}, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated repository: %sRepository (%s)\n", data.Title, res.Package)
	return nil
}

// requireEntity fails fast when the aggregate's entity file does not exist yet.
// A repository implements the entity's port, so the entity must be generated
// first (`nova add entity <Name>`).
func (g *ComponentGenerator) requireEntity(name string) error {
	res, err := g.target("entity", name, "")
	if err != nil {
		return err
	}
	path := g.path(res, "")
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf(
			"entity %s not found at %s — run `nova add entity %s` first",
			toTitle(name), path, toTitle(name),
		)
	}
	return nil
}

// target resolves a component's layout entry, erroring if the manifest does not
// declare it.
func (g *ComponentGenerator) target(component, name, dbOverride string) (manifest.Resolved, error) {
	r, ok := g.manifest.Resolve(component, name, dbOverride)
	if !ok {
		return manifest.Resolved{}, fmt.Errorf("component %q is not declared in the layout", component)
	}
	return r, nil
}

// path joins baseDir + the resolved Dir + a filename. fallbackFile is used when
// the layout entry has no File pattern (e.g. handler.go).
func (g *ComponentGenerator) path(r manifest.Resolved, fallbackFile string) string {
	file := r.File
	if file == "" {
		file = fallbackFile
	}
	return filepath.Join(g.baseDir, r.Dir, file)
}

// Helper functions

func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func toTitle(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
