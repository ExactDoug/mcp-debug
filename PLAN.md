# mcp-debug (ExactDoug fork) — plan

Status and sequencing for this fork live here. Item-level state (open/closed, discussion) lives in
[GitHub Issues](https://github.com/ExactDoug/mcp-debug/issues) — this file references issues by
number and never restates whether they are open or closed. `CLAUDE.md` is the durable brief.

## Where things stand (2026-09-28)

- `main` = `1fedda3` (PR #11, RFC 9728 path-aware resource-metadata discovery). HTTP-stream +
  OAuth 2.1 + DCR + dashboard-initiated auth are shipped and verified against a FastMCP (Azure
  OAuth proxy) server: discovery → token refresh → tool discovery all work.
- `bin/mcp-debug` is built from `main` with `-ldflags` version info (`--version` shows the commit).
  Before 2026-09-27 the deployed binary was built from a working tree that carried uncommitted
  work (see Track C) — see `CLAUDE.md` "Building" for why this matters.

## Track A — OAuth token store correctness (next)

Order matters: #14 first, because every token refresh currently corrupts the token file.

1. **#14** — refresh drops the DCR `client_id`/`client_secret` from the token file. Two-line fix in
   `refreshAccessToken` + a test that refreshes and asserts the stored client credentials survive.
   Until fixed, the next proxy start after any refresh forces a new browser login and a new DCR
   registration.
2. **#15** — token file shared by multiple proxy processes (read once, unlocked writes, rotating
   one-time-use refresh tokens revoke each other). Re-read under `flock` before refresh; atomic
   writes; pick up tokens obtained by another process.
3. **#16** — dashboard port collision: second proxy runs headless silently and OAuth callbacks go to
   the other process. Make it visible (status field / tool error), optional `dashboard.required`.
4. **#12, #13** — RFC 9728 discovery follow-ups from PR #11 review.

Workaround until #15/#16 land: give every proxy config its **own** `token_file` and a distinct
fixed dashboard port with `port_range: 1`.

## Track B — config/env ergonomics

- **#17** — unset `${VAR}` silently expands to `""` (and blocks a server's `.env` fallback). Warn
  at load; omit rather than set empty.
- **#20** — one config-path resolver for proxy mode and `config` subcommands
  (`--config` > `MCP_CONFIG_PATH` > `.mcp-debug.yaml` > `config.yaml`).

## Track C — config auto-discovery (parked)

- Draft **PR #18** (`feat/config-autodiscovery`, worktree `.worktrees/config-autodiscovery`)
  preserves March 2026 work that was never committed (carried by stash/pop onto the PR #10 branch
  and left out of its commit). **Do not merge as-is** — rework per **#19** (never rename files,
  revert the missing-file→empty-config loader change, shared resolver from #20, tests).
- Consequence already felt: consumers whose `--config` pointed at a non-existent `config.yaml`
  "worked" only because the uncommitted loader change started them with zero servers; since the
  2026-09-27 rebuild they fail to start (correct behaviour — fix the consumer config).

## Track D — repo hygiene

- **#21** — stop tracking compiled test-server binaries; build from source.
- **#22** — `.gitignore`: all of `/bin/`, generated subdirectory `CLAUDE.md`, `.mcp-debug.yaml`.
- Branch auto-delete on merge is **off** for this repo; delete merged branches manually
  (`gh pr merge --delete-branch`) or enable the setting.
