# FlameRouter — Agent Instructions

## What this is

Local AI routing gateway: one OpenAI-compatible surface (`/v1/*`) + management APIs (`/api/*`) + Next.js dashboard UI.

| | |
|--|--|
| Backend Gateway Port | `20130` (`PORT`) |
| Frontend Dashboard Port | `20129` (`NEXT_PUBLIC_BASE_URL`) |
| Data | `~/.flamerouter` (`DATA_DIR`) |
| Module | `flamerouter` — **never** `github.com/...` |
| CLI | `flamerouter serve` \| `flamerouter version` (not bare `go run` without subcommand) |
| Login default | `INITIAL_PASSWORD` default **`123456`** (see `.env.example`) |
| API keys | `sk-{machineId}-{keyId}-{crc8}` |

## Commands

```powershell
# server (required subcommand)
go run ./cmd/flamerouter serve
go run ./cmd/flamerouter version

# before commit
go vet ./...
go test ./...

# focused
go test ./internal/gateway/ -run TestSPA -count=1
go test ./internal/translator/...

# dashboard SPA
cd web
npm run dev          # :5173, proxies /api /v1 /v1beta /codex → :20128
npm run build        # vite build + copies → internal/gateway/ui/dist (embed)
```

Run tests from **repo root** (where `go.mod` lives). `go test ./...` outside the module fails with “does not contain main module”.

## Layout (where to edit)

| Path | Role |
|------|------|
| `cmd/flamerouter/` | Entrypoint only |
| `internal/gateway/` | HTTP routes, SPA fallback (`spa.go`), most `/api/*` handlers |
| `internal/gateway/ui/dist/` | **Embedded** UI (`//go:embed`). Change UI via `web/` then `npm run build` |
| `web/` | Vite React SPA source (not served until built into `ui/dist`) |
| `internal/opensse/` | Executors, chat/media handlers, fallback |
| `internal/translator/` | Format translate; **`schema/`** for roles/blocks/finish — never hardcode strings |
| `internal/provider/` | Registry (provider defs) |
| `internal/oauth/` | OAuth configs + proxy (codex:1455, xai:56121) + imports |
| `internal/store/` | SQLite (`modernc.org/sqlite`), migrations |
| `internal/auth/` | API keys, JWT session cookie `auth_token`, `DashboardGuard` |
| `docs/` | **gitignored** — local plans/specs only; not on remote |

## Request flow

`gateway` → `opensse` handlers → detect format → **translate** (pivot OpenAI; direct route only if registered) → `executor.Execute` → translate response → SSE/JSON.

## Rules agents miss

1. **Parity with 9router** — same magic strings/constants as upstream (e.g. fallback `"unknown"` if 9router uses it).
2. **Fail-open RTK/hooks** — on error leave body; never throw/fail the request from compress hooks.
3. **`GET /api/providers` = connections**, shape `{connections:[...]}` (secrets stripped). Not the full registry catalog. Registry lives in `internal/provider`; SPA empty-state uses known provider ids / detail routes.
4. **Dashboard auth** — cookie JWT. Public: `/api/health`, `GET /api/settings/require-login`, `/api/auth/login|logout|status|oidc/*`, `/v1/*`, non-`/api` (SPA). Most other `/api/*` need session. SPA `fetch` must use `credentials: "include"`.
5. **SPA routing** — `handleSPA` is registered last (`/`). Must not steal `/api/`, `/v1/`, `/v1beta/`, `/codex/`. After UI changes: `cd web && npm run build` so embed updates.
6. **OAuth** — configs in `internal/oauth/types.go`. Codex/xAI fixed-port proxies; device flows use `DeviceURL` + `AuthStyle: "device"`.
7. **No CGO** — sqlite is pure Go (`modernc.org/sqlite`).

## Dashboard status

- **MVP done:** login, shell, providers, usage, keys, settings (embedded).
- **Phase 2 open:** combos, translator UI, MITM, tunnel, pxpipe, cli-tools, chat playground, media-providers, quota, full OAuth UX, etc.

## Testing notes

- Unit tests need **no** external services.
- Gateway has route-table + SPA tests; keep them green when touching routes or embed.
- Frontend: no required unit suite yet; gate is `npm run build` + manual smoke when changing `web/`.

<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
