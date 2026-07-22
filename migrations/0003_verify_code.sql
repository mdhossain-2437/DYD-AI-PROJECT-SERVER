-- 0003_verify_code.sql — stable, non-PII document verification code.
--
-- Each application gets a verification code seeded ONLY on its immutable id
-- (`admit-card|<uuid>` HMAC'd with VERIFICATION_HMAC_KEY, formatted DYD-AC-XXXXXX).
-- Because the seed is the row id — never a name, phone, or roll number — the code
-- is stable for the life of the record and leaks no PII. It is what a QR scan on
-- the printed admit card resolves to, and what GET /v1/verify looks up.
--
-- Stored (not recomputed on every read) so a lookup is a single indexed hit and
-- the code survives even if the derivation ever changes. Backfilled for any rows
-- that predate this column by the application on the next issue; NULL until then.
--
-- Idempotent: safe to re-run (ADD COLUMN / CREATE INDEX IF NOT EXISTS).

ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS verify_code TEXT NOT NULL DEFAULT '';

-- Look up an application by its verification code. Partial index skips the many
-- empty-string rows so it stays small and only covers real, issued codes.
CREATE UNIQUE INDEX IF NOT EXISTS ux_applications_verify_code
    ON applications (verify_code) WHERE verify_code <> '';
