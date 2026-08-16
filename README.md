<div align="center">

# 🔥 FlameRouter

[![CI](https://github.com/agungprasastia/flamerouter/actions/workflows/ci.yml/badge.svg)](https://github.com/agungprasastia/flamerouter/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev/dl/)
[![No CGO](https://img.shields.io/badge/No_CGO-required-yellow.svg)](https://pkg.go.dev/modernc.org/sqlite)

**Local AI routing gateway** — one OpenAI-compatible endpoint, many providers.

</div>

---

## What it does

FlameRouter is a **pure-Go rewrite** of [9router](https://github.com/agungprasastia/9router). It runs as a **single process** on your machine:

- **API gateway** — OpenAI-compatible surface (`/v1/*`) that routes to 100+ providers
- **Credential management** — OAuth flows, API key generation, provider connections
- **Usage tracking** — SQLite-backed stats, charts, pricing hooks
- **Dashboard** — embedded SPA for managing everything (React + Vite)

### At a glance

| | |
|--|--|
| Gateway Port (Backend) | `20130` (`PORT`) |
| Dashboard Port (Frontend) | `20129` (`NEXT_PUBLIC_BASE_URL`) |
| Data directory | `~/.flamerouter` (override with `DATA_DIR`) |
| Binary entry | `flamerouter serve` · `flamerouter version` |
| Module | `flamerouter` (no CGO; SQLite via `modernc.org/sqlite`) |

---

## Features

| Category | Details |
|----------|---------|
| **OpenAI-compatible API** | `/v1/chat/completions`, messages, responses, embeddings, images, audio (TTS/STT), video, search, fetch |
| **Multi-provider routing** | 100+ providers, format translation, combo / account fallback |
| **Auth** | Dashboard session (JWT cookie), optional API keys (`sk-…`), OAuth for many providers |
| **Ops** | Usage stats/charts, pricing hooks, proxy pools, tunnels, MITM/headroom/pxpipe APIs |
| **Dashboard SPA** | Login, providers, usage, keys, settings (embedded in the binary) |

---

## Quick start

### Requirements

- [Go](https://go.dev/dl/) **1.22+** (use a current toolchain)
- Node.js 20+ *(only if developing the dashboard)*

### Run (development)

Cukup jalankan satu perintah ini, **Backend Go + Frontend Next.js langsung menyala bersamaan**:

```bash
npm run dev
```

- **Frontend Dashboard** → **http://localhost:20129**
- **Backend API Gateway** → **http://127.0.0.1:20130**
- Login password default → `123456` (dikonfigurasi di `INITIAL_PASSWORD`)

### Build

```bash
# API + embedded UI
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

> `npm run build` compiles the Vite app and copies assets into `internal/gateway/ui/dist` (embedded at compile time).

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

> OAuth client credentials for desktop-style providers are baked in (same approach as 9router desktop).

---

## Dashboard

### Production (embedded)

After `serve`, open:

| URL | Description |
|-----|-------------|
| [localhost:20128/login](http://localhost:20128/login) | Login page |
| [localhost:20128/dashboard](http://localhost:20128/dashboard) | Dashboard home |

**MVP pages:** home · providers · usage · keys · settings

### Dashboard development

Two terminals:

```bash
# Terminal 1 — API
go run ./cmd/flamerouter serve

# Terminal 2 — Vite (hot reload)
cd web
npm install
npm run dev
```

| | |
|--|--|
| Dev UI | **http://127.0.0.1:5173** |
| Proxy targets | `/api`, `/v1`, `/v1beta`, `/codex` → `http://127.0.0.1:20128` |

---

## API overview

### Inference (OpenAI-compatible)

| Method | Path |
|--------|------|
| `GET` | `/v1/models`, `/v1/models/info`, `/v1/models/{kind}` |
| `POST` | `/v1/chat/completions` |
| `POST` | `/v1/messages`, `/v1/messages/count_tokens` |
| `POST` | `/v1/responses`, `/v1/responses/compact` |
| `POST` | `/v1/embeddings` |
| `POST` | `/v1/images/generations` |
| `POST` | `/v1/audio/speech`, `/v1/audio/transcriptions` |
| `GET` | `/v1/audio/voices` |
| `POST` | `/v1/videos/generations` · edits · extensions |
| `GET` | `/v1/videos/{id}` |
| `POST` | `/v1/search`, `/v1/web/fetch` |

Also: `/v1beta/*` (Gemini-style), `/codex/*` rewrite helpers.

### Management

Prefix `/api/*` — health, auth, settings, keys, combos, providers, usage, OAuth, tunnels, MITM, etc.

| Scope | Access |
|-------|--------|
| **Public (no session)** | `/api/health`, `/api/auth/login\|logout\|status`, `GET /api/settings/require-login`, `/v1/*` |
| **Dashboard session** | Most other `/api/*` (JWT cookie `auth_token`) |
| **`GET /api/providers`** | Returns **connections** as `{ "connections": [ … ] }` (secrets stripped), not the full registry |

> Client API keys: `sk-{machineId}-{keyId}-{crc8}`

---

## Project layout

```
flamerouter/
├── cmd/flamerouter/          # CLI entrypoint (serve / version)
├── internal/
│   ├── gateway/              # HTTP server, SPA embed, /api/* routes
│   │   └── ui/dist/          # Embedded SPA (go:embed) — edit via web/
│   ├── opensse/              # Executors, chat/media handlers
│   ├── translator/           # Format translation (schema/ for roles/blocks)
│   ├── provider/             # Provider registry (100+ entries)
│   ├── oauth/                # OAuth configs, proxies, device flows
│   ├── store/                # SQLite (modernc.org/sqlite)
│   └── auth/                 # API keys (sk-*) + dashboard JWT
├── web/                      # Dashboard source (Vite + React)
├── docs/                     # Local plans/specs (gitignored)
├── .env.example              # Environment variable reference
├── Makefile                  # serve / build / test / ui-build / all
└── go.mod                    # Module: flamerouter (no CGO)
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

| Link | Description |
|------|-------------|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution guidelines |
| [SECURITY.md](SECURITY.md) | Security policy |
| [AGENTS.md](AGENTS.md) | Agent conventions |

---

## License

[MIT](LICENSE) © 2026 [agungprasastia](https://github.com/agungprasastia)
