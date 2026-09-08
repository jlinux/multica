-- Continuation chains are walked backwards from the newest goal, so the
-- lookup is by successor. Partial because only L3 goals ever set this.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_prev
    ON goal (prev_goal_id)
    WHERE prev_goal_id IS NOT NULL;
