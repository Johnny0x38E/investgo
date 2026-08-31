-- User edits of built-in pool members. Edit rows are scoped to the original
-- built-in instrument so baseline updates never clobber them, and they coexist
-- with add/exclude overrides (exclusion still hides the member; restore keeps
-- the edited display data).
CREATE TABLE pool_member_edits (
    pool_id TEXT NOT NULL,
    instrument_id TEXT NOT NULL,
    symbol TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (pool_id, instrument_id),
    FOREIGN KEY (pool_id) REFERENCES pools(id) ON DELETE CASCADE,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE RESTRICT
);
