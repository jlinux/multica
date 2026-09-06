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
    owner_type = COALESCE(sqlc.narg('owner_type'), owner_type),
    owner_id = COALESCE(sqlc.narg('owner_id'), owner_id),
    cycle = COALESCE(sqlc.narg('cycle'), cycle),
    due_date = COALESCE(sqlc.narg('due_date'), due_date),
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
UPDATE goal SET parent_goal_id = NULL, updated_at = now()
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
