# DYD AI — API Server

Backend for the **DYD AI training-program admissions portal** (যুব উন্নয়ন অধিদপ্তর / Department of Youth Development). It backs the four public flows of the frontend: application submission, admit-card lookup, contact, and newsletter — plus the authoritative geography feed for the cascading division→district select.

Written in **Go + [Fiber](https://gofiber.io/) v2**, backed by **PostgreSQL** (via pgx) and **Redis** (for distributed rate limiting). It is a standalone service: the Next.js frontend talks to it over HTTP only, so the two can be deployed and scaled independently.

---

## Why this exists / what it guarantees

This is a **government-linked portal that stores real applicants' personal data**. The design goals, in priority order:

1. **Don't leak PII.** Every personal field (name, phone, email, DOB, address, education) is **encrypted at rest** with AES-256-GCM. Lookups happen through **blind indexes** (keyed HMAC) so we can find an application by phone without ever storing the phone in plaintext. A stolen database dump without the keys is inert.
2. **Don't lose submissions.** A form that *looks* live but silently drops data is worse than an honest "coming soon." Writes are transactional; duplicate phones return a clean 409; nothing is acknowledged to the applicant unless it was actually persisted.
3. **Survive abuse.** Cloudflare Turnstile blocks bots on every public form; Redis-backed rate limiting throttles floods **across all instances**, not per-process; strict security headers, CORS lockdown, body limits, and timeouts close the common holes.
4. **Zero-downtime operation.** Graceful shutdown drains in-flight requests on SIGTERM, so rolling deploys and autoscaling don't drop connections.

### An honest note on "millions of users / DDoS-proof / 0 downtime"

Those are **properties of the whole deployment, not of this code alone.** This repository gives you a correct, efficient, horizontally-scalable *application* — stateless, so you can run as many copies as you want behind a load balancer, with shared state pushed to Postgres and Redis. But surviving a real DDoS or millions of concurrent users also requires **infrastructure this repo can't contain**:

- A CDN / WAF in front (this is why we integrate with **Cloudflare** — see below). Volumetric attacks must be absorbed *before* they reach your origin.
- A load balancer and **multiple API replicas** (the app is stateless precisely so you can).
- A managed, replicated PostgreSQL and Redis with their own failover.
- Autoscaling + health-based routing (the `/healthz` and `/readyz` probes are built for exactly this).

The code is built to *slot into* that topology and not be the bottleneck or the weak point. It does not, by itself, make a single VM invincible. `ARCHITECTURE.md` and `SECURITY.md` are explicit about which guarantees are code and which are infra.

---

## Quick start (local, full stack)

Requires Docker.

```bash
cp .env.example .env
make keys           # prints three base64 secrets — paste them into .env
# set POSTGRES_PASSWORD and CORS_ALLOWED_ORIGINS in .env too
make up             # db + redis + api + caddy, all wired
```

The API comes up behind Caddy at `https://localhost` (self-signed locally). Health check:

```bash
curl -k https://localhost/healthz
```

### Run just the API against your own Postgres

```bash
cp .env.example .env   # fill DATABASE_URL etc.
make run
```

With `APP_ENV=development`, missing encryption keys fall back to an **insecure, deterministic dev key** so the server boots — never used in production, where a missing key is fatal.

---

## API surface

| Method | Path                    | Purpose                                            | Rate limit           |
|--------|-------------------------|----------------------------------------------------|----------------------|
| GET    | `/healthz`              | Liveness (never touches the DB)                    | exempt               |
| GET    | `/readyz`               | Readiness (pings DB + Redis)                       | exempt               |
| GET    | `/v1/geo`               | Authoritative 8-division / 64-district tree        | general              |
| POST   | `/v1/applications`      | Submit an application                              | submit (tight)       |
| POST   | `/v1/admit-card/lookup` | Look up an admit card by phone + DOB               | lookup (tight)       |
| POST   | `/v1/contact`           | Contact message                                    | submit               |
| POST   | `/v1/newsletter`        | Newsletter subscribe (idempotent)                  | submit               |

All POST endpoints require a Cloudflare Turnstile token in the `CF-Turnstile-Response` header (unless `TURNSTILE_ENABLED=false`). Validation failures return `422` with a `fields` map; duplicate application returns `409`; admit-card misses return a **deliberately generic** `404` (anti-enumeration — never reveals whether the phone or the DOB was wrong).

---

## Configuration

All config comes from the environment; see `.env.example` for every variable, its default, and what it does. Security-critical values (encryption keys in production, CORS origins, Turnstile secret) are validated at startup — the server **fails fast** rather than booting insecurely.

Generate the three required 32-byte keys with:

```bash
make keys
```

---

## Recommended production topology (Cloudflare in front)

```
            ┌─────────────┐
  Internet ─▶  Cloudflare  │  ← WAF, DDoS absorption, bot mgmt, TLS, caching
            └──────┬──────┘
                   │ (only Cloudflare IPs allowed at origin firewall)
            ┌──────▼──────┐
            │    Caddy     │  ← TLS to origin, load-balances replicas, real-IP headers
            └──────┬──────┘
        ┌──────────┼──────────┐
   ┌────▼───┐ ┌────▼───┐ ┌────▼───┐
   │ api #1 │ │ api #2 │ │ api #N │  ← stateless Go replicas (this repo)
   └────┬───┘ └────┬───┘ └────┬───┘
        └──────────┼──────────┘
          ┌────────┴────────┐
    ┌─────▼─────┐     ┌─────▼─────┐
    │ Postgres  │     │   Redis    │  ← managed, replicated; shared state
    │ (+replica)│     │ (rate lim) │
    └───────────┘     └───────────┘
```

**DDoS / high-traffic checklist** (infra, not code):
- Put Cloudflare (or equivalent CDN/WAF) in front and **lock the origin firewall to Cloudflare IP ranges** — otherwise attackers bypass the shield by hitting the origin directly.
- Enable Cloudflare rate limiting / bot fight mode as the first line; the app's rate limiter is the second.
- Run **≥2 API replicas** with `REDIS_URL` set, so rate limits and health-based routing hold across instances.
- Use managed Postgres with a **read replica** and set `DATABASE_REPLICA_URL` (admit-card lookups are served from it).
- Set generous but finite Postgres/Redis memory ceilings; the compose file already caps Redis.

See `ARCHITECTURE.md` for the request lifecycle and `SECURITY.md` for the threat model.

---

## Development

```bash
make check     # gofmt + go vet + tests (race detector)
make build     # static binary → ./out/api
make docker    # production image
```

Database schema lives in `migrations/`. In the compose setup it is applied automatically on first init of an empty data volume; against a managed database, apply it with your migration tool of choice.
