# Goal layer

A planning tier above issues: **direction (L1) → product goal (L2) → cycle goal
(L3)**, each cycle goal carrying **launch → first use → nth use** milestones.
Execution stays where it already is — an issue, an agent, a task, a pull
request — and connects to the plan through one join table.

## The merge contract

This feature is developed against a moving upstream, so its primary
architectural constraint is *how little of the existing tree it touches*. The
rule is: **new files only, wherever a choice exists.**

What that ruled out, and why:

| Tempting | Rejected because |
| --- | --- |
| `issue.level` column | `issue.sql` is the hottest query file in the repo. A tier column forces `AND level IS NULL` into a large number of existing queries and keeps this branch permanently in conflict. |
| `issue.goal_id` column | Same, plus it makes one issue serving two goals unrepresentable. |
| Extending `issue_pull_request` | Would edit a table three integrations already write to. `milestone_release` points at the PR row instead. |

The isolation this buys is not only about merges. An issue is an **execution**
unit — assignable to an agent, backed by a task, costing tokens. A goal is a
**planning** unit with no executable body. Sharing a table means every
assignable-pool, my-issues and autopilot query has to remember to exclude
goals; the day one forgets, an agent picks up *"become the best product in the
category (3 years)"* and really spends money on it. Separate tables cannot leak
that way, and `goal.Assignable()` is the second lock for any future code path
tempted to build a list from a union.

## What this change adds

Entirely new, conflicts with nothing upstream:

```
server/migrations/451..468_*.sql        6 tables, 12 concurrent indexes
server/pkg/db/queries/goal.sql          new file
server/pkg/db/queries/milestone.sql     new file
server/internal/goal/                   new package (this one)
```

Tables: `goal`, `goal_issue`, `milestone`, `milestone_date_change`,
`milestone_release`, `milestone_proposal`. **Zero lines are added to any
pre-existing SQL file, and no existing table gains a column.**

Still to come, with their expected merge cost:

| Step | Touches upstream | Cost |
| --- | --- | --- |
| `internal/handler/goal.go`, `milestone.go` | new files | none |
| Route registration | ~12 lines in `cmd/server/router.go` | one hunk, in the pattern of the `/api/projects` block |
| `packages/core/goals/`, `packages/views/goals/` | new directories | none |
| Sidebar entry | ~2 lines in `packages/views/layout/app-sidebar.tsx` | one hunk |
| Web + desktop routes | new files under each app's router | one hunk each |

That is the whole intended footprint on existing files: **one route block, one
nav entry, two router registrations.** Anything beyond it should be treated as
a design smell and pushed back into this package.

### Rebasing onto upstream

Because every conflict is confined to the four call sites above, a rebase is:

1. `git rebase upstream/main` — migrations never conflict (new numbers), SQL
   query files never conflict (new files), this package never conflicts.
2. If a migration number collides with an upstream one, renumber this branch's
   files upward. Nothing references migration numbers by name.
3. Re-apply the route/nav hunks if upstream reshaped them.
4. `make sqlc` — the generated file is derived, so regenerate rather than merge.

## Where the rules live

The database enforces every invariant that needs no lookup (see the CHECK
constraints in migration 451 and 453). This package enforces the ones that do,
and owns every message a user reads when a rule refuses an edit:

- `CanAlign` — a goal aligns exactly one tier up. Skipping a tier is refused;
  the map's readability is the feature.
- `Validate` — tier/kind/project/orphan-reason invariants.
- `CanTransition` — the milestone state machine. `achieved` is terminal.
- `OnTime` — measured against `original_planned_date`, never the rescheduled
  one, so a slip cannot be laundered into a hit.
- `CanPropose` / `CanDecide` — an agent proposes, a human accepts. An agent may
  never accept, including its own proposal.

Everything here is pure: no database handle, no context, no clock beyond what a
caller passes in. Run it without a Postgres:

```bash
cd server && go test ./internal/goal/...
```

## Agent and git hookups

Both are first-class, and both are why this sits inside Multica rather than
beside it:

- **Agent → goal.** `goal.owner_type = 'agent'` makes an agent the goal's
  record-keeper. `milestone_proposal` is how it acts: it proposes a milestone
  was reached, attaches evidence, and names the run (`source_task_id`) and issue
  (`source_issue_id`) behind the claim, so a reviewer can open the execution log
  and the token cost sitting behind a one-line assertion that a feature is live.
  It never applies the change itself.
- **Git → goal.** `milestone_release` either records a release by hand or points
  at a `github_pull_request` / `vcs_pull_request` row this server already has.
  An agent finishes an issue, the issue already carries its PR, and that same PR
  row is what the milestone attaches — nothing is retyped for the engineering
  fact and the delivery record to agree.
- **Cost → goal.** No new storage needed: `task_usage → agent_task_queue →
  issue → goal_issue → goal` already yields real spend per goal.
