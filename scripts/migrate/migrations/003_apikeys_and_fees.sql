-- 003: align api_keys schema with the rest of the codebase.
-- The original 001_init.sql used TEXT[] for permissions and TIMESTAMPTZ for
-- timestamps, which is inconsistent with the BIGINT timestamps used elsewhere
-- and harder to consume from Go without array scanning. Drop and recreate so
-- the schema matches pg_apikey.go.
DROP TABLE IF EXISTS api_keys;
CREATE TABLE api_keys (
    key_id      TEXT PRIMARY KEY,
    secret      TEXT NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users(id),
    permissions TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  BIGINT NOT NULL,
    expires_at  BIGINT
);
CREATE INDEX idx_apikeys_user ON api_keys(user_id);
CREATE INDEX idx_apikeys_active ON api_keys(active) WHERE active = true;

-- 004: add a ledger entry linking transactions to trades so we can reconcile
-- wallet balances against the matching engine. The transactions table already
-- stores per-user ledger entries, and this optional column links a trade-ledger row
-- back to its trade for audit/reconciliation.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS trade_id TEXT REFERENCES trades(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tx_trade ON transactions(trade_id) WHERE trade_id IS NOT NULL;

-- 005: support soft-deleted / archived orders (kept for audit, hidden from
-- active queries). Mirrors how Binance keeps historical orders.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_orders_user_active ON orders(user_id) WHERE archived = false;

-- 006: per-pair fee schedule stored alongside the engine. The wallet service
-- reads this table on startup to build its StaticFeeSchedule.
CREATE TABLE IF NOT EXISTS fee_schedule (
    pair        TEXT PRIMARY KEY,
    taker_rate  NUMERIC(10,6) NOT NULL DEFAULT 0.001,
    maker_rate  NUMERIC(10,6) NOT NULL DEFAULT 0.001,
    updated_at  BIGINT NOT NULL
);

-- Seed default fees for common pairs (10 bps taker / 10 bps maker).
INSERT INTO fee_schedule (pair, taker_rate, maker_rate, updated_at) VALUES
    ('BTC/USDT', 0.001, 0.001, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000),
    ('ETH/USDT', 0.001, 0.001, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000),
    ('SOL/USDT', 0.0015, 0.001, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000),
    ('BNB/USDT', 0.001, 0.001, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000),
    ('ADA/USDT', 0.0015, 0.001, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000000000)
ON CONFLICT (pair) DO NOTHING;
