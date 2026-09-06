-- Every proposal still waiting on a human, workspace-wide. This is the query
-- behind the inbox badge, so it is asked far more often than the full history
-- of one milestone; the partial predicate keeps decided rows out of it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_proposal_pending
    ON milestone_proposal (workspace_id, created_at DESC)
    WHERE state = 'pending';
