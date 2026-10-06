package generator

import (
	"fmt"
	"go/format"
	"strings"
)

// formatGoSource gofmt's rendered Go so conditional template lines cannot
// leave misaligned fields or unsorted imports in the output. Non-Go files
// pass through untouched. A parse failure means the template produced
// invalid Go, which is a generator bug worth surfacing with the file name.
func formatGoSource(outPath string, src []byte) ([]byte, error) {
	if !strings.HasSuffix(outPath, ".go") {
		return src, nil
	}
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("rendered %s is not valid Go: %w", outPath, err)
	}
	return formatted, nil
}
