-- 015: persist the withdrawal address whitelist.
--
-- The whitelist used to live in an in-memory map inside api-gateway and was
-- lost on every restart. Admins add entries per user; withdrawals may only
-- target whitelisted addresses. Addresses are stored normalised (lower-case,
-- column `address`) for uniqueness + matching; `address_raw` keeps the
-- original spelling for display so the API contract stays unchanged.
-- Fully idempotent (IF NOT EXISTS) so re-runs are safe.

CREATE TABLE IF NOT EXISTS address_whitelist (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    asset       TEXT NOT NULL,          -- upper-case asset code
    address     TEXT NOT NULL,          -- normalised (lower-case) form
    address_raw TEXT NOT NULL,          -- original spelling as submitted
    label       TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL,        -- unix nanos
    UNIQUE (user_id, asset, address)
);
CREATE INDEX IF NOT EXISTS idx_address_whitelist_user ON address_whitelist(user_id, asset);
