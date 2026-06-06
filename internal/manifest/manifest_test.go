package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLayoutKeys(t *testing.T) {
	m := Default()
	for _, key := range []string{"entity", "port", "usecase", "repository", "handler"} {
		if _, ok := m.Layout[key]; !ok {
			t.Errorf("Default() layout missing %q", key)
		}
	}
	if m.Stack.Database != "postgres" {
		t.Errorf("Default() stack database = %q, want postgres", m.Stack.Database)
	}
}

func TestResolve(t *testing.T) {
	m := Default()
	tests := []struct {
		name        string
		component   string
		input       string
		db          string
		wantDir     string
		wantFile    string
		wantPackage string
	}{
		{"entity", "entity", "Order", "", "internal/domain/entity", "order.go", "entity"},
		{"entity snake", "entity", "OrderItem", "", "internal/domain/entity", "order_item.go", "entity"},
		{"port", "port", "Order", "", "internal/domain", "order.go", "domain"},
		{"usecase lower dir", "usecase", "Order", "", "internal/usecase/order", "", "order"},
		{
			"repository default db",
			"repository",
			"Order",
			"",
			"internal/adapter/repository/postgres",
			"order_repository.go",
			"postgres",
		},
		{
			"repository db override",
			"repository",
			"Order",
			"mysql",
			"internal/adapter/repository/mysql",
			"order_repository.go",
			"mysql",
		},
		{"handler", "handler", "Order", "", "internal/transport/http/v1/order", "", "order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := m.Resolve(tt.component, tt.input, tt.db)
			if !ok {
				t.Fatalf("Resolve(%q) returned ok=false", tt.component)
			}
			if got.Dir != tt.wantDir {
				t.Errorf("Dir = %q, want %q", got.Dir, tt.wantDir)
			}
			if got.File != tt.wantFile {
				t.Errorf("File = %q, want %q", got.File, tt.wantFile)
			}
			if got.Package != tt.wantPackage {
				t.Errorf("Package = %q, want %q", got.Package, tt.wantPackage)
			}
		})
	}
}

func TestResolveUnknownComponent(t *testing.T) {
	if _, ok := Default().Resolve("nonexistent", "Order", ""); ok {
		t.Error("Resolve(unknown) returned ok=true, want false")
	}
}

func TestLoadAbsentUsesDefaultsAndModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/svc\n\ngo 1.25.1\n")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Module != "github.com/acme/svc" {
		t.Errorf("Module = %q, want github.com/acme/svc (from go.mod)", m.Module)
	}
	if m.Root != dir {
		t.Errorf("Root = %q, want %q", m.Root, dir)
	}
	if m.Stack.Database != "postgres" {
		t.Errorf("Stack.Database = %q, want default postgres", m.Stack.Database)
	}
}

func TestLoadOverlayMergesPerField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/ignored\n")
	writeFile(t, filepath.Join(dir, FileName), `version: 1
module: github.com/acme/shop
stack:
  http_framework: gin
  database: mysql
layout:
  handler:
    dir: api/http/{lower}
`)

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// File module wins over go.mod.
	if m.Module != "github.com/acme/shop" {
		t.Errorf("Module = %q, want github.com/acme/shop", m.Module)
	}
	if m.Stack.HTTPFramework != "gin" || m.Stack.Database != "mysql" {
		t.Errorf("stack overlay wrong: %+v", m.Stack)
	}
	// Unset stack field keeps the default.
	if m.Stack.Cache != "redis" {
		t.Errorf("Stack.Cache = %q, want default redis", m.Stack.Cache)
	}
	// Overridden dir applies; unset Package field falls back to default.
	got, _ := m.Resolve("handler", "Order", "")
	if got.Dir != "api/http/order" {
		t.Errorf("handler Dir = %q, want api/http/order", got.Dir)
	}
	if got.Package != "order" {
		t.Errorf("handler Package = %q, want order (default kept)", got.Package)
	}
	// Untouched component keeps its default entirely.
	if e, _ := m.Resolve("entity", "Order", ""); e.Dir != "internal/domain/entity" {
		t.Errorf("entity Dir = %q, want default", e.Dir)
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(dir, FileName), "version: [not-an-int\n")

	if _, err := Load(dir); err == nil {
		t.Error("Load(malformed) returned nil error, want parse failure")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
