-- 014: multi-chain deposit verification (T52).
-- Records the chain a deposit claim was submitted for, so the on-chain
-- verifier routes the txid to the correct network and manual reviewers can
-- see which chain the claim refers to.
--   network — Alchemy network slug: eth-mainnet | polygon-mainnet |
--             arbitrum-mainnet | optimism-mainnet | base-mainnet
--             (legacy rows / clients omitting the field default to
--             eth-mainnet, matching the pre-014 single-chain behaviour)
-- Fully idempotent (ADD COLUMN IF NOT EXISTS) so re-runs are safe.

ALTER TABLE deposit_claims ADD COLUMN IF NOT EXISTS network TEXT NOT NULL DEFAULT 'eth-mainnet';
