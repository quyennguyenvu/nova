# Knowledge graph (graphify)

[graphify](https://github.com/Graphify-Labs/graphify) turns this repo into a queryable knowledge
graph. Code is parsed locally with tree-sitter (deterministic, no LLM, no API cost); markdown and
other docs go through semantic extraction. Every edge is tagged `EXTRACTED` (explicit in the
source) or `INFERRED` (resolved by graphify), so the graph is auditable.

**This repo already has a graph committed.** `graphify-out/` is in git, so after cloning you can
query it immediately — you do not need to build anything. First-time setup is just installing the
tool and wiring your assistant to it.

## First-time setup

### 1. Prerequisites

| Requirement        | Minimum | Check               |
| ------------------ | ------- | ------------------- |
| Python             | 3.10+   | `python3 --version` |
| uv (recommended)   | any     | `uv --version`      |
| pipx (alternative) | any     | `pipx --version`    |

Install uv if you don't have it:

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

### 2. Install the package

The PyPI package is `graphifyy` (two y's); the command it installs is `graphify`.

```bash
uv tool install graphifyy      # recommended — isolated env
# or: pipx install graphifyy
# or: pip install graphifyy
```

Optional extras — only if you need them:

```bash
uv tool install "graphifyy[pdf]"     # PDF extraction
uv tool install "graphifyy[video]"   # video/audio transcription
uv tool install "graphifyy[neo4j]"   # Neo4j push
uv tool install "graphifyy[all]"     # everything
```

Verify:

```bash
graphify --version     # nova's graph was built with 0.9.16
```

### 3. Register graphify with your assistant

```bash
graphify install                       # Claude Code
graphify cursor install                # Cursor
graphify install --platform gemini     # Gemini CLI
graphify install --platform codex      # Codex
graphify install --platform copilot    # GitHub Copilot CLI
```

Then `/graphify` is available as a slash command. `graphify install --help` lists the full platform
set (opencode, aider, amp, droid, kiro, antigravity, …).

Optionally make it always-on for this repo:

```bash
graphify claude install     # writes a `## graphify` section to CLAUDE.md + a PreToolUse hook
```

That tells the assistant to check the graph before answering codebase questions instead of grepping
files. Nova's `CLAUDE.md` does **not** carry that section today — it's per-developer opt-in.
`graphify claude uninstall` removes it.

### 4. Install the git hooks

Git hooks live in `.git/hooks/` and are **not** committed, so every developer installs their own:

```bash
graphify hook install     # post-commit + post-checkout auto-rebuild
graphify hook status      # verify
graphify hook uninstall   # remove
```

After each commit the hook re-runs AST extraction on the changed files and rebuilds `graph.json` /
`GRAPH_REPORT.md` in a detached process — code only, no LLM, no cost, and `git commit` returns
immediately. It pins your interpreter path at install time, so re-run `hook install` if you
reinstall graphify. Changes to `docs/*.md` are _not_ picked up by the hook; refresh those with
`/graphify . --update`.

Escape hatches: `GRAPHIFY_SKIP_HOOK=1` skips one rebuild, and the rebuild log is at
`~/.cache/graphify-rebuild.log`.

### 5. (Optional) API key

You do not need an API key. Go code is AST-parsed locally for free. Only docs, PDFs, and images go
through semantic extraction, and that uses Gemini if `GEMINI_API_KEY` or `GOOGLE_API_KEY` is set —
otherwise your assistant session does the extraction itself.

Other useful env vars: `GRAPHIFY_MAX_WORKERS` (AST parallelism), `GRAPHIFY_FORCE` (allow a rebuild
that shrinks the graph), `GRAPHIFY_GEMINI_MODEL` (override the default Gemini model).

## Using the committed graph

In an assistant session, ask a normal question — with `graphify-out/graph.json` present, the skill
answers from the graph instead of rebuilding:

```text
How does nova pick which handler template to render?
What breaks if I change buildFileList?
```

From a terminal:

```bash
graphify query "what connects nova add to the layout manifest?"   # BFS traversal
graphify query "..." --dfs --budget 1500                          # DFS, capped output
graphify path "ComponentGenerator" "Manifest"                     # shortest path between nodes
graphify explain "buildFileList"                                  # plain-language node summary
graphify affected "ProjectConfig" --depth 2                       # reverse traversal — blast radius
```

Open `graphify-out/graph.html` in a browser for the interactive force-directed view, and read
`graphify-out/GRAPH_REPORT.md` for god nodes, cross-community bridges, and suggested questions.

## Keeping the graph fresh

| Situation                            | Command                                                 |
| ------------------------------------ | ------------------------------------------------------- |
| Code changed                         | Automatic via the post-commit hook                      |
| Docs / markdown changed              | `/graphify . --update` (re-extracts only changed files) |
| Manual code-only refresh             | `graphify update .`                                     |
| Communities look wrong               | `graphify cluster-only .`                               |
| Full rebuild from scratch            | `/graphify .`                                           |
| Rebuild after deleting a lot of code | `graphify update . --force`                             |

graphify refuses to overwrite `graph.json` with a **smaller** graph — that guard catches half-broken
extractions. When a shrink is intentional (you deleted files), `--force` or `GRAPHIFY_FORCE=1` is
the way through.

## What's committed vs. ignored

Committed, so a fresh clone starts with the map:

```bash
graphify-out/graph.json          # queryable graph — the important one
graphify-out/GRAPH_REPORT.md     # audit report
graphify-out/graph.html          # interactive visualization
graphify-out/manifest.json       # file manifest for --update
graphify-out/.graphify_labels.json  # community names
graphify-out/.vocab.txt          # graph vocabulary for query expansion
graphify-out/memory/             # saved Q&A results
graphify-out/reflections/        # LESSONS.md distilled from memory/
```

Ignored (see [.gitignore](.gitignore)):

```bash
graphify-out/cost.json           # per-machine token tally
graphify-out/cache/              # extraction cache — commit it to trade repo size for speed
```

Because `graph.json` is tracked, concurrent branches can conflict on it. Two ways out: take either
side and let the hook rebuild, or use graphify's union merge driver (`graphify merge-driver`, wired
up by `hook install`).

### Known gotcha in this repo

Two tracked sidecars hold **absolute paths from the machine that built the graph**:

```bash
cat graphify-out/.graphify_python   # /Users/<someone>/.local/share/uv/tools/graphifyy/bin/python
cat graphify-out/.graphify_root     # /Users/<someone>/workspace/gh_leo/nova
```

On anyone else's machine those paths don't exist. `.graphify_python` is harmless (probes are guarded
and fall through to your real interpreter), but `.graphify_root` is read by the hook's background
rebuild as the scan root, so a hook-triggered rebuild can target a directory that isn't there. They
should be local-only:

```bash
printf 'graphify-out/.graphify_python\ngraphify-out/.graphify_root\n' >> .gitignore
git rm --cached graphify-out/.graphify_python graphify-out/.graphify_root
```

Until that lands, delete both files locally after cloning — the skill and the hook regenerate them
with your own paths on the next run.

## Troubleshooting

**`graphify: command not found`** — the install dir isn't on your PATH:

```bash
uv tool update-shell     # uv installs
pipx ensurepath          # pipx installs
```

Then open a new terminal.

**Verify the install:**

```bash
graphify --version
python3 -c "import graphify; print(graphify.__file__)"
```

**PowerShell** — use `graphify .`, not `/graphify .`.

**`ERROR: Graph is empty`** — extraction produced no nodes. Usually a wrong path or an
all-files-skipped corpus; check the path argument before rebuilding.

**`refused to shrink graphify-out/graph.json`** — the shrink guard above. Re-run with `--force` if
the shrink is intentional.

**Graph integrity warnings** (dangling or collapsed edges) — inspect with:

```bash
graphify diagnose multigraph
```
