-- 009: admin action audit trail.
-- Every mutating admin operation (manual deposits, withdrawal approvals,
-- risk config changes, AMM controls, ...) records who did what, from where,
-- and whether it succeeded. Written asynchronously by the api-gateway; rows
-- are append-only.
CREATE TABLE IF NOT EXISTS audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_user_id TEXT,
    actor_email   TEXT,
    action        TEXT,
    target_type   TEXT,
    target_id     TEXT,
    ip            TEXT,
    user_agent    TEXT,
    details       TEXT,
    success       BOOLEAN,
    error         TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_ts ON audit_logs (ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
