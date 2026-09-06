# Goals and milestones

Goals are the planning tier above issues. They answer what a stretch of work is
for; issues answer what to do about it.

## The one thing to get right

**A goal is never assignable.** Direction, product and cycle goals never enter
the task queue and never appear in anyone's My Issues. If you were handed a
goal, something is wrong upstream — the work is the issues linked beneath it.

You will not find a command to accept a milestone, edit a goal or move a date
here, and that is not an oversight. The server refuses those from an agent.

## When to read a goal

Before you start on an issue, once:

```bash
multica goal for-issue ACME-826
```

It tells you what the work is ultimately for, which the issue body usually does
not. If it prints nothing, the issue serves no goal and there is nothing to
report at the end — carry on.

To see the plan around it:

```bash
multica goal show <goal-id>            # alignment plus every milestone
multica goal issues <goal-id>          # the other work delivering the same goal
multica goal list --level 3            # the cycle goals in flight
```

Add `--output json` to any of these when you are parsing rather than reading.

## The three stages

Every goal's milestones run: **launch → first real use → still in use.**

| Stage | Means | Settled by |
|---|---|---|
| `launch` | The thing is available | The engineering record: a merged PR, a tag |
| `first_use` | Someone ran a real end-to-end pass with it | A person, an event threshold, or your evidence |
| `nth_use` | They are still running it, N passes in | Same |

Shipping is the **first** of the three, not the last. The distance between
launch and first use is what this whole tier exists to measure, so a milestone
you leave un-reported is the measurement nobody gets.

## Reporting what changed

When your run finishes something a milestone is about, say so:

```bash
multica milestone propose <milestone-id> \
  --status pending_accept \
  --evidence "PR 4471 merged and v2.1 is tagged; the batch path passed smoke on three tenants." \
  --issue ACME-826 \
  --ref pr=4471
```

This changes nothing about the milestone. It records what you believe and why,
and hands the decision to a person.

**You cannot accept your own proposal, and neither can any other agent.** A
milestone is a claim that something is genuinely done or genuinely used, and a
system where the party doing the work also certifies the work has stopped
measuring anything. Proposing is the whole of your part.

### What counts as evidence

Something a reviewer can check without trusting you.

| Good | Why |
|---|---|
| "PR 4471 merged; tag v2.1.0 exists; the batch suite passed" | Each clause is verifiable in a minute |
| "Access log shows three distinct tenants completed the import flow end to end on 12-14 Oct" | Names the source, the count and the window |
| "Support ticket 8821 confirms the customer ran payroll through it" | Points at a record that outlives the claim |

| Bad | Why |
|---|---|
| "Done" | Nothing to check |
| "Implemented as requested" | Restates the task, says nothing about the world |
| "Should be working now" | A hope with a claim's grammar |

Attach `--issue` and `--task` whenever you have them. They are what let a
reviewer open the execution log, the diff and the token cost behind your one
line, instead of taking it on trust.

### Which stage to propose

- Finished the work that makes a **launch** true: propose `pending_accept` on
  the launch milestone, or `achieved` with `--date` if it is already live and
  you can point at the release.
- Found evidence that someone **used** it: propose on the `first_use` or
  `nth_use` milestone. Never propose `achieved` on one of those without a date
  and a source; the server refuses it from you, and a refused proposal is worse
  than none.
- Not sure which milestone: run `multica goal show` first. A proposal on the
  wrong milestone is a claim a reviewer has to undo.

## What you may not do

| Not available to you | Why |
|---|---|
| Accepting or rejecting a proposal | A person decides, including on your own proposal |
| Marking an adoption milestone reached | Same rule; creation is checked too, not only updates |
| Deleting a direction or product goal | Restricted to workspace owners and admins |
| Reading why a date was moved | Reschedule reasons go to the goal's owner and the reviewers, so they stay honest |

Creating goals and milestones, and linking issues to them, are open to you the
same as to any member — but prefer proposing over creating. A goal you invent
to describe your own work is a plan nobody agreed to.

## Overdue and rescheduling

`multica goal show` marks a milestone `overdue` when it is unfinished and past
its date. That is a queue to work, not a fact to report: propose progress on
it, or say on the issue what is blocking it. Do not move the date — moving it
is a human decision that has to carry a reason, and the reason is what the
record is for.
