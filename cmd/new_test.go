package cmd

import (
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
