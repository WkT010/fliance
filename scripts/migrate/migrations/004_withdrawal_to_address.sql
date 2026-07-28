-- 004: add withdrawal destination address to the ledger.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS to_address TEXT;
CREATE INDEX IF NOT EXISTS idx_tx_to_address ON transactions(to_address) WHERE to_address IS NOT NULL;
