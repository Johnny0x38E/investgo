-- Auto-update preferences. Automatic checks are opt-out (default enabled);
-- background downloading is opt-in (default disabled), so the two defaults
-- differ on purpose.
ALTER TABLE settings ADD COLUMN auto_update_enabled INTEGER NOT NULL DEFAULT 1 CHECK (auto_update_enabled IN (0, 1));

ALTER TABLE settings ADD COLUMN auto_update_background_download INTEGER NOT NULL DEFAULT 0 CHECK (auto_update_background_download IN (0, 1));
