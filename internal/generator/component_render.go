package generator

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// skelFS holds the .tmpl files that back `nova add`. Each command owns one
// subdirectory (e.g. skel/worker/), keeping a command's files together and
// separate from the `nova new` template tree.
//
//go:embed all:skel
var skelFS embed.FS

// renderSpec maps one embedded template to an output path.
type renderSpec struct {
	tmpl         string // path within componentTemplateFS
	outRel       string // output path relative to baseDir
	skipIfExists bool   // for shared/project-level files — don't clobber on re-run
}

// renderTemplates executes each spec against data and writes it under baseDir.
// skipIfExists files already present are left untouched (so adding a second
// feature doesn't overwrite shared scaffolding the user may have edited).
func (g *ComponentGenerator) renderTemplates(specs []renderSpec, data any) error {
	for _, s := range specs {
		out := filepath.Join(g.baseDir, s.outRel)
		if s.skipIfExists {
			if _, statErr := os.Stat(out); statErr == nil {
				fmt.Fprintf(os.Stdout, "   ↩︎  exists, skipped %s\n", s.outRel)
				continue
			}
		}
		rendered, err := renderTemplateString(s.tmpl, data)
		if err != nil {
			return err
		}
		if wErr := writeFile(out, rendered); wErr != nil {
			return wErr
		}
		fmt.Fprintf(os.Stdout, "   📄 %s\n", s.outRel)
	}
	return nil
}

// renderTemplateString executes a skel template against data and returns the
// rendered source without writing it. Used by callers that post-process the
// output (e.g. merging generated declarations into an existing file).
func renderTemplateString(tmpl string, data any) (string, error) {
	raw, err := skelFS.ReadFile(tmpl)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", tmpl, err)
	}
	t, err := template.New(filepath.Base(tmpl)).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", tmpl, err)
	}
	var buf bytes.Buffer
	if exErr := t.Execute(&buf, data); exErr != nil {
		return "", fmt.Errorf("execute template %s: %w", tmpl, exErr)
	}
	return buf.String(), nil
}
