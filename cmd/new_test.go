package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quyennguyenvu/nova/internal/config"
)

// TestNextStepsPerTransport pins the post-generation checklist: the real
// file name, generators before anything that needs their output, and the
// subcommand that actually exists for the chosen transport.
func TestNextStepsPerTransport(t *testing.T) {
	t.Parallel()
	cases := []struct{ transport, want string }{
		{"http", "go run main.go api"},
		{"worker", "go run main.go worker"},
		{"grpc", "go run main.go grpc"},
	}
	for _, tc := range cases {
		t.Run(tc.transport, func(t *testing.T) {
			t.Parallel()
			cfg := config.DefaultConfig()
			cfg.ProjectName = "demo"
			cfg.Transport = tc.transport
			joined := strings.Join(nextSteps(cfg), "\n")
			for _, want := range []string{"cd demo", "cp .env.example .env", "make gen", tc.want} {
				if !strings.Contains(joined, want) {
					t.Errorf("missing %q in:\n%s", want, joined)
				}
			}
			for _, stale := range []string{"cp env.example", "go mod tidy", "docker-compose up"} {
				if strings.Contains(joined, stale) {
					t.Errorf("stale step %q in:\n%s", stale, joined)
				}
			}
			if strings.Index(joined, "make gen") > strings.Index(joined, "go run") {
				t.Errorf("make gen must precede go run:\n%s", joined)
			}
		})
	}
}

// parseNewFlags builds the `new` command and parses args as a user would type
// them, so applyFlags sees real pflag state (Changed, defaults, values).
func parseNewFlags(t *testing.T, args ...string) *config.ProjectConfig {
	t.Helper()
	cmd := newCommand()
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	cfg := config.DefaultConfig()
	if err := applyFlags(cmd, cfg); err != nil {
		t.Fatalf("applyFlags %v: %v", args, err)
	}
	return cfg
}

// TestApplyFlags pins the booleans that used to be no-ops: --docker=false and
// --ci=none must switch the feature off, and an unknown --ci value must fail.
func TestApplyFlags(t *testing.T) {
	t.Parallel()
	if cfg := parseNewFlags(t, "--transport=http", "--docker=false", "--ci=none"); cfg.IncludeDocker || cfg.IncludeCI {
		t.Errorf("docker=%v ci=%v, want both false", cfg.IncludeDocker, cfg.IncludeCI)
	}
	if cfg := parseNewFlags(t, "--transport=http"); !cfg.IncludeDocker || !cfg.IncludeCI {
		t.Errorf("defaults: docker=%v ci=%v, want both true", cfg.IncludeDocker, cfg.IncludeCI)
	}
	cmd := newCommand()
	if err := cmd.ParseFlags([]string{"--ci=gitlab"}); err != nil {
		t.Fatal(err)
	}
	if err := applyFlags(cmd, config.DefaultConfig()); err == nil {
		t.Error("--ci=gitlab accepted")
	}
	if cmd.Flags().Lookup("type") != nil || cmd.Flags().Lookup("grpc-gateway") != nil {
		t.Error("dead flags --type / --grpc-gateway still registered")
	}
}

// TestCheckTargetDir pins the overwrite guard: Generate writes file by file
// and would silently clobber edits in an existing project.
func TestCheckTargetDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	empty := filepath.Join(root, "empty")
	full := filepath.Join(root, "full")
	for _, d := range []string{empty, full} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "keep.go"), []byte("package keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkTargetDir(missing, false); err != nil {
		t.Errorf("missing dir: %v", err)
	}
	if err := checkTargetDir(empty, false); err != nil {
		t.Errorf("empty dir: %v", err)
	}
	if err := checkTargetDir(full, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("non-empty dir without --force: want error mentioning --force, got %v", err)
	}
	if err := checkTargetDir(full, true); err != nil {
		t.Errorf("non-empty dir with --force: %v", err)
	}
}
