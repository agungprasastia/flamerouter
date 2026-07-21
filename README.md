# FlameRouter

[![CI](https://github.com/agungprasastia/flamerouter/actions/workflows/ci.yml/badge.svg)](https://github.com/agungprasastia/flamerouter/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Local AI routing gateway** — one OpenAI-compatible endpoint, many providers.

FlameRouter is a pure-Go rewrite of 9router. It runs as a single process on your machine: API gateway, credential management, usage tracking, and an optional embedded dashboard.

| | |
|--|--|
| Default port | `20128` |
| Data directory | `~/.flamerouter` (override with `DATA_DIR`) |
| Binary entry | `flamerouter serve` · `flamerouter version` |
| Module | `flamerouter` (no CGO; SQLite via `modernc.org/sqlite`) |
| License | [MIT](LICENSE) |

---

## Features

- **OpenAI-compatible API** — `/v1/chat/completions`, messages, responses, embeddings, images, audio (TTS/STT), video, search, fetch
- **Multi-provider routing** — large provider registry, format translation, combo / account fallback
- **Auth** — dashboard session (JWT cookie), optional API keys (`sk-…`), OAuth for many providers
- **Ops** — usage stats/charts, pricing hooks, proxy pools, tunnels, MITM/headroom/pxpipe APIs (backend)
- **Dashboard SPA** — login, providers, usage, keys, settings (embedded in the binary)

---

## Quick start

### Requirements

- [Go](https://go.dev/dl/) 1.22+ (module targets a recent Go release; use a current toolchain)
- Optional: Node.js 20+ only if you develop or rebuild the dashboard

### Run (development)

```bash
# optional: copy env
cp .env.example .env   # Windows: copy .env.example .env

go run ./cmd/flamerouter serve
# or: make serve
```

- Gateway + dashboard: **http://localhost:20128**
- Login password: value of `INITIAL_PASSWORD` (default in code: `123456` if unset — **change it**)

```bash
go run ./cmd/flamerouter version
```

### Build

```bash
# API + currently embedded UI
go build -o flamerouter ./cmd/flamerouter
# Windows: go build -o flamerouter.exe ./cmd/flamerouter
# or: make build

./flamerouter serve
```

To refresh the embedded UI after changing `web/`:

```bash
make ui-build          # or: cd web && npm install && npm run build
go build -o flamerouter ./cmd/flamerouter
```

`npm run build` compiles the Vite app and copies assets into `internal/gateway/ui/dist` (embedded at compile time).

---

## Configuration

Environment variables are documented in [`.env.example`](.env.example). Important ones:

| Variable | Purpose |
|----------|---------|
| `PORT` | Listen port (default `20128`) |
| `DATA_DIR` | SQLite and app data (default `~/.flamerouter`) |
| `BASE_URL` | Public base URL (callbacks / links) |
| `JWT_SECRET` | Dashboard session signing |
| `INITIAL_PASSWORD` | First/bootstrap dashboard password |
| `API_KEY_SECRET` / `MACHINE_ID_SALT` | API key generation |
| `REQUIRE_API_KEY` | Require client API keys on `/v1/*` |
| `AUTH_COOKIE_SECURE` | Set secure cookie flag |
| `SHUTDOWN_SECRET` | Optional secret for remote shutdown |

OAuth client credentials for desktop-style providers are baked in (same approach as 9router desktop).

---

## Dashboard

### Production (embedded)

After `serve`, open:

- **http://localhost:20128/login**
- **http://localhost:20128/dashboard**

MVP pages: home, providers, usage, keys, settings.

### Dashboard development

Two terminals:

```bash
# 1 — API
go run ./cmd/flamerouter serve

# 2 — Vite (hot reload)
cd web
npm install
npm run dev
```

Dev UI: **http://127.0.0.1:5173**  
Vite proxies `/api`, `/v1`, `/v1beta`, and `/codex` to `http://127.0.0.1:20128`.

---

## API overview

### Inference (OpenAI-compatible)

| Method | Path |
|--------|------|
| GET | `/v1/models`, `/v1/models/info`, `/v1/models/{kind}` |
| POST | `/v1/chat/completions` |
| POST | `/v1/messages`, `/v1/messages/count_tokens` |
| POST | `/v1/responses`, `/v1/responses/compact` |
| POST | `/v1/embeddings` |
| POST | `/v1/images/generations` |
| POST | `/v1/audio/speech`, `/v1/audio/transcriptions` |
| GET | `/v1/audio/voices` |
| POST | `/v1/videos/generations` · edits · extensions; GET `/v1/videos/{id}` |
| POST | `/v1/search`, `/v1/web/fetch` |

Also: `/v1beta/*` (Gemini-style), `/codex/*` rewrite helpers.

### Management

Prefix `/api/*` — health, auth, settings, keys, combos, providers, usage, OAuth, tunnels, MITM, etc.

- **Public (no session):** e.g. `/api/health`, `/api/auth/login|logout|status`, `GET /api/settings/require-login`, and `/v1/*`
- **Most other `/api/*`:** dashboard JWT cookie (`auth_token`)
- **`GET /api/providers`:** lists **connections** as `{ "connections": [ … ] }` (secrets stripped), not the full registry catalog

Client API keys use the form `sk-{machineId}-{keyId}-{crc8}`.

---

## Project layout

```
cmd/flamerouter/     # CLI entrypoint
internal/
  gateway/           # HTTP server, SPA embed, management routes
  opensse/           # executors, chat/media handlers
  translator/        # format translation
  provider/          # provider registry
  oauth/             # OAuth flows & proxies
  store/             # SQLite
  auth/              # API keys + dashboard JWT
web/                 # Dashboard source (Vite + React)
```

---

## Development

```bash
# from repository root (directory containing go.mod)
go vet ./...
go test ./... -count=1
# or: make vet && make test

# focused
go test ./internal/gateway/ -count=1
go test ./internal/translator/...

# full local pipeline (vet, test, UI build, binary)
make all
```

- Contributing: [CONTRIBUTING.md](CONTRIBUTING.md)
- Security: [SECURITY.md](SECURITY.md)
- Agent conventions: [AGENTS.md](AGENTS.md)

---

## License

[MIT](LICENSE) © 2026 agungprasastia
