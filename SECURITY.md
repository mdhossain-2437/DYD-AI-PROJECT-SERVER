# Security

The threat model and controls for the DYD admissions API. This service stores **real personal data for a government training program**, so the bar is "treat every record as sensitive," not "good enough for a demo."

## What we're protecting

- **Applicant PII:** name, phone, email, date of birth, home address, education, and an uploaded photo reference.
- **The integrity of admissions:** an application that was accepted must not be silently lost or altered; admit-card lookups must not become an enumeration oracle.
- **Availability:** the portal must stay up under bot abuse and traffic spikes during an application window.

## Controls in the code

### 1. Encryption of PII at rest
Every personal field is encrypted with **AES-256-GCM** (authenticated encryption) using a **per-record random nonce**. Ciphertext is stored as `base64(nonce||ciphertext||tag)`. A database dump without the key reveals nothing; tampering with ciphertext fails authentication on decrypt. Key is a base64 32-byte value supplied via `PII_ENCRYPTION_KEY` and never written to logs.

### 2. Blind-index lookup (no plaintext identifiers)
We must find applications by phone (and by phone+DOB for admit cards) without storing the phone in plaintext. We store a **keyed HMAC-SHA256** of the normalized value in a `_bidx` column and query on that. It is:
- **deterministic** — equal inputs match, so lookup works;
- **non-reversible** — reveals nothing about the input without the key;
- **not precomputable** — the HMAC key (`BLIND_INDEX_KEY`) defeats rainbow tables.

### 3. Anti-enumeration on admit-card lookup
Lookup requires **phone + DOB together** (combined blind index) and returns a **single generic 404** on any miss — never disclosing whether the phone existed or the DOB was wrong. This stops the endpoint from being used to discover which numbers have applied. The lookup path is also rate-limited tightly per IP.

### 4. Bot mitigation (Cloudflare Turnstile)
Every public form verifies a Turnstile token server-side with Cloudflare before doing any work. Bots and cheap automated abuse are rejected at the edge of our logic. Verification can be disabled only in dev; in production a missing secret with verification enabled is a fatal misconfiguration.

### 5. Rate limiting
Per-IP limits on every route, with **tighter budgets on writes and lookups** than on general reads. Backed by **Redis** so the limit is shared across all instances — a limit of 5/min means 5/min for that IP across the whole fleet, not 5×N. Health probes are exempt so orchestrators never get throttled. Exceeding a limit returns `429` with a JSON body.

### 6. Input validation
All input is validated (`validator/v10`) before it reaches the database: required fields, lengths, email format, a **Bangladeshi mobile-number** rule, and flexible date parsing. The cascading division→district pair is validated against the **authoritative 8-division / 64-district dataset** server-side — the client's selection is never trusted.

### 7. Transport & headers
Helmet sets a strict, JSON-API-appropriate policy: `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`, HSTS (2 years, preload), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a locked-down Permissions-Policy. CORS is restricted to the configured frontend origins with credentials only then allowed.

### 8. Safe failure & no data loss
- Handlers acknowledge success **only after** the write is persisted; a duplicate phone returns a clean `409` rather than a second silent record.
- `recover` middleware turns any panic into a 500 instead of crashing the process.
- Internal errors return a generic message; **error detail and PII are never leaked** to clients, and the access log records neither bodies nor query strings.
- Config validation fails fast: the server refuses to start insecurely (missing keys/CORS/Turnstile in production).

### 9. Fail-fast secret handling
`PII_ENCRYPTION_KEY`, `BLIND_INDEX_KEY`, and `VERIFICATION_HMAC_KEY` must each decode to exactly 32 bytes in production or startup aborts. Placeholder (`CHANGE_ME…`) values are rejected in production. In development only, a deterministic insecure key is derived so the server boots locally — this key is never suitable for real data.

## Controls that MUST come from infrastructure

The code cannot provide these alone. They are the operator's responsibility and are non-optional for a real government deployment:

- **DDoS absorption:** a CDN/WAF (Cloudflare) in front, with the **origin firewall locked to the CDN's IP ranges** so attackers can't bypass it by hitting the origin directly. The app's rate limiter is a second line, not the first.
- **Key management:** store the three secrets in a real secrets manager / KMS, not a file on the box. Rotate on a schedule and on suspected compromise. (Note: rotating `PII_ENCRYPTION_KEY` requires a re-encryption migration; rotating `BLIND_INDEX_KEY` requires rebuilding the index columns.)
- **TLS everywhere:** Caddy terminates TLS and auto-renews; enforce TLS from CDN→origin too.
- **Database security:** managed Postgres with encryption at rest, network isolation (private subnet, no public ingress), least-privilege credentials, backups, and PITR.
- **Redis security:** authenticated, network-isolated, memory-capped (the compose file caps it).
- **Least privilege & isolation:** the container runs as a non-root distroless image with no shell; keep it that way. Run behind a private network; expose only the reverse proxy.
- **Monitoring & alerting:** ship the structured logs to aggregation, alert on 5xx/429 spikes and readiness flaps.

## Data-handling notes

- **No PII in logs, ever.** The access log records request id, method, path, status, latency, and IP — never names, phones, emails, bodies, or query strings.
- **Two distinct contact channels** (government office vs. e-Learning & Earning Ltd. vendor) are a frontend content concern; the API's `/v1/contact` simply stores encrypted messages and does not merge them.
- **Retention:** define and enforce a retention policy for applicant data per the program's legal requirements (not implemented in code — an operational/policy decision).

## Reporting

Security issues should be reported privately to the project owner / DYD technical contact, not filed as public issues.
