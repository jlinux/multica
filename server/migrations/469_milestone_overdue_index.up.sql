-- The scheduled sweep that flags overdue milestones and the inbox notices that
-- follow it read exactly this shape: not yet finished, planned before today.
-- Keeping it partial means the index holds only the open work, not the archive.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestone_open_planned
    ON milestone (workspace_id, planned_date)
    WHERE deleted_at IS NULL AND status IN ('planned', 'in_progress', 'pending_accept');
