# FlameRouter — Agent Instructions

Go rewrite of 9router. **100% feature parity with 9router — no behavior differences allowed.**

## What this is

FlameRouter is a local AI routing gateway. One OpenAI-compatible endpoint (`/v1/*`) routes traffic across 40+ providers with format translation, model combo fallback, multi-account fallback, and usage tracking.

- Default port: `20128`
- Data dir: `~/.flamerouter` (override via `DATA_DIR`)
- API key format: `sk-{machineId}-{keyId}-{crc8}` (HMAC-SHA256 CRC)

## Commands

```bash
# run
go run ./cmd/flamerouter

# vet + test (MUST pass before any commit)
rtk go vet ./...
rtk go test ./...

# single test
rtk go test ./internal/translator/...
```

## Architecture

Request flow: `gateway` → `handlers/chat.go` → detect source format → translate request → `executor.Execute` → translate response → SSE back.

Two authoritative references:
- `9router/open-sse/AGENTS.md` — routing/translation engine conventions
- `9router/CLAUDE.md` — full system lifecycle

### Directory map

- `cmd/flamerouter/` — entrypoint
- `internal/config/` — env + config loading
- `internal/store/` — SQLite (modernc.org/sqlite), migrations, repos
- `internal/auth/` — API key HMAC, JWT
- `internal/gateway/` — HTTP server, route wiring
- `internal/opensse/` — provider-agnostic engine (executors, handlers, fallback, streaming)
- `internal/translator/` — format translation (request + response, concerns, schema)
- `internal/translator/schema/` — role/block/finish enums (NEVER hardcode strings — use these)

## Rules

1. **Parity with 9router is non-negotiable.** Every translator concern, fallback behavior, and schema constant must match 9router exactly. If 9router uses `MODEL_FALLBACK = "unknown"`, use `"unknown"`, not a model name.
2. **Config-driven, not hardcoded.** Use `internal/translator/schema/` constants for roles, blocks, finish reasons. Use `internal/config/` for magic values.
3. **Translator pipeline pivots through OpenAI.** Source → OpenAI → target. Direct route only when registered explicitly.
4. **Fail-open on RTK/hooks.** Errors return null, body untouched. Never throw.
5. **No `github.com/` prefix in module path.** Module is just `flamerouter`.

## Parity Checklist

This is a rewrite — every feature, every UI page, every provider must exist in Go.

### Backend (Tasks 1–15) — done

- [x] All `/v1/*` endpoints (chat, messages, responses, embeddings, TTS, STT, images, video, search, fetch)
- [x] 100+ provider registry with capabilities
- [x] 20+ executors (Github, GeminiCLI, Codex, Kiro, Cursor, Vertex, Antigravity, Azure, etc.)
- [x] OAuth (20+ providers) + token refresh (+ device flow github/copilot/qwen/kiro)
- [x] RTK token compression (12 filters, caveman, ponytail, headroom, pxpipe hooks)
- [x] Proxy pools, tunnels, MITM API
- [x] Usage charts, pricing, per-provider quota APIs
- [x] Media providers (TTS, STT multipart, image, embedding, video routes)
- [x] Search + fetch (searxng + provider + direct fetch SSRF-safe)
- [x] CLI tools + MCP bridge APIs
- [x] Env parity + account strategy from settings (`fallbackStrategy`)
- [x] Backend route table audit (`route_table_test.go`)

### Deferred

- [x] Dashboard SPA MVP (login, shell, providers, usage, keys, settings) — phase 2 open (combos, quota, chat playground, etc.)
- [ ] Docker packaging (optional)

Minor API gaps: translator load/save/stream, proxy-pool test — see `docs/superpowers/plans/parity-backend-routes.md`.

Full plan: `docs/superpowers/plans/2026-07-20-flamerouter-full-parity.md`

## Testing

- `rtk go test ./...` — must be all green
- `rtk go vet ./...` — must be clean
- No external services required for unit tests
