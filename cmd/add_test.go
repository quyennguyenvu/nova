package cmd

import (
	"testing"

	"github.com/quyennguyenvu/nova/internal/manifest"
	"github.com/quyennguyenvu/nova/internal/prompt"
)

// TestValidateComponentNames pins the identifier rule: names flow raw into
// file paths, package names and Go identifiers, so anything that is not a Go
// identifier must stop before a file is written.
func TestValidateComponentNames(t *testing.T) {
	t.Parallel()
	m := manifest.Default()
	m.Module = "example.com/app"
	for _, bad := range []string{"../../escape", "Bad Name", "123abc", "order-item", "a.b", `x"y`} {
		c := prompt.Component{Type: "entity", Name: bad}
		if err := validateComponent(&c, m); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	for _, good := range []string{"Order", "order", "OrderItem", "HTTPServer", "V2Thing"} {
		c := prompt.Component{Type: "entity", Name: good}
		if err := validateComponent(&c, m); err != nil {
			t.Errorf("name %q rejected: %v", good, err)
		}
	}
	c := prompt.Component{Type: "entity", Name: "Order"}
	if err := validateComponent(&c, manifest.Default()); err == nil {
		t.Error("empty module path accepted; generated imports would start with a bare slash")
	}
}
