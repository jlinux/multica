-- name: CreateMilestone :one
-- original_planned_date is written from the same value as planned_date and is
-- never updated afterwards; on-time attainment is measured against it.
INSERT INTO milestone (
    workspace_id, goal_id, type, n, title, value_statement,
    original_planned_date, planned_date, status,
    adoption_check, adoption_config,
    verifier_type, verifier_id, verifier_label,
    actual_date, is_retro, created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    sqlc.arg('planned_date'), sqlc.arg('planned_date'), $7,
    $8, $9, $10, $11, $12, $13, $14, $15, $16
) RETURNING *;

-- name: GetMilestoneInWorkspace :one
SELECT * FROM milestone
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: ListMilestonesForGoal :many
SELECT * FROM milestone
WHERE goal_id = $1 AND workspace_id = $2 AND deleted_at IS NULL
ORDER BY planned_date, type;

-- name: ListMilestonesForTimeline :many
-- The roadmap read: one workspace, one date window, every goal at once. The
-- window is on planned_date rather than actual_date so an unfinished milestone
-- still occupies its slot on the chart instead of disappearing until it lands.
SELECT * FROM milestone
WHERE workspace_id = $1
  AND deleted_at IS NULL
  -- Filtered on the date the chart actually draws: once a milestone has
  -- landed, the mark sits on its actual date. Filtering on the plan alone
  -- dropped a launch planned for 30 June and shipped on 2 July out of a
  -- window starting 1 July, even though the mark belonged inside it.
  AND COALESCE(actual_date, planned_date) >= sqlc.arg('from_date')
  AND COALESCE(actual_date, planned_date) <= sqlc.arg('to_date')
ORDER BY COALESCE(actual_date, planned_date);

-- name: ListOverdueMilestones :many
-- Drives the sweep that sets is_delayed and the notices that follow it.
-- Deliberately not restricted to one goal: a milestone nobody is looking at is
-- exactly the one that needs to surface.
SELECT * FROM milestone
WHERE workspace_id = $1
  AND deleted_at IS NULL
  AND status IN ('planned', 'in_progress', 'pending_accept')
  AND planned_date < sqlc.arg('today')
ORDER BY planned_date;

-- name: UpdateMilestoneStatus :one
-- Guarded on the status the caller validated against.
--
-- Two requests could otherwise both read `pending_accept`, both decide their
-- transition was legal, and then write `achieved` followed by `in_progress` —
-- leaving a terminal state, and keeping the first writer's acceptance metadata
-- attached to a milestone that is no longer accepted. The loser gets no row
-- and is told the milestone moved.
UPDATE milestone SET
    status = sqlc.arg('status'),
    actual_date = COALESCE(sqlc.narg('actual_date'), actual_date),
    accepted_by_type = COALESCE(sqlc.narg('accepted_by_type'), accepted_by_type),
    accepted_by_id = COALESCE(sqlc.narg('accepted_by_id'), accepted_by_id),
    accept_note = COALESCE(sqlc.narg('accept_note'), accept_note),
    accepted_at = COALESCE(sqlc.narg('accepted_at'), accepted_at),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: MarkMilestoneDelayed :exec
UPDATE milestone SET is_delayed = sqlc.arg('is_delayed'), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND is_delayed <> sqlc.arg('is_delayed');

-- name: LockMilestoneForUpdate :one
-- Serializes the read-modify-write paths that must not interleave: a
-- reschedule reading the date it is moving from, and a proposal retiring the
-- ones before it. Both computed their input outside the transaction, so two
-- concurrent requests each saw the pre-state and wrote a record of a move that
-- never happened — or left two claims pending on the same milestone, which the
-- queue is supposed to make impossible.
SELECT * FROM milestone
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: RescheduleMilestone :one
-- Moves the date only. original_planned_date is untouched by construction, so
-- no caller can launder a slip into a hit by rescheduling.
UPDATE milestone SET planned_date = sqlc.arg('planned_date'), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteMilestonesForGoal :exec
-- Explicit dependent cleanup, run in the same transaction as the goal delete.
UPDATE milestone SET deleted_at = now(), updated_at = now()
WHERE goal_id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: CreateMilestoneDateChange :one
INSERT INTO milestone_date_change (
    workspace_id, milestone_id, from_date, to_date, reason,
    changed_by_type, changed_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListMilestoneDateChanges :many
SELECT * FROM milestone_date_change
WHERE milestone_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: CountMilestoneDateChanges :many
-- The "moved N times" counter, batched for a whole list of milestones.
SELECT milestone_id, COUNT(*) AS change_count
FROM milestone_date_change
WHERE workspace_id = $1 AND milestone_id = ANY(sqlc.arg('milestone_ids')::uuid[])
GROUP BY milestone_id;

-- name: CreateMilestoneRelease :one
INSERT INTO milestone_release (
    workspace_id, milestone_id, kind, repo_url, ref, tag, released_at,
    pull_request_id, pull_request_source,
    summary, summary_by_type, summary_by_id, summary_at,
    created_by_type, created_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ListMilestoneReleases :many
SELECT * FROM milestone_release
WHERE milestone_id = $1 AND workspace_id = $2
ORDER BY released_at DESC NULLS LAST, created_at DESC;

-- name: DeleteMilestoneReleasesForMilestone :exec
DELETE FROM milestone_release WHERE milestone_id = $1 AND workspace_id = $2;

-- name: CreateMilestoneProposal :one
-- An agent's claim that a milestone was reached, with the run that produced it.
-- Nothing here changes the milestone; acceptance is a separate, human write.
INSERT INTO milestone_proposal (
    workspace_id, milestone_id, proposed_status, proposed_actual_date,
    evidence, evidence_refs, proposed_by_type, proposed_by_id,
    source_task_id, source_issue_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: SupersedePendingProposals :exec
-- A newer proposal for the same milestone retires the older ones. This is
-- bookkeeping rather than a judgement, which is why it records no human.
UPDATE milestone_proposal SET
    state = 'superseded', decided_at = now(),
    decided_by_type = sqlc.arg('decided_by_type'),
    decided_by_id = sqlc.arg('decided_by_id')
WHERE workspace_id = $1 AND milestone_id = $2 AND state = 'pending'
  AND id <> sqlc.arg('keep_id');

-- name: DecideMilestoneProposal :one
-- Guarded on state = 'pending' so two reviewers racing cannot both decide;
-- the loser gets no row back and the handler reports the current state.
UPDATE milestone_proposal SET
    state = sqlc.arg('state'),
    decided_by_type = sqlc.arg('decided_by_type'),
    decided_by_id = sqlc.arg('decided_by_id'),
    decided_at = now(),
    decide_note = sqlc.arg('decide_note')
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: ListPendingMilestoneProposals :many
-- Workspace-wide queue behind the inbox badge.
--
-- Joined against live milestones. Deleting a goal soft-deletes its milestones
-- but deliberately keeps their proposals as history, and without this join the
-- queue kept offering Accept on claims whose milestone no longer exists — a
-- button that could only ever fail. The rows stay in the table; they are just
-- not work anyone can still do.
SELECT p.* FROM milestone_proposal p
JOIN milestone m ON m.id = p.milestone_id AND m.deleted_at IS NULL
WHERE p.workspace_id = $1 AND p.state = 'pending'
ORDER BY p.created_at DESC;

-- name: ListMilestoneProposals :many
-- One milestone's full proposal history, decided ones included: what was
-- claimed, by which agent, and what the human said.
SELECT * FROM milestone_proposal
WHERE milestone_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: DeleteMilestoneProposalsForMilestone :exec
DELETE FROM milestone_proposal WHERE milestone_id = $1 AND workspace_id = $2;
