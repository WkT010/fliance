-- AMM pools, liquidity positions, and swap history.

CREATE TABLE IF NOT EXISTS amm_pools (
    id TEXT PRIMARY KEY,
    pair TEXT NOT NULL UNIQUE,
    token0 TEXT NOT NULL,
    token1 TEXT NOT NULL,
    reserve0 NUMERIC(40,18) NOT NULL DEFAULT 0,
    reserve1 NUMERIC(40,18) NOT NULL DEFAULT 0,
    lp_shares NUMERIC(40,18) NOT NULL DEFAULT 0,
    fee_rate NUMERIC(10,9) NOT NULL DEFAULT 0.003,
    status TEXT NOT NULL DEFAULT 'active',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_amm_pools_status ON amm_pools(status);

CREATE TABLE IF NOT EXISTS amm_liquidity (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    pool_id TEXT NOT NULL REFERENCES amm_pools(id) ON DELETE CASCADE,
    shares NUMERIC(40,18) NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE(user_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_amm_liquidity_user ON amm_liquidity(user_id);
CREATE INDEX IF NOT EXISTS idx_amm_liquidity_pool ON amm_liquidity(pool_id);

CREATE TABLE IF NOT EXISTS amm_swaps (
    id TEXT PRIMARY KEY,
    pool_id TEXT NOT NULL REFERENCES amm_pools(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    token_in TEXT NOT NULL,
    token_out TEXT NOT NULL,
    amount_in NUMERIC(40,18) NOT NULL,
    amount_out NUMERIC(40,18) NOT NULL,
    fee NUMERIC(40,18) NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_amm_swaps_pool ON amm_swaps(pool_id);
CREATE INDEX IF NOT EXISTS idx_amm_swaps_user ON amm_swaps(user_id);
CREATE INDEX IF NOT EXISTS idx_amm_swaps_created ON amm_swaps(created_at DESC);
