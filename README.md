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
- **Dashboard** — full-featured Next.js 16 + React 19 web interface

### At a glance

| | |
|--|--|
| Gateway Port (Backend) | `20130` (`PORT` / `BACKEND_PORT`) |
| Dashboard Port (Frontend) | `20129` (`PORT` for Next.js / `NEXT_PUBLIC_BASE_URL`) |
| Data directory | `~/.flamerouter` (override with `DATA_DIR`) |
| Backend Entry | `go run ./cmd/flamerouter serve` |
| Module | `flamerouter` (Go 1.22+, SQLite via `modernc.org/sqlite` without CGO) |

---

## Features

| Category | Details |
|----------|---------|
| **OpenAI-compatible API** | `/v1/chat/completions`, messages, responses, embeddings, images, audio (TTS/STT), video, search, fetch |
| **Multi-provider routing** | 100+ providers, format translation, combo / account fallback |
| **Auth** | Dashboard session (JWT cookie), optional API keys (`sk-…`), OAuth for many providers |
| **Ops** | Usage stats/charts, pricing hooks, proxy pools, tunnels, MITM/headroom/pxpipe APIs |
| **Dashboard UI** | Next.js App Router (login, providers, keys, combos, media providers, usage, settings) |

---

## Quick start

### Requirements

- [Go](https://go.dev/dl/) **1.22+**
- [Node.js](https://nodejs.org/) **20+** & npm

### Run (Development)

Jalankan perintah ini dari root folder untuk menyalakan **Backend Go** + **Frontend Next.js** secara otomatis:

```bash
npm install
npm run dev
```

- **Frontend Dashboard** → **http://localhost:20129**
- **Backend API Gateway** → **http://127.0.0.1:20130**
- Login password default → `123456` (dikonfigurasi di `INITIAL_PASSWORD`)

### Run Separately (Manual)

**Terminal 1 (Backend Go Gateway):**
```bash
go run ./cmd/flamerouter serve
```

**Terminal 2 (Frontend Next.js):**
```bash
npm run dev:next-only
```

### Build

```bash
# Build binary backend Go
go build -o flamerouter.exe ./cmd/flamerouter

# Build production frontend Next.js
npm run build
```

---

## Configuration

Environment variables are documented in [`.env.example`](.env.example). Important ones:

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` (Go) / `BACKEND_PORT` | `20130` | Gateway backend listen port |
| `PORT` (Next.js) | `20129` | Dashboard frontend port |
| `DATA_DIR` | `~/.flamerouter` | SQLite DB, settings, tokens storage |
| `BASE_URL` | `http://localhost:20130` | Gateway base URL |
| `JWT_SECRET` | *(auto-generated)* | Dashboard session JWT secret |
| `INITIAL_PASSWORD` | `123456` | Bootstrap dashboard password |
| `API_KEY_SECRET` | `endpoint-proxy-api-key-secret` | API key signature secret |
| `MACHINE_ID_SALT` | `endpoint-proxy-salt` | Machine ID generation salt |
| `REQUIRE_API_KEY` | `false` | Require client API keys on `/v1/*` |

---

## Dashboard

| URL | Description |
|-----|-------------|
| [http://localhost:20129/login](http://localhost:20129/login) | Login page |
| [http://localhost:20129/dashboard](http://localhost:20129/dashboard) | Dashboard home |
| [http://localhost:20129/dashboard/providers](http://localhost:20129/dashboard/providers) | Manage AI Provider Connections & OAuth |
| [http://localhost:20129/dashboard/combos](http://localhost:20129/dashboard/combos) | Model routing combo rules & fallbacks |
| [http://localhost:20129/dashboard/keys](http://localhost:20129/dashboard/keys) | Gateway API keys (`sk-...`) |
| [http://localhost:20129/dashboard/usage](http://localhost:20129/dashboard/usage) | Live usage stats & charts |
| [http://localhost:20129/dashboard/settings](http://localhost:20129/dashboard/settings) | Proxy, system, and authentication settings |

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
├── cmd/flamerouter/          # Go CLI & Gateway entrypoint (serve / version)
├── internal/
│   ├── auth/                 # API keys (sk-*) + dashboard JWT session
│   ├── clitools/             # CLI tools setup & integration helpers
│   ├── config/               # App configuration & env loader
│   ├── gateway/              # HTTP router, reverse proxies, /api/* routes
│   ├── infra/                # Headroom, proxy, tunnel, pxpipe, MITM
│   ├── mcp/                  # MCP bridge support
│   ├── netutil/              # SSRF protection, machine ID utilities
│   ├── oauth/                # OAuth configs, token refresher, device flows
│   ├── opensse/              # Core SSE executor, chat/media dispatchers
│   ├── ops/                  # Background worker & maintenance ops
│   ├── provider/             # Provider registry & model catalog (100+ entries)
│   ├── store/                # SQLite storage engine (modernc.org/sqlite, pure Go)
│   ├── tokenrefresh/         # Automatic background OAuth token refreshing
│   ├── translator/           # OpenAI/Claude/Gemini format translation
│   └── usage/                # Usage tracking, stats, and pricing calculations
├── src/                      # Next.js 16 App Router UI frontend
│   ├── app/                  # Pages: dashboard, login, providers, keys, combos...
│   ├── lib/                  # Client-side API clients & utilities
│   ├── shared/               # Shared constants, helpers, and schemas
│   └── store/                # Frontend state management (Zustand)
├── scripts/
│   └── dev.mjs               # Unified dev orchestrator (Go + Next.js concurrently)
├── public/                   # Static assets & provider logos
├── .env.example              # Environment variable reference
├── package.json              # Frontend npm dependencies & build scripts
└── go.mod                    # Module: flamerouter (Go 1.22+, no CGO)
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
