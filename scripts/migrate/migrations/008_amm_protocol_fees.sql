-- 008: accumulate swap protocol fees per pool.
-- Since the x*y=k fix, the wallet debits the full amountIn but only
-- amountIn * (1 - fee_rate) enters the pool reserves; the remainder is the
-- protocol fee. These columns record where it goes (per token), so fees no
-- longer vanish from the ledger. Defaults to 0 keep pre-existing rows valid.
ALTER TABLE amm_pools ADD COLUMN IF NOT EXISTS protocol_fees0 NUMERIC(40,18) NOT NULL DEFAULT 0;
ALTER TABLE amm_pools ADD COLUMN IF NOT EXISTS protocol_fees1 NUMERIC(40,18) NOT NULL DEFAULT 0;
