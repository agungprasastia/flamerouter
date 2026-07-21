# Contributing

Thanks for helping improve FlameRouter.

## Prerequisites

- Go (see `go.mod` toolchain version)
- Node.js 20+ only when working on `web/`

## Workflow

1. Fork / branch from `main`.
2. Make focused changes (one concern per PR when possible).
3. From **repo root** (where `go.mod` is):

   ```bash
   go vet ./...
   go test ./... -count=1
   ```

4. If you touch `web/`:

   ```bash
   cd web && npm ci && npm run build
   ```

   That copies the SPA into `internal/gateway/ui/dist` for embedding.

5. Open a PR with a short summary of **what** and **why**.

## Conventions

- **Module path:** `flamerouter` only (no `github.com/` import prefix in app code).
- **CLI:** `serve` and `version` subcommands — do not assume bare `go run` without args.
- **Parity:** Backend behavior should match 9router unless the PR explicitly documents a deliberate change.
- **Secrets:** Never commit `.env`, real OAuth secrets beyond the intentional desktop-app constants already in tree, or live API keys.
- **Commits:** Prefer [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `chore:`, …).

## Architecture notes for agents/humans

See [AGENTS.md](AGENTS.md) for package boundaries, public API auth paths, and SPA embed rules.
