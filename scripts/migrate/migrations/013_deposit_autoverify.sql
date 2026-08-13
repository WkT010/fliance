-- 013: on-chain auto-verification for deposit claims (Alchemy).
-- Adds the bookkeeping columns the automated verifier writes when a claim is
-- checked against the chain at submission time:
--   auto_verified  — true only when the verifier approved the claim itself
--   verify_note    — human-readable outcome / failure reason (also shown to
--                    admins so manual reviewers see why a claim stayed pending)
--   verified_at    — unix nanos of the verification attempt
-- Fully idempotent (ADD COLUMN IF NOT EXISTS) so re-runs are safe.

ALTER TABLE deposit_claims ADD COLUMN IF NOT EXISTS auto_verified BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE deposit_claims ADD COLUMN IF NOT EXISTS verify_note TEXT;
ALTER TABLE deposit_claims ADD COLUMN IF NOT EXISTS verified_at BIGINT;
