-- 0002_intake_fields.sql — extend applications with the full live intake form.
--
-- The public application form collects more than the initial schema captured:
-- gender, GPA/CGPA, passing year, computer-skill level, whether the applicant
-- owns a computer, an attendance commitment, and a (now mandatory) passport
-- photo. This migration adds those columns.
--
-- PII handling is unchanged: GPA and the photo are personal, so they are stored
-- ENCRYPTED (AES-256-GCM, base64 text) in `*_enc` columns. The categorical
-- answers (gender, computer_skill, own_computer, can_attend) and passing_year
-- are non-identifying and stored as plain canonical tokens for analytics.
--
-- Applied automatically on first init of an empty volume (compose). For an
-- already-initialized database, run this file with your migration tool.
--
-- Idempotent: safe to re-run (ADD COLUMN IF NOT EXISTS).

ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS gpa_enc        TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS photo_enc      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS gender         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passing_year   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS computer_skill TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS own_computer   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS can_attend     TEXT NOT NULL DEFAULT '';

-- photo_url from 0001 is superseded by the encrypted inline photo (photo_enc).
-- Kept (nullable/defaulted) for backward compatibility; new writes leave it ''.
