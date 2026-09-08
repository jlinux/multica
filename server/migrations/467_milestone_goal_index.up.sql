-- A goal's milestones, in the order they are always displayed: launch, then
-- first use, then each nth use. Sorting by planned_date rather than type keeps
-- a rescheduled milestone in its true place on the goal detail.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_goal
    ON milestone (goal_id, planned_date)
    WHERE deleted_at IS NULL;
