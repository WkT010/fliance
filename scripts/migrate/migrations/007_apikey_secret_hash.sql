-- 007: store API key secrets as Argon2id hashes instead of plaintext.
-- New keys write the hash into secret_hash (the legacy secret column is left
-- empty). Pre-existing rows keep their plaintext secret value and are lazily
-- upgraded (secret_hash populated, secret cleared) on first successful
-- validation, enabling a zero-downtime rollout.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS secret_hash TEXT
