-- The reschedule history of one milestone, newest first, plus the "moved N
-- times" counter shown on its card.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_date_change_milestone
    ON milestone_date_change (milestone_id, created_at DESC);
