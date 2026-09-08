-- A milestone's releases, ordered the way the detail panel shows them.
-- released_at is nullable for a pending release, which sorts last.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_release_milestone
    ON milestone_release (milestone_id, released_at DESC NULLS LAST);
