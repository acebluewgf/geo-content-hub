-- Encrypted copy of each API token so administrators can view it later.
-- Authentication still uses token_hash. Ciphertext is only for the admin UI.
ALTER TABLE api_clients ADD COLUMN token_cipher VARBINARY(512) NULL AFTER token_hash;
