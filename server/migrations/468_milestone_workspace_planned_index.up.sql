-- The timeline view scans one workspace over a date window, across every goal
-- and product at once. Without this it is a full scan of the table on the most
-- frequently opened page in the goal layer after the panorama.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_workspace_planned
    ON milestone (workspace_id, planned_date)
    WHERE deleted_at IS NULL;
