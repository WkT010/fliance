-- 012: manual deposit claims. Users submit an on-chain txid (plus an optional
-- screenshot) as proof of deposit; admins review each claim and crediting
-- happens only on approval (single transaction: claim status + spot wallet
-- credit + type=1 deposit ledger entry).
--
-- txid carries a global UNIQUE constraint so the same on-chain transaction
-- can never be credited twice — including after a rejection (a rejected
-- claim still proves the tx was seen; re-submitting it must 409).
-- Fully idempotent (IF NOT EXISTS) so re-runs are safe.

CREATE TABLE IF NOT EXISTS deposit_claims (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id),
    asset           TEXT NOT NULL,
    amount          NUMERIC(40,18) NOT NULL,
    txid            TEXT NOT NULL UNIQUE,
    screenshot_path TEXT,                    -- on-disk png/jpg, nullable
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    reject_reason   TEXT,
    reviewer_id     TEXT,
    created_at      BIGINT NOT NULL,         -- unix nanos
    reviewed_at     BIGINT
);
CREATE INDEX IF NOT EXISTS idx_deposit_claims_user ON deposit_claims(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deposit_claims_status ON deposit_claims(status, created_at DESC);
