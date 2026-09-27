-- User-facing display aliases live on the instrument catalog so a rename
-- applies to holdings, watchlists, and every hot pool that shares the row.
-- Official provider/baseline names stay in instruments.name and remain
-- recoverable by clearing display_name.
ALTER TABLE instruments ADD COLUMN display_name TEXT NOT NULL DEFAULT '';

-- Promote any pool-scoped name overlays onto the shared instrument alias.
UPDATE instruments
SET display_name = (
    SELECT e.name
    FROM pool_member_edits AS e
    WHERE e.instrument_id = instruments.id
      AND TRIM(e.name) != ''
    ORDER BY e.updated_at DESC
    LIMIT 1
)
WHERE TRIM(display_name) = ''
  AND EXISTS (
      SELECT 1
      FROM pool_member_edits AS e
      WHERE e.instrument_id = instruments.id
        AND TRIM(e.name) != ''
  );
