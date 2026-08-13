-- 010: KYC submissions, KYC-tiered withdrawal limits, numeric UID sequence
-- and the admin price-adjustment layer. Fully idempotent (IF NOT EXISTS /
-- ON CONFLICT DO NOTHING) so re-runs are safe.

-- Numeric UID generation: auth_handler scrambles nextval() through a Feistel
-- network so issued IDs are non-enumerable 9..11 digit numbers.
CREATE SEQUENCE IF NOT EXISTS users_uid_seq START 100000000;

-- KYC verification level per user (0 = unverified, 1 = verified).
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_level SMALLINT NOT NULL DEFAULT 0;

-- Manual-review KYC submissions. doc_front/doc_back hold relative paths to
-- the on-disk identity documents (data/kyc/{user_id}/...).
CREATE TABLE IF NOT EXISTS kyc_submissions (
    id            TEXT PRIMARY KEY,
    user_id       TEXT REFERENCES users(id),
    full_name     TEXT,
    id_number     TEXT,
    doc_front     TEXT,
    doc_back      TEXT,
    status        TEXT NOT NULL DEFAULT 'pending', -- pending|approved|rejected
    reject_reason TEXT,
    reviewer_id   TEXT,
    submitted_at  BIGINT,
    reviewed_at   BIGINT
);
CREATE INDEX IF NOT EXISTS idx_kyc_user ON kyc_submissions(user_id, submitted_at DESC);

-- Daily withdrawal limits (USDT-equivalent) per KYC level.
CREATE TABLE IF NOT EXISTS platform_limits (
    kyc_level        SMALLINT PRIMARY KEY,
    daily_limit_usdt NUMERIC(40,18),
    updated_at       BIGINT
);
INSERT INTO platform_limits (kyc_level, daily_limit_usdt, updated_at) VALUES (0, 1000, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000) ON CONFLICT DO NOTHING;
INSERT INTO platform_limits (kyc_level, daily_limit_usdt, updated_at) VALUES (1, 50000, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000) ON CONFLICT DO NOTHING;

-- Per-user per-asset per-day withdrawal usage (USDT-equivalent), maintained
-- atomically together with the fund reservation.
CREATE TABLE IF NOT EXISTS withdrawal_daily_usage (
    user_id TEXT NOT NULL,
    asset   TEXT NOT NULL,
    day     TEXT NOT NULL, -- UTC calendar day, yyyy-mm-dd
    used    NUMERIC(40,18) NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, asset, day)
);

-- Admin price-adjustment layer applied to every outbound market quote:
-- price' = price * multiplier + offset. Column is quoted because "offset"
-- is a reserved word in PostgreSQL.
CREATE TABLE IF NOT EXISTS price_adjustments (
    pair       TEXT PRIMARY KEY,
    multiplier NUMERIC(20,8) NOT NULL DEFAULT 1,
    "offset"   NUMERIC(40,18) NOT NULL DEFAULT 0,
    updated_by TEXT,
    updated_at BIGINT
);
