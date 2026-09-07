-- name: CreateGoal :one
INSERT INTO goal (
    workspace_id, level, title, description,
    parent_goal_id, orphan_reason, prev_goal_id, project_id,
    kind, status, owner_type, owner_id, cycle, due_date,
    is_retro, position, created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
) RETURNING *;

-- name: GetGoalInWorkspace :one
SELECT * FROM goal
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: ListGoals :many
-- The panorama's read. Every filter is optional so one query serves the
-- unfiltered board and each narrowing the user applies, rather than growing a
-- family of near-identical statements.
SELECT * FROM goal
WHERE workspace_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg('level')::smallint IS NULL OR level = sqlc.narg('level'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('owner_id')::uuid IS NULL OR owner_id = sqlc.narg('owner_id'))
  AND (sqlc.narg('parent_goal_id')::uuid IS NULL OR parent_goal_id = sqlc.narg('parent_goal_id'))
  -- 'true' restricts to unaligned goals, which the panorama shows in their own
  -- section; NULL leaves them in the main list alongside everything else.
  AND (sqlc.narg('orphan_only')::bool IS NULL OR parent_goal_id IS NULL)
ORDER BY level, position, created_at;

-- name: ListGoalsByIDs :many
SELECT * FROM goal
WHERE workspace_id = $1 AND id = ANY(sqlc.arg('ids')::uuid[]) AND deleted_at IS NULL;

-- name: CountGoalChildren :many
-- Feeds the "nothing is picking this up" warning: for each goal named, how
-- many live goals align to it. Returned as a batch because the panorama asks
-- about every upper-tier goal on screen at once.
SELECT parent_goal_id, COUNT(*) AS child_count
FROM goal
WHERE workspace_id = $1
  AND deleted_at IS NULL
  AND parent_goal_id = ANY(sqlc.arg('parent_ids')::uuid[])
GROUP BY parent_goal_id;

-- name: ListGoalContinuations :many
-- The continuation chain walked from a successor backwards. Ordered newest
-- first, which is the order the detail panel renders it in.
SELECT * FROM goal
WHERE workspace_id = $1 AND prev_goal_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateGoal :one
-- level is absent on purpose: a goal's tier is fixed at creation because its
-- parent and children were chosen under that tier's rules.
UPDATE goal SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    parent_goal_id = CASE WHEN sqlc.arg('clear_parent')::bool THEN NULL
                          ELSE COALESCE(sqlc.narg('parent_goal_id'), parent_goal_id) END,
    orphan_reason = COALESCE(sqlc.narg('orphan_reason'), orphan_reason),
    prev_goal_id = CASE WHEN sqlc.arg('clear_prev')::bool THEN NULL
                        ELSE COALESCE(sqlc.narg('prev_goal_id'), prev_goal_id) END,
    project_id = COALESCE(sqlc.narg('project_id'), project_id),
    kind = COALESCE(sqlc.narg('kind'), kind),
    status = COALESCE(sqlc.narg('status'), status),
    -- Clearing and omitting are different requests, and COALESCE alone can
    -- only express one of them. A goal that once had an owner could never lose
    -- one: JSON null arrives as an absent Go pointer, an empty string is
    -- ignored, and the endpoint returned success having changed nothing —
    -- which is worse than refusing, because the caller believes it worked.
    owner_type = CASE WHEN sqlc.arg('clear_owner')::bool THEN NULL
                      ELSE COALESCE(sqlc.narg('owner_type'), owner_type) END,
    owner_id = CASE WHEN sqlc.arg('clear_owner')::bool THEN NULL
                    ELSE COALESCE(sqlc.narg('owner_id'), owner_id) END,
    cycle = COALESCE(sqlc.narg('cycle'), cycle),
    due_date = CASE WHEN sqlc.arg('clear_due_date')::bool THEN NULL
                    ELSE COALESCE(sqlc.narg('due_date'), due_date) END,
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGoal :exec
-- Soft because a goal that carried milestones must stay explainable: the
-- panorama needs to say why a chain is broken rather than just showing a hole.
UPDATE goal SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: DetachGoalChildren :exec
-- Explicit dependent cleanup: no foreign keys by house rule, so orphaning the
-- children of a deleted goal is the application's job, in the same transaction
-- as the delete. They become unaligned goals rather than vanishing.
--
-- The reason is written here, not left empty, because an unaligned goal with
-- no reason is a shape the rules refuse. Clearing the parent alone produced a
-- goal that rendered fine and then failed validation on the owner's next edit,
-- with an error about a field they never touched. Only goals that had no
-- reason of their own are given one, so a goal that was deliberately unaligned
-- before, and later re-aligned, keeps what its owner wrote.
UPDATE goal SET
    parent_goal_id = NULL,
    orphan_reason = CASE
        WHEN orphan_reason = '' THEN sqlc.arg('detached_reason')::text
        ELSE orphan_reason
    END,
    updated_at = now()
WHERE workspace_id = $1 AND parent_goal_id = $2 AND deleted_at IS NULL;

-- name: DetachGoalContinuations :exec
UPDATE goal SET prev_goal_id = NULL, updated_at = now()
WHERE workspace_id = $1 AND prev_goal_id = $2 AND deleted_at IS NULL;

-- name: LinkGoalIssue :exec
-- The seam between planning and execution. Idempotent because the same link
-- can be proposed by an agent and then confirmed by a person.
INSERT INTO goal_issue (goal_id, issue_id, workspace_id, linked_by_type, linked_by_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (goal_id, issue_id) DO NOTHING;

-- name: UnlinkGoalIssue :exec
DELETE FROM goal_issue
WHERE goal_id = $1 AND issue_id = $2 AND workspace_id = $3;

-- name: DeleteGoalIssueLinksForGoal :exec
DELETE FROM goal_issue WHERE goal_id = $1 AND workspace_id = $2;

-- name: ListGoalIssues :many
-- The work delivering a goal, with enough of each issue to render a row.
--
-- Returning bare ids, as this did first, forced every caller into a second
-- round trip per issue or a full workspace issue list to look up a title —
-- and a list of opaque ids is not something any interface can show.
--
-- The join is on the issue side of the seam, not the goal side: the issue
-- table still stores nothing about goals, and reversing the direction here
-- would not change that.
SELECT i.id, i.number, i.title, i.status, i.priority,
       i.assignee_type, i.assignee_id, gi.linked_at
FROM goal_issue gi
JOIN issue i ON i.id = gi.issue_id
WHERE gi.goal_id = $1 AND gi.workspace_id = $2
ORDER BY gi.linked_at;

-- name: ListGoalsForIssue :many
-- "Which goals does this issue serve?", asked on every issue detail render.
SELECT g.* FROM goal g
JOIN goal_issue gi ON gi.goal_id = g.id
WHERE gi.issue_id = $1 AND gi.workspace_id = $2 AND g.deleted_at IS NULL
ORDER BY g.level, g.created_at;

-- name: GetWorkspaceGoalMetrics :one
-- Period-level goal metrics for one workspace.
--
-- There is deliberately no owner, member or agent dimension here, and adding
-- one is not a small change to this query — it is a change of what the feature
-- is for. The moment attainment can be sliced by person it becomes a
-- performance instrument, and the data that feeds it stops being true: reasons
-- for moving a date turn into whatever is safe to write down, and milestones
-- get set late so they cannot be missed. The numbers survive only as long as
-- nobody is scored on them.
--
-- Everything is counted over the whole workspace and, optionally, one cycle.
WITH scoped_goals AS (
    SELECT g.* FROM goal g
    WHERE g.workspace_id = $1
      AND g.deleted_at IS NULL
      AND g.status <> 'archived'
      AND (sqlc.narg('cycle')::text IS NULL OR g.cycle = sqlc.narg('cycle'))
),
scoped_milestones AS (
    SELECT m.* FROM milestone m
    JOIN scoped_goals sg ON sg.id = m.goal_id
    WHERE m.deleted_at IS NULL
)
SELECT
    (SELECT COUNT(*) FROM scoped_goals)::bigint AS goal_count,
    -- Alignment: upper-tier goals that something beneath them has picked up.
    (SELECT COUNT(*) FROM scoped_goals u
       WHERE u.level < 3
         AND EXISTS (SELECT 1 FROM scoped_goals c WHERE c.parent_goal_id = u.id)
    )::bigint AS aligned_upper_count,
    (SELECT COUNT(*) FROM scoped_goals WHERE level < 3)::bigint AS upper_count,
    -- The delivery arc. launched counts goals that reached launch; adopted
    -- counts the subset that reached a first real use. The gap between them is
    -- the number this whole feature exists to expose.
    (SELECT COUNT(DISTINCT goal_id) FROM scoped_milestones
       WHERE type = 'launch' AND status = 'achieved')::bigint AS launched_count,
    (SELECT COUNT(DISTINCT goal_id) FROM scoped_milestones
       WHERE type = 'first_use' AND status = 'achieved')::bigint AS adopted_count,
    -- On time is measured against the FIRST date planned, not the current one,
    -- so a reschedule cannot launder a slip into a hit.
    (SELECT COUNT(*) FROM scoped_milestones
       WHERE status = 'achieved' AND actual_date IS NOT NULL)::bigint AS achieved_count,
    (SELECT COUNT(*) FROM scoped_milestones
       WHERE status = 'achieved' AND actual_date IS NOT NULL
         AND actual_date <= original_planned_date)::bigint AS on_time_count,
    -- Unfinished and past its date. Read as a queue to work, not a tally.
    (SELECT COUNT(*) FROM scoped_milestones
       WHERE status IN ('planned', 'in_progress', 'pending_accept')
         AND planned_date < sqlc.arg('today')::date)::bigint AS overdue_count,
    -- Recorded after the fact. A signal about planning granularity; the query
    -- cannot attribute it to anyone, which is the point.
    (SELECT COUNT(*) FROM scoped_goals WHERE is_retro)::bigint AS retro_goal_count,
    (SELECT COUNT(*) FROM scoped_goals WHERE parent_goal_id IS NULL AND level > 1)::bigint AS orphan_count;

-- name: GetGoalUsageSummary :one
-- What a goal has cost, rolled up through everything aligned beneath it.
--
-- This is the number no other product-management tool can produce. The chain
-- already exists — task_usage -> agent_task_queue -> issue -> goal_issue ->
-- goal — so nothing here is recorded by hand: the cost of a plan is a
-- by-product of running it, and stays true whether or not anyone maintains it.
-- With the adoption milestones supplying the numerator, a goal finally has
-- both halves of a return.
--
-- The roll-up is recursive because a direction's cost is the cost of the work
-- under it, and that work hangs off cycle goals two tiers down. Without the
-- recursion an upper-tier goal reports zero, which reads as free rather than
-- as "ask the tier below".
--
-- cost_usd_ticks is the provider's own price at 1e-10 USD. Some runs arrive
-- unpriced, so the uncosted token totals come back alongside rather than being
-- silently folded in as zero: a goal whose spend is unknown must not render as
-- a goal that was cheap.
WITH RECURSIVE goal_tree AS (
    SELECT g.id FROM goal g
    WHERE g.id = $1 AND g.workspace_id = $2 AND g.deleted_at IS NULL
    UNION
    SELECT child.id FROM goal child
    JOIN goal_tree parent ON child.parent_goal_id = parent.id
    WHERE child.workspace_id = $2 AND child.deleted_at IS NULL
),
tree_issues AS (
    SELECT DISTINCT gi.issue_id
    FROM goal_issue gi
    JOIN goal_tree gt ON gt.id = gi.goal_id
    WHERE gi.workspace_id = $2
)
SELECT
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS total_cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COUNT(DISTINCT tu.task_id)::bigint AS task_count,
    (SELECT COUNT(*) FROM tree_issues)::bigint AS issue_count,
    (SELECT COUNT(*) FROM goal_tree)::bigint AS goal_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN tree_issues ti ON ti.issue_id = atq.issue_id;

-- name: ListGoalUsageByProvider :many
-- The same roll-up, split by provider and model.
--
-- Split by what ran, never by who ran it. Which CLI a goal's work went through
-- is an operational fact about tooling; attaching spend to a person is the
-- performance instrument this layer refuses to be, and the split is only
-- useful for the first question anyway.
WITH RECURSIVE goal_tree AS (
    SELECT g.id FROM goal g
    WHERE g.id = $1 AND g.workspace_id = $2 AND g.deleted_at IS NULL
    UNION
    SELECT child.id FROM goal child
    JOIN goal_tree parent ON child.parent_goal_id = parent.id
    WHERE child.workspace_id = $2 AND child.deleted_at IS NULL
),
tree_issues AS (
    SELECT DISTINCT gi.issue_id
    FROM goal_issue gi
    JOIN goal_tree gt ON gt.id = gi.goal_id
    WHERE gi.workspace_id = $2
)
SELECT
    tu.provider,
    tu.model,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens + tu.output_tokens), 0)::bigint AS tokens,
    COUNT(DISTINCT tu.task_id)::bigint AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN tree_issues ti ON ti.issue_id = atq.issue_id
GROUP BY tu.provider, tu.model
ORDER BY cost_usd_ticks DESC, tokens DESC;
