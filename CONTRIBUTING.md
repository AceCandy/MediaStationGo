# Contributing to MediaStationGo

Thanks for taking the time to contribute! This document is intentionally short —
prefer asking in an issue/PR over re-reading rules.

## Local development

```bash
# Backend (Go 1.25+ required)
make dev               # MEDIASTATION_APP_DEBUG=true go run ./cmd/server

# Frontend (Node 20+ required)
make dev-web           # vite dev server on :3000, proxies /api -> :8080
```

Default admin: `admin / admin123` — change it after first login.

## Code style

- **Go**: keep packages small, services thin, errors wrapped with `%w`. Run
  `go vet ./...` and `go test ./...` before pushing.
- **TypeScript**: `strict: true` is on. Prefer `interface` over `type` for
  object shapes. Components live next to their page when not reused.
- Comments should explain *why*, not *what*. Avoid emoji in code.

## Commit / PR conventions

- Conventional commits encouraged: `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`.
- One concern per PR. Update the README/docs when behaviour changes.
- CI must pass: `go vet`, `go test`, frontend `tsc -b && vite build`.

## Issue triage

When filing a bug please include:

1. The steps to reproduce.
2. The expected and actual behaviour.
3. Logs from `docker logs mediastation-go` (or stdout if running locally).
4. Your deployment context (NAS model, OS, Docker version).

Thanks for keeping the project healthy!
