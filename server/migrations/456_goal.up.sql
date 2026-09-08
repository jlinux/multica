-- The goal layer: a planning tier that sits ABOVE issues and is deliberately
-- NOT part of the issue table.
--
-- The obvious alternative was `issue.level`, reusing issue's parent/status/
-- property machinery. It was rejected for two reasons, in this order:
--
--   1. Isolation by construction. An issue is an EXECUTION unit: an agent can
--      be assigned it, a task runs against it, tokens are spent. A goal is a
--      PLANNING unit with a quarter-to-year horizon and no executable body.
--      Sharing the table means every assignable-pool, my-issues and autopilot
--      query must remember to exclude goals; the day one of them forgets, an
--      agent picks up "become the best product in the category (3 years)" and
--      really spends money on it. A separate table cannot leak that way.
--
--   2. Merge surface. issue.sql is the hottest query file in this repository.
--      A `level` column would force `AND level IS NULL` into a large number of
--      existing queries and keep this work permanently in conflict with
--      upstream. Every table in this change is new, so the goal layer adds
--      zero lines to any pre-existing SQL file.
--
-- The link back to execution is goal_issue (migration 452), a join table, so
-- the issue table itself stays untouched in both directions.
--
-- No foreign keys by house rule: workspace_id, parent_goal_id, prev_goal_id,
-- project_id and owner_id are all re-validated in application code, and
-- dependent cleanup is explicit (see internal/goal for the rules).
CREATE TABLE goal (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,

    -- 1 direction / 2 product goal / 3 cycle goal. Fixed at creation: a goal
    -- cannot change tier, because its parent and children were chosen under the
    -- old tier's rules. Archive and recreate instead.
    level SMALLINT NOT NULL CHECK (level IN (1, 2, 3)),

    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',

    -- Alignment. Weak by design: a lower goal SHOULD have a parent but
    -- is allowed not to. A goal with no parent is an "orphan goal" — a
    -- legitimate, explicitly displayed category for one-off work, not an error
    -- state. The rule that L2 may only point at L1 and L3 only at L2 needs a
    -- lookup, so it lives in application code (internal/goal.CanAlign), not in
    -- a CHECK.
    parent_goal_id UUID,
    orphan_reason TEXT NOT NULL DEFAULT '',

    -- Continuation chain, L3 only: 2.1 continues 2.0. Independent of alignment
    -- — a goal can continue a predecessor while aligning elsewhere, or nowhere.
    prev_goal_id UUID,

    -- The product this goal belongs to. There is deliberately no separate
    -- "product" entity: project IS the product. Adding a second container
    -- would leave users asking which one to create.
    project_id UUID,

    -- L2 only: sustain the base product vs explore a new capability
    -- (Horizon 1 / Horizon 2). Exists to make "only chasing the new, never
    -- maintaining the floor" visible in the mix.
    kind TEXT CHECK (kind IN ('base', 'brk')),

    status TEXT NOT NULL DEFAULT 'not_started'
        CHECK (status IN ('not_started', 'in_progress', 'at_risk', 'done', 'archived')),

    -- Polymorphic owner, matching issue.assignee and project.lead. An AGENT
    -- owner is a first-class case, not a curiosity: it is the agent responsible
    -- for keeping this goal's milestones current — proposing achievements,
    -- drafting summaries, flagging staleness. Owning a goal grants no execution
    -- rights; the agent still only ever runs against the issues under it.
    owner_type TEXT CHECK (owner_type IN ('member', 'agent')),
    owner_id UUID,

    cycle TEXT NOT NULL DEFAULT '',
    due_date DATE,

    -- Recorded after the fact (work that shipped before anyone filed a goal).
    -- Kept as data rather than hidden, so "how much of what we shipped was
    -- planned" is answerable. It is a signal about planning granularity, never
    -- about a person.
    is_retro BOOLEAN NOT NULL DEFAULT false,

    position DOUBLE PRECISION NOT NULL DEFAULT 0,

    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Soft delete: a goal with children or milestones must never vanish from
    -- the record, and the panorama needs to explain a broken chain.
    deleted_at TIMESTAMPTZ,

    -- Tier invariants that need no lookup are enforced here; the ones that do
    -- are in internal/goal. The database is the backstop, not the error
    -- message: application code rejects these first, with a readable reason.
    CONSTRAINT goal_l1_has_no_parent CHECK (level <> 1 OR parent_goal_id IS NULL),
    CONSTRAINT goal_kind_is_l2_only CHECK (level = 2 OR kind IS NULL),
    CONSTRAINT goal_prev_is_l3_only CHECK (level = 3 OR prev_goal_id IS NULL),
    CONSTRAINT goal_no_self_parent CHECK (parent_goal_id IS NULL OR parent_goal_id <> id),
    CONSTRAINT goal_no_self_prev CHECK (prev_goal_id IS NULL OR prev_goal_id <> id),
    CONSTRAINT goal_owner_pair CHECK ((owner_type IS NULL) = (owner_id IS NULL))
);

COMMENT ON TABLE goal IS
    'Planning tier above issues: direction (1), product goal (2), cycle goal (3). Deliberately not the issue table — goals are never assignable to an agent and never enter the task queue. Execution links through goal_issue.';
COMMENT ON COLUMN goal.level IS
    'Tier: 1 direction (1-3y), 2 product goal (6-12m), 3 cycle goal (2w-3m). Immutable after creation.';
COMMENT ON COLUMN goal.parent_goal_id IS
    'Aligned-to goal, exactly one tier up. NULL means an orphan goal, which is a displayed category and not an error. Tier compatibility is checked in application code; no FK by house rule.';
COMMENT ON COLUMN goal.prev_goal_id IS
    'Previous L3 in a continuation chain (2.0 -> 2.1). Independent of parent_goal_id.';
COMMENT ON COLUMN goal.owner_type IS
    'member | agent. An agent owner is the goal''s record-keeper: it proposes milestone achievements and drafts summaries. It confers no execution or approval rights.';
COMMENT ON COLUMN goal.is_retro IS
    'Created after the work already shipped. Feeds the planned-vs-unplanned mix, which is a team signal only and is never attributed to a person.';
