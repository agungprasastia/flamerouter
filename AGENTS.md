# FlameRouter — Agent Instructions

## What this is

Local AI routing gateway: one OpenAI-compatible surface (`/v1/*`) + management APIs (`/api/*`) + optional dashboard SPA.

| | |
|--|--|
| Port | `20128` (`PORT`) |
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
