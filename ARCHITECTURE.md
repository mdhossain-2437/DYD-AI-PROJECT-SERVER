# Architecture

How the DYD API is put together, why the pieces are shaped the way they are, and — importantly — **where the code's responsibility ends and infrastructure's begins.**

## Shape of the service

A single stateless Go binary (`cmd/api`) exposing an HTTP/JSON API via Fiber. All durable state lives in **PostgreSQL** (applicant data) and **Redis** (rate-limit counters). Because the process holds no state of its own, you can run any number of copies behind a load balancer — that is the foundation of both scaling and zero-downtime deploys.

```
cmd/api/main.go          wiring + lifecycle (config → deps → server → graceful shutdown)
internal/
  config/                env parsing + fail-fast validation
  logger/                zerolog; JSON in prod, console in dev; never logs PII
  crypto/                AES-256-GCM cipher, HMAC blind index, HMAC signer
  database/              pgx pools; primary + optional read replica
  redisstore/            go-redis adapter implementing fiber.Storage
  models/                request/response DTOs + validation tags
  validate/              validator/v10 wrapper + bdphone/datestr rules
  data/                  authoritative 8-division / 64-district geography
  repository/            the ONLY layer that maps PII ↔ encrypted columns/indexes
  turnstile/             Cloudflare Turnstile verification
  middleware/            recover, request-id, access log, headers, CORS, rate limit
  handlers/              thin HTTP handlers + health probes
  server/                Fiber app assembly, route table, shutdown
migrations/              SQL schema
deploy/                  Caddyfile (reverse proxy / LB / TLS)
```

## Request lifecycle

Middleware is applied in a deliberate order (see `internal/middleware`):

```
recover → request-id → access-log → security-headers → CORS → general rate-limit
   → [route: + per-endpoint rate-limit] → handler
```

1. **recover** — any panic becomes a JSON 500; a crashing handler never becomes downtime.
2. **request-id** — a UUID per request for cross-log correlation.
3. **access-log** — one structured line per request: status, latency, method, path, id, IP. Never query strings or bodies (they contain PII).
4. **security-headers** — helmet: locked-down CSP (`default-src 'none'`), HSTS (2y, preload), nosniff, `X-Frame-Options: DENY`, no-referrer, permissions policy.
5. **CORS** — only the configured frontend origins.
6. **rate limiting** — a broad per-IP budget over everything, then tighter per-endpoint limits stacked on the sensitive write/lookup paths. Redis-backed so limits hold across replicas.
7. **handler** — bind → verify Turnstile → validate → (geo pair check) → repository → map domain errors to clean JSON.

## The PII boundary (the most important design decision)

Handlers deal in **plaintext DTOs**. The database stores **only ciphertext and blind indexes**. The `repository` package is the sole translator between the two — nothing else in the codebase knows how a phone number maps to `phone_enc` / `phone_bidx`.

- **Encryption at rest:** each PII field is sealed with AES-256-GCM using a per-record random nonce, stored as `base64(nonce||ciphertext||tag)`.
- **Blind index for lookup:** to find an application by phone (or phone+DOB for admit cards) we store `HMAC-SHA256(normalized value)` in a `_bidx` column and query by it. Deterministic enough to match, keyed so it can't be reversed or precomputed without the secret.
- **Anti-enumeration:** admit-card lookup keys on a combined phone+DOB blind index and returns a generic 404 on any miss, so the endpoint can't be used to discover which phone numbers have applied.

## Read/write splitting

Writes always go to the primary pool. Reads that can tolerate replica lag (admit-card lookups) go to `Replica`, which falls back to the primary when no `DATABASE_REPLICA_URL` is set. This lets lookup traffic scale independently of the write path.

## Lifecycle & zero-downtime

`main` connects dependencies (failing fast on any that are misconfigured), starts the listener in a goroutine, and blocks on SIGINT/SIGTERM. On signal it calls `ShutdownWithContext` with the configured drain window, letting in-flight requests finish before the process exits. Combined with a load balancer that stops routing to a draining instance (via `/readyz`), rolling deploys drop zero connections.

## Where code ends and infrastructure begins

This is the honest boundary the project owner asked for.

| Property | Provided by the code | Requires infrastructure |
|---|---|---|
| PII confidentiality at rest | ✅ AES-256-GCM + blind index | Key management (KMS/secrets store) |
| Bot mitigation on forms | ✅ Turnstile verification | Cloudflare account + keys |
| Rate limiting across instances | ✅ Redis-backed limiter | Redis deployment |
| Horizontal scalability | ✅ stateless design | Load balancer + N replicas + autoscaler |
| Zero-downtime deploy | ✅ graceful drain + probes | Orchestrator that respects `/readyz` |
| Read scaling | ✅ replica-aware pools | An actual Postgres read replica |
| DDoS resilience | ⚠️ app-level rate limits only | CDN/WAF (Cloudflare), origin firewall locked to CDN IPs |
| "Millions of users" | ⚠️ not a bottleneck | Managed DB/Redis, capacity, the above |

The code is designed so that **adding the infrastructure is a configuration exercise, not a rewrite** — every hook it needs (probes, real-IP handling, replica URL, Redis URL, trusted proxies) is already present and wired through config.
