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
- `cloudflared`: optional Cloudflare Tunnel sidecar for public HTTPS ingress

Environment:

- `DATABASE_URL`
- `CODEX_HOME` (optional, defaults to `~/.codex`)
- `HTTP_ADDR` (default `:8080`)
- `POLL_INTERVAL` (default `5m`)
- `WEB_PORT` (default `4748`)
- `APP_HOSTNAME` (for example `monitor.namjaeyoun.com`)
- `CLOUDFLARE_TUNNEL_TOKEN` (required only when starting the tunnel profile)

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

Cloudflare Tunnel deploy:

1. Create or reuse a named tunnel in Cloudflare Zero Trust.
2. Add a public hostname for `monitor.namjaeyoun.com` that points to `http://webserver:80`.
3. Copy the tunnel token into `.env` as `CLOUDFLARE_TUNNEL_TOKEN`.
4. Start the stack with the tunnel profile:

```bash
cp .env.example .env
docker compose --profile tunnel up -d --build
```

Notes for the tunnel setup:

- The `cloudflared` container waits for `webserver` health before connecting.
- You can keep `WEB_PORT` published for local fallback access, or change it if `4748` is already in use.
- Public traffic should terminate at Cloudflare and then enter the internal Docker network through the tunnel.
- This repository does not provision the Cloudflare DNS/public hostname automatically; that mapping must exist in Cloudflare first.

Endpoints:

- `GET /`
- `GET /api/accounts`
- `GET /api/accounts/{id}/history?window=five_hour&limit=288`
- `GET /healthz`
