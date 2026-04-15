# jmonitor

V1 Codex usage monitor.

Features:

- scans `CODEX_HOME/accounts/*.auth.json`
- polls Codex usage every 5 minutes
- stores 5-hour and weekly remaining percentages in PostgreSQL
- serves a local dashboard and JSON API

Docker Compose stack:

- `postgresdb`: local PostgreSQL
- `server`: poller + API/dashboard app
- `webserver`: nginx reverse proxy published to the host

Environment:

- `DATABASE_URL`
- `CODEX_HOME` (optional, defaults to `~/.codex`)
- `HTTP_ADDR` (default `:8080`)
- `POLL_INTERVAL` (default `5m`)

Run:

```bash
go run ./cmd/jmonitor
```

Run with Docker Compose:

```bash
cp .env.example .env
docker compose up --build
```

Notes:

- `server` reads Codex account snapshots from `/codex/accounts/*.auth.json`.
- In Compose, that path is backed by `${CODEX_HOME_HOST:-$HOME/.codex}` mounted read-only from the host.
- `.env` is optional. You only need `CODEX_HOME_HOST` when your Codex home is not `~/.codex`.
- Open `http://localhost:4748` after the stack is healthy.

Endpoints:

- `GET /`
- `GET /api/accounts`
- `GET /api/accounts/{id}/history?window=five_hour&limit=288`
- `GET /healthz`
