-- The proposal history of one milestone, including decided ones: what was
-- claimed, by which agent, and what the human said.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_proposal_milestone
    ON milestone_proposal (milestone_id, created_at DESC);
