---
type: "query"
date: "2026-08-03T06:40:05.465703+00:00"
question: "Why does New() connect Project Generation Test Matrix to Architecture & Layout Docs, New Command & Config Prompts, sqlc Repository Code-Gen, Component Generator Tests, Template File Selection, CLI Command Wiring?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["New()", "ProjectConfig", "runNew()", "TestNewRejectsUnsupportedDI()", "Up-front whitelist rejection (nethttp, sqlite, mongodb)", "Generator", ".renderFile()"]
---

# Q: Why does New() connect Project Generation Test Matrix to Architecture & Layout Docs, New Command & Config Prompts, sqlc Repository Code-Gen, Component Generator Tests, Template File Selection, CLI Command Wiring?

## Answer

Expanded from original query via vocab: [new, generator, config, template, whitelist, matrix, prompt, sqlc, selection, validate, manifest, build]. Then traversed via graphify explain New() plus per-neighbor community grouping, and verified every INFERRED calls edge against source. RESULT: only 4 of 6 bridged communities are real. The AST pass resolved calls edges by bare symbol name, so unqualified New( calls were wired to generator.New. FALSE EDGES: renderTemplateString (component_render.go:58) calls template.New; generateSQLCRepository (repository_sqlc.go:51) calls errors.New; TestWorkerAppTemplatesIdentical and TestWorkerCmdTemplatesIdentical (component_test.go:484) call template.New; validateComponent (cmd/add.go:92) calls errors.New. Communities 5 (sqlc Repository Code-Gen), 7 (Component Generator Tests) and 10 (CLI Command Wiring) are therefore NOT connected to New(); betweenness 0.254 is inflated. REAL bridges: (1) cmd/new.go:96 generator.New(cfg) - the single CLI call site, passing *config.ProjectConfig; (2) 15 verified call sites in generator_test.go (L248,342,385,417,464,515,571,603,628,720,780,828,879,972) including TestNewRejectsUnsupportedDI which asserts on New's failure mode; (3) docs/01-cli-options.md 'Up-front whitelist rejection (nethttp, sqlite, mongodb)' EXTRACTED reference; (4) same-package community 8 edges (generator.go contains, Generator, .renderFile). WHY they converge: New() at generator.go:81 is a 13-line validation gate before any template path is resolved - three whitelist checks (supportedFrameworks, supportedDatabases, supportedDI) at generator.go:82-99, then a template.FuncMap and the struct. No I/O, no file list. It is the only cheap place to reject an unsupported option, which is why the CLI wants the early error, the matrix tests want a fast assertion point that does not render a project, and the docs want one function to name for up-front rejection vs silently-ignored options. CLAUDE.md states the invariant. NOTABLE: Louvain assigned New() to community 3 (test matrix), not community 8 where its own file lives - 15 test edges outweigh its 3 same-package edges.

## Outcome

- Signal: useful

## Source Nodes

- New()
- ProjectConfig
- runNew()
- TestNewRejectsUnsupportedDI()
- Up-front whitelist rejection (nethttp, sqlite, mongodb)
- Generator
- .renderFile()