-- Reverse lookup: "which goals does this issue serve?", asked on every issue
-- detail render. The primary key leads with goal_id and cannot answer it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_issue_issue
    ON goal_issue (issue_id);
