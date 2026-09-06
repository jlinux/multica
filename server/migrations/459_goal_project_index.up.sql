-- The product view: every goal attached to one project. Also the timeline,
-- which groups its rows by product before it lays out any dates.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_project
    ON goal (project_id)
    WHERE project_id IS NOT NULL AND deleted_at IS NULL;
