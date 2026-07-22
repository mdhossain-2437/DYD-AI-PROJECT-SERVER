-- 0001_init.sql — DYD AI Course application schema.
--
-- PII PROTECTION MODEL
-- --------------------
-- Personally-identifiable fields (name, phone, email, address, dob, education)
-- are stored ENCRYPTED (AES-256-GCM, base64 text) — columns suffixed `_enc`.
-- The application layer encrypts on write and decrypts on read; the database
-- never sees plaintext PII. To still allow lookup-by-phone without storing the
-- phone in the clear, we store a keyed HMAC "blind index" (`phone_bidx`) and a
-- second index over phone+dob used by the anti-enumeration admit-card lookup.
--
-- A raw dump of this database (without the app's encryption/index keys) leaks
-- no applicant PII.

CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- for gen_random_uuid()

-- ---- applications -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS applications (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Encrypted PII (base64 AES-256-GCM). Never plaintext.
    name_enc           TEXT NOT NULL,
    phone_enc          TEXT NOT NULL,
    email_enc          TEXT NOT NULL DEFAULT '',
    dob_enc            TEXT NOT NULL DEFAULT '',
    address_enc        TEXT NOT NULL DEFAULT '',
    education_enc      TEXT NOT NULL DEFAULT '',
    photo_url          TEXT NOT NULL DEFAULT '',

    -- Non-PII, queryable/analytics-safe geo fields.
    division           TEXT NOT NULL,
    district           TEXT NOT NULL,

    -- Blind indexes (keyed HMAC hex). Deterministic, non-reversible.
    phone_bidx         TEXT NOT NULL,           -- lookup by phone
    phone_dob_bidx     TEXT NOT NULL DEFAULT '', -- anti-enumeration lookup (phone+dob)

    -- Program lifecycle.
    batch              TEXT NOT NULL DEFAULT '',
    roll_no            TEXT,
    admission_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
    status             TEXT NOT NULL DEFAULT 'submitted', -- submitted|reviewed|selected|rejected

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One application per phone number (the blind index enforces it without
-- exposing the phone). Partial-safe: phone_bidx is always present.
CREATE UNIQUE INDEX IF NOT EXISTS ux_applications_phone_bidx
    ON applications (phone_bidx);

CREATE INDEX IF NOT EXISTS ix_applications_phone_dob_bidx
    ON applications (phone_dob_bidx);

CREATE INDEX IF NOT EXISTS ix_applications_created_at
    ON applications (created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS ux_applications_roll_no
    ON applications (roll_no) WHERE roll_no IS NOT NULL;

-- ---- contact_messages -------------------------------------------------------
CREATE TABLE IF NOT EXISTS contact_messages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name_enc     TEXT NOT NULL,
    email_enc    TEXT NOT NULL DEFAULT '',
    phone_enc    TEXT NOT NULL DEFAULT '',
    subject      TEXT NOT NULL DEFAULT '',
    message_enc  TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_contact_created_at ON contact_messages (created_at DESC);

-- ---- newsletter_subscribers -------------------------------------------------
CREATE TABLE IF NOT EXISTS newsletter_subscribers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email_enc    TEXT NOT NULL,
    email_bidx   TEXT NOT NULL,          -- dedupe without storing plaintext
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_newsletter_email_bidx
    ON newsletter_subscribers (email_bidx);
