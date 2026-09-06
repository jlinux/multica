-- The seam between planning and execution.
--
-- This join table is the ONLY thing connecting a goal to the work that
-- delivers it, and it exists as a table rather than an `issue.goal_id` column
-- for the same two reasons goal is its own table (see migration 451): the
-- issue table keeps zero knowledge of goals, so no issue query can accidentally
-- surface or filter one, and no pre-existing SQL file is touched.
--
-- Many-to-one is not assumed. One issue may serve two goals (a refactor that
-- unblocks both the console and billing tracks), and forcing a single column
-- would make that unrepresentable.
--
-- workspace_id is denormalised so the workspace filter every query in this
-- repository applies can be satisfied without joining goal or issue, and so
-- workspace deletion can clear these rows directly.
CREATE TABLE goal_issue (
    goal_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    linked_by_type TEXT CHECK (linked_by_type IN ('member', 'agent')),
    linked_by_id UUID,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (goal_id, issue_id)
);

COMMENT ON TABLE goal_issue IS
    'Links a goal (in practice level 3) to the issues delivering it. The issue side stores nothing, which is what keeps goals structurally out of the assignable pool, my-issues and autopilot scans.';
COMMENT ON COLUMN goal_issue.linked_by_type IS
    'member | agent. An agent links issues when it derives the connection itself, e.g. while proposing a goal from an existing cluster of work.';
