-- Alignment lookups in both directions: a goal's children, and the
-- "which upper goals have nothing beneath them" warning the panorama shows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_parent
    ON goal (parent_goal_id)
    WHERE parent_goal_id IS NOT NULL AND deleted_at IS NULL;
