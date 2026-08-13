-- Multi-account wallets: adds the account_type dimension (spot / futures /
-- funding) to the wallets table.
--
--  1. New NOT NULL column defaulting to 'spot' so every existing row is
--     backfilled automatically.
--  2. The old UNIQUE(user_id, asset) is replaced by
--     UNIQUE(user_id, asset, account_type) so each account keeps its own row.
--  3. A CHECK constraint restricts account_type to the known set.
--
-- transactions.wallet_id keeps referencing wallets(id); no change needed
-- there because rows are added, not split.

ALTER TABLE wallets ADD COLUMN IF NOT EXISTS account_type TEXT NOT NULL DEFAULT 'spot';
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_user_id_asset_key;
ALTER TABLE wallets ADD CONSTRAINT wallets_user_asset_account_uniq UNIQUE (user_id, asset, account_type);
ALTER TABLE wallets ADD CONSTRAINT wallets_account_type_check CHECK (account_type IN ('spot', 'futures', 'funding'));
CREATE INDEX IF NOT EXISTS idx_wallets_user_account ON wallets(user_id, account_type);
