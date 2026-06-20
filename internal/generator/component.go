package generator

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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

// GenerateUseCase creates a new use case (service + DTO). Errors are not
// scaffolded per-usecase: the canonical layout centralizes them as locale
// codes (pkg/locale) raised via pkg/errors, mirroring `nova new`.
func (g *ComponentGenerator) GenerateUseCase(name string) error {
	res, err := g.target("usecase", name, "")
	if err != nil {
		return err
	}
	data := tmplData{Package: res.Package, Title: toTitle(name), Lower: strings.ToLower(name)}
	specs := []renderSpec{
		{"skel/usecase/service.go.tmpl", filepath.Join(res.Dir, "service.go"), false},
		{"skel/usecase/dto.go.tmpl", filepath.Join(res.Dir, "dto.go"), false},
	}
	if rErr := g.renderTemplates(specs, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated use case: %s\n", res.Package)
	return nil
}

// GenerateHandler creates a new HTTP handler plus its request/response DTOs,
// assembler, and route registrar — the same file split as `nova new`
// (handler.go + dto.go + assembler.go + registrar.go in one package). The
// handler + registrar are rendered for the manifest's Stack.HTTPFramework
// (fiber/gin/chi/echo); dto + assembler are framework-agnostic.
func (g *ComponentGenerator) GenerateHandler(name string) error {
	res, err := g.target("handler", name, "")
	if err != nil {
		return err
	}
	fw := g.manifest.Stack.HTTPFramework
	if !supportedFrameworks[fw] {
		return fmt.Errorf(
			"add handler: unsupported http_framework %q in nova.yaml (valid: fiber, gin, chi, echo)",
			fw,
		)
	}
	data := tmplData{Package: res.Package, Title: toTitle(name), Lower: strings.ToLower(name)}
	if rErr := g.renderTemplates([]renderSpec{
		{"skel/handler/" + fw + "_handler.go.tmpl", relOf(res, "handler.go"), false},
		{"skel/handler/dto.go.tmpl", relOf(res, "dto.go"), false},
		{"skel/handler/assembler.go.tmpl", relOf(res, "assembler.go"), false},
		{"skel/handler/" + fw + "_registrar.go.tmpl", relOf(res, "registrar.go"), false},
	}, data); rErr != nil {
		return rErr
	}

	fmt.Fprintf(os.Stdout, "✅ Generated handler: %sHandler (%s)\n", data.Title, fw)
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
	MessageQueue  string // worker: the broker ("kafka"/"rabbitmq") — picks the DI providers
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
	broker := g.detectMessageQueue()
	data := tmplData{
		ModuleName:    g.manifest.Module,
		Package:       res.Package,
		Title:         toTitle(name),
		Lower:         lower,
		Topic:         lower + ".event",
		MessageQueue:  broker,
		FeatureImport: g.manifest.Module + "/" + res.Dir,
	}

	rabbit := broker == rabbitmq
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
// sets the worker needs. To match the nova-new layout, the WorkerApp struct is
// merged into the existing app.go and InitializeWorker into wire.go (or fx.go)
// rather than dropped in separate files; when that canonical file is absent it
// falls back to a standalone worker_*.go. Each symbol is written only when the
// package does not already declare it, so a project generated with the worker
// enabled is left untouched and a re-run never duplicates. When there is no di
// package (not a nova-new layout) it degrades to a printed hint.
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

	// Detect the DI engine from the package layout: a nova-new fx project ships
	// fx.go, a wire project ships wire.go. This is more reliable than the
	// manifest's Stack.DI, which defaults to "wire" for any project lacking a
	// nova.yaml — so an fx project would otherwise get the wrong scaffold.
	useFx := fileExists(filepath.Join(abs, "fx.go")) && !fileExists(filepath.Join(abs, "wire.go"))
	if !useFx && !fileExists(filepath.Join(abs, "wire.go")) {
		useFx = g.manifest.Stack.DI == "fx" // no canonical injector file — trust the manifest
	}

	wrote := false
	if !g.diPkgDeclares(abs, "type WorkerApp struct") {
		if err := g.mergeOrWriteDI(
			"skel/worker/di_worker_app.go.tmpl",
			filepath.Join(diDir, "app.go"),
			filepath.Join(diDir, "worker_app.go"),
			data,
		); err != nil {
			return err
		}
		wrote = true
	}
	if !g.diPkgDeclares(abs, "func InitializeWorker(") {
		tmpl, target, fallback := "skel/worker/di_worker_wire.go.tmpl", "wire.go", "worker_wire.go"
		if useFx {
			tmpl, target, fallback = "skel/worker/di_worker_fx.go.tmpl", "fx.go", "worker_fx.go"
		}
		if err := g.mergeOrWriteDI(
			tmpl, filepath.Join(diDir, target), filepath.Join(diDir, fallback), data,
		); err != nil {
			return err
		}
		wrote = true
	}
	// provideWorkerHandlers feeds workerInfraSet the []worker.Handler slice; it
	// lives in the non-build-tagged provider file so wire_gen.go can call it.
	// Wire's workerInfraSet (wireinject only) is broker-aware, so this provider
	// is — for fx the modules carry the providers, so it isn't needed there.
	if !useFx && !g.diPkgDeclares(abs, "func provideWorkerHandlers(") {
		if err := g.mergeOrWriteDI(
			"skel/worker/di_worker_provider.go.tmpl",
			filepath.Join(diDir, "provider.go"),
			filepath.Join(diDir, "worker_provider.go"),
			data,
		); err != nil {
			return err
		}
		wrote = true
	}
	if !wrote {
		return nil // worker DI already present
	}
	if !useFx {
		fmt.Fprintf(
			os.Stdout,
			"   ▶ wire scaffold written — add provider sets to InitializeWorker, then run `wire ./...`\n",
		)
	}
	return nil
}

// mergeOrWriteDI renders the scaffold template and merges its declarations into
// the existing targetRel file (the nova-new canonical home — app.go/wire.go/
// fx.go), injecting any imports the target lacks. When targetRel does not exist
// (a project that isn't laid out like nova-new) it writes the scaffold verbatim
// to the standalone fallbackRel instead.
func (g *ComponentGenerator) mergeOrWriteDI(tmpl, targetRel, fallbackRel string, data tmplData) error {
	src, err := renderTemplateString(tmpl, data)
	if err != nil {
		return err
	}
	targetAbs := filepath.Join(g.baseDir, targetRel)
	if _, statErr := os.Stat(targetAbs); statErr != nil {
		if wErr := writeFile(filepath.Join(g.baseDir, fallbackRel), src); wErr != nil {
			return wErr
		}
		fmt.Fprintf(os.Stdout, "   📄 %s\n", fallbackRel)
		return nil
	}
	if mErr := mergeGoDecls(targetAbs, src); mErr != nil {
		return fmt.Errorf("merge worker DI into %s: %w", targetRel, mErr)
	}
	fmt.Fprintf(os.Stdout, "   ✏️  merged worker DI into %s\n", targetRel)
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

// mergeGoDecls splices the declarations rendered into scaffoldSrc onto the end
// of the existing Go file at absPath: every top-level declaration after the
// scaffold's import block is appended, and any import the scaffold needs but
// the file lacks is injected into the file's import group (a fresh group is
// created when the file has none). The merged source is gofmt'd before writing.
// Both inputs must already parse.
func mergeGoDecls(absPath, scaffoldSrc string) error {
	target, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", absPath, err)
	}

	declBlock, scaffoldImports, err := splitGoScaffold(scaffoldSrc)
	if err != nil {
		return err
	}
	if declBlock == "" {
		return nil // nothing to merge
	}

	merged, err := injectImports(string(target), absPath, scaffoldImports)
	if err != nil {
		return err
	}
	merged = strings.TrimRight(merged, "\n") + "\n\n" + declBlock
	if !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	formatted, err := format.Source([]byte(merged))
	if err != nil {
		return fmt.Errorf("format merged %s: %w", absPath, err)
	}
	//nolint:gosec // absPath = baseDir + fixed di file name; not user-tainted.
	if wErr := os.WriteFile(absPath, formatted, 0o600); wErr != nil {
		return fmt.Errorf("write %s: %w", absPath, wErr)
	}
	return nil
}

// splitGoScaffold parses rendered Go source and returns (1) the text of every
// top-level declaration that is not the import block — from the first such
// declaration (including its doc comment) to EOF — and (2) the imports those
// declarations bring along.
func splitGoScaffold(src string) (string, []*ast.ImportSpec, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scaffold.go", src, parser.ParseComments)
	if err != nil {
		return "", nil, fmt.Errorf("parse scaffold: %w", err)
	}
	start := -1
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		pos := d.Pos()
		if doc := declDoc(d); doc != nil {
			pos = doc.Pos()
		}
		if off := fset.Position(pos).Offset; start == -1 || off < start {
			start = off
		}
	}
	if start == -1 {
		return "", nil, nil
	}
	return src[start:], f.Imports, nil
}

// declDoc returns the doc comment group attached to a top-level declaration, or
// nil when it has none.
func declDoc(d ast.Decl) *ast.CommentGroup {
	switch decl := d.(type) {
	case *ast.GenDecl:
		return decl.Doc
	case *ast.FuncDecl:
		return decl.Doc
	default:
		return nil
	}
}

// injectImports adds every import in want that src does not already declare,
// returning the updated source. Missing imports go into the file's existing
// parenthesised import group; if the file has none, a new group is inserted
// after the package clause. gofmt (run by the caller) re-sorts the result.
func injectImports(src, name string, want []*ast.ImportSpec) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", name, err)
	}

	have := make(map[string]bool, len(f.Imports))
	for _, imp := range f.Imports {
		have[importPath(imp)] = true
	}
	var lines []string
	for _, imp := range want {
		if have[importPath(imp)] {
			continue
		}
		spec := imp.Path.Value
		if imp.Name != nil {
			spec = imp.Name.Name + " " + spec
		}
		lines = append(lines, "\t"+spec)
		have[importPath(imp)] = true
	}
	if len(lines) == 0 {
		return src, nil
	}
	block := strings.Join(lines, "\n")

	if grp := importGroup(f); grp != nil && grp.Lparen.IsValid() {
		at := fset.Position(grp.Rparen).Offset
		return src[:at] + block + "\n" + src[at:], nil
	}
	at := fset.Position(f.Name.End()).Offset
	return src[:at] + "\n\nimport (\n" + block + "\n)" + src[at:], nil
}

// importGroup returns the file's import declaration, or nil when it has none.
func importGroup(f *ast.File) *ast.GenDecl {
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			return gd
		}
	}
	return nil
}

// importPath returns an import spec's unquoted path, the key used to dedupe.
func importPath(imp *ast.ImportSpec) string {
	return strings.Trim(imp.Path.Value, `"`)
}

// fileExists reports whether path names an existing file or directory.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// detectMessageQueue infers the project's broker from its infrastructure/pubsub
// files (kafka.go vs rabbitmq.go) rather than the manifest, whose Stack is only
// a default when the project has no nova.yaml — so the consumer + DI providers
// match the project even on a plain `nova new` layout. Falls back to the
// manifest's declared queue when neither file is present.
func (g *ComponentGenerator) detectMessageQueue() string {
	pubsub := filepath.Join(g.baseDir, "internal/infrastructure/pubsub")
	if fileExists(filepath.Join(pubsub, "rabbitmq.go")) {
		return rabbitmq
	}
	if fileExists(filepath.Join(pubsub, "kafka.go")) {
		return kafka
	}
	return g.manifest.Stack.MessageQueue
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
