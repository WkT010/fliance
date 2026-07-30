-- 005: add futures positions and orders persistence.
CREATE TABLE IF NOT EXISTS futures_positions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    pair TEXT NOT NULL,
    side TEXT NOT NULL,
    leverage INT NOT NULL,
    margin_mode TEXT NOT NULL,
    entry_price NUMERIC(40,18) NOT NULL,
    mark_price NUMERIC(40,18) NOT NULL,
    quantity NUMERIC(40,18) NOT NULL,
    margin NUMERIC(40,18) NOT NULL,
    pnl NUMERIC(40,18) NOT NULL DEFAULT 0,
    pnl_pct NUMERIC(40,18) NOT NULL DEFAULT 0,
    liq_price NUMERIC(40,18) NOT NULL,
    tp_price NUMERIC(40,18),
    sl_price NUMERIC(40,18),
    status TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_futures_positions_user ON futures_positions(user_id);
CREATE INDEX IF NOT EXISTS idx_futures_positions_status ON futures_positions(status);

CREATE TABLE IF NOT EXISTS futures_orders (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    pair TEXT NOT NULL,
    side TEXT NOT NULL,
    type TEXT NOT NULL,
    price NUMERIC(40,18),
    stop_price NUMERIC(40,18),
    quantity NUMERIC(40,18) NOT NULL,
    tp_price NUMERIC(40,18),
    sl_price NUMERIC(40,18),
    leverage INT NOT NULL,
    margin_mode TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_futures_orders_user ON futures_orders(user_id);
CREATE INDEX IF NOT EXISTS idx_futures_orders_status ON futures_orders(status);
