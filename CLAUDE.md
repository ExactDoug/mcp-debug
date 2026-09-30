# Project overview
The ./README.md provides an overview of this project.

> **Status is not tracked here — see [PLAN.md](PLAN.md)** (sequencing + rationale) and
> [GitHub Issues](https://github.com/ExactDoug/mcp-debug/issues) (item state).
> This is a **public** fork of standardbeagle/mcp-debug: never commit internal hostnames,
> credentials, or deployment runbooks.

@README.md

## Potentially out-dated README.md contents

** IMPORTANT **
We are actively implementing new features in this project and have not updated all the docs yet.
Therefore the contents of the README.md as provided above are just for reference to provide
a good foundational understanding of the conceptual functionality of this project (or as the
project was a day or two ago).

** Double-Check **
For any work you may do, you must NOT consider the README.md contents as fully accurate,
up-to-date, or authoratative.

Double-check and verify actual state before doing any work.

You can also look at recent git commits.

And also use claude-mem tool to get an excellent understanding of recent work, developments & plans.

# Build Path

`cd /path/to/mcp-debug`  <---- replace this with actual path to the project.
(if already in the project root directory, the above command is unnecessary)

go build -o ./bin/mcp-debug .

## Build to ./bin/

Do not build/publish to ./

Only build/publish to ./bin/

(our MCP client is pointed at `./bin/mcp-debug`, so that is where you must create the build).

## Building the deployed binary — build from a clean `main`, with version info

Many local MCP clients across other projects run `bin/mcp-debug` directly, so this binary is
effectively deployed. Two rules, both learned the hard way:

- **Build from a clean checkout of the default branch, never from a working tree with uncommitted
  changes.** From March to September 2026 the deployed binary silently contained uncommitted work
  (now PR #18); consumers came to depend on behaviour that was never in git, and broke when a
  clean rebuild dropped it. Use `git archive origin/main | tar -x -C <tmpdir>` or a worktree.
- **Stamp the version** so `bin/mcp-debug --version` identifies what is running:
  ```bash
  go build -ldflags "-X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.GitCommit=$(git rev-parse HEAD)" -o bin/mcp-debug.new .
  ```
- **Replace atomically:** build to `bin/mcp-debug.new`, keep a `bin/mcp-debug.bak-<timestamp>`,
  then `mv -f bin/mcp-debug.new bin/mcp-debug`. Running proxies keep the old inode and pick up the
  new build on their next restart.

## Operational gotchas (proxy behaviour consumers trip over)

- **Env inheritance defaults to `tier1`** (PATH, HOME, …). A variable set in the MCP client's
  environment does **not** reach a stdio server unless the server config lists it under `env:`
  (`KEY: "${KEY}"`) or `inherit.extra`. `${KEY}` expands from mcp-debug's *own* environment; an
  unset variable becomes `""` (#17).
- **One token file and one dashboard port per proxy config.** Two proxies sharing an OAuth
  `token_file` revoke each other's rotating refresh tokens (#15); two proxies wanting the same
  dashboard port leave the second one headless while OAuth callbacks go to the first (#16). The DCR
  client in the token file is bound to the dashboard port it registered with, so pin it with
  `dashboard.port` + `port_range: 1`.
- **A token file without `client_id` cannot be refreshed** — the next refresh re-registers a new DCR
  client and needs a browser login. Builds before `210cb08` dropped the client on every refresh (#14),
  so old token files may be in that state. Back up a token file (0600) before experiments that refresh it.
- **Root checkout stays on `main`**; feature work goes in `.worktrees/<name>`.
