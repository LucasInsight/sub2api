-- Track OpenAI/Codex 7d quota-window observations. The pending
-- flag gates the admin-only operation that resets every active user subscription.
ALTER TABLE accounts
	ADD COLUMN IF NOT EXISTS codex_7d_observed_reset_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS codex_quota_observed_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS codex_official_early_reset_pending BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS codex_official_early_reset_detected_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS codex_official_early_reset_handled_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_accounts_codex_official_early_reset_pending
    ON accounts (codex_official_early_reset_pending)
    WHERE codex_official_early_reset_pending = TRUE AND deleted_at IS NULL;
