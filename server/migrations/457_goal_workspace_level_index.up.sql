-- The panorama's primary read: every live goal in a workspace, one tier band
-- at a time. deleted_at is in the predicate rather than the key because soft-
-- deleted goals are never listed, only fetched by id when explaining a chain.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_workspace_level
    ON goal (workspace_id, level, position)
    WHERE deleted_at IS NULL;
