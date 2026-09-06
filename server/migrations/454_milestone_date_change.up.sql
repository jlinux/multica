-- Every reschedule, with its reason, kept forever.
--
-- The product rule this table implements is that the system never silently
-- swallows an overdue milestone: a date can move, but only by leaving a row
-- here. What that buys at review time is the question "how many times did this
-- move, and what did we say each time" — which is a planning-quality question.
--
-- It is deliberately NOT an accountability trail. Reasons are shown to the
-- goal's owner and aggregated for the period; they are not exposed as a
-- per-person history, because a reschedule log that can be read as a personal
-- record stops containing true reasons within a quarter.
CREATE TABLE milestone_date_change (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    milestone_id UUID NOT NULL,
    from_date DATE NOT NULL,
    to_date DATE NOT NULL,
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    changed_by_type TEXT NOT NULL CHECK (changed_by_type IN ('member', 'agent')),
    changed_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT milestone_date_change_moves CHECK (from_date <> to_date)
);

COMMENT ON TABLE milestone_date_change IS
    'Append-only reschedule log. A milestone date may only move by writing a row here with a reason, so no overdue item disappears quietly. Read in aggregate for planning quality, never as a per-person record.';
