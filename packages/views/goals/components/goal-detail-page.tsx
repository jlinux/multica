"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowLeft,
  Ban,
  CircleCheck,
  CalendarClock,
  CircleCheckBig,
  CircleDashed,
  Clock,
  Plus,
} from "lucide-react";
import { useQueries } from "@tanstack/react-query";
import {
  goalDetailOptions,
  goalIssuesOptions,
  goalListOptions,
  goalMilestonesOptions,
  pendingMilestoneProposalsOptions,
  useUpdateMilestoneStatus,
  type MilestoneProposal,
  type Goal,
  type GoalIssue,
  type Milestone,
} from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { PageHeader, PAGE_GUTTER } from "../../layout/page-header";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { GoalCostCard } from "./goal-cost-card";
import { MilestoneProposalCard } from "./milestone-proposal-card";
import { MilestoneFormDialog } from "./milestone-form-dialog";
import { RescheduleDialog } from "./reschedule-dialog";

/**
 * One goal: where it sits in the plan, what it has promised, and the work
 * carrying it.
 *
 * The page is arranged around the seam. Above it, alignment and continuation
 * say where this goal came from. On it, the milestones say what was promised
 * and whether shipping turned into use. Below it, the linked issues are the
 * only things here an agent can actually be handed — which the page states in
 * words rather than leaving the reader to infer from the absence of an
 * assignee picker.
 */
export function GoalDetailPage({ goalId }: { goalId: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const [goalQuery, milestonesQuery, issuesQuery, allGoalsQuery, proposalsQuery] =
    useQueries({
      queries: [
        goalDetailOptions(wsId, goalId),
        goalMilestonesOptions(wsId, goalId),
        goalIssuesOptions(wsId, goalId),
        goalListOptions(wsId),
        pendingMilestoneProposalsOptions(wsId),
      ],
    });

  // Grouped from the workspace queue rather than fetched per milestone: the
  // queue is an inbox bounded by what humans have not decided, already loaded
  // once for the badge, and a request per milestone would turn one page into
  // an N+1 for a list that is usually empty.
  const [addingMilestone, setAddingMilestone] = useState(false);
  // The milestone whose date is being moved, or null. Held here rather than in
  // each row so only one reschedule dialog can ever be open.
  const [rescheduling, setRescheduling] = useState<Milestone | null>(null);

  const proposalsByMilestone = useMemo(() => {
    const map = new Map<string, MilestoneProposal[]>();
    for (const proposal of proposalsQuery.data ?? []) {
      const list = map.get(proposal.milestone_id);
      if (list) list.push(proposal);
      else map.set(proposal.milestone_id, [proposal]);
    }
    return map;
  }, [proposalsQuery.data]);

  const goal = goalQuery.data;
  const ancestors = useAlignmentChain(goal, allGoalsQuery.data);
  const previous = useMemo(() => {
    if (!goal?.prev_goal_id || !allGoalsQuery.data) return null;
    return allGoalsQuery.data.find((g) => g.id === goal.prev_goal_id) ?? null;
  }, [goal, allGoalsQuery.data]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <AppLink
          href={paths.goals()}
          className="flex items-center gap-1.5 text-label text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          {t(($) => $.detail.back)}
        </AppLink>
      </PageHeader>

      <div className={cn("flex-1 overflow-y-auto pt-4 pb-16", PAGE_GUTTER)}>
        {goalQuery.isPending ? (
          <DetailSkeleton label={t(($) => $.page.loading)} />
        ) : goalQuery.isError || !goal ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <AlertTriangle className="size-4" />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.detail.not_found)}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="mx-auto max-w-4xl">
            <GoalHeading goal={goal} />

            {(ancestors.length > 0 || previous) && (
              <div className="mt-4 flex flex-col gap-1.5 rounded-lg border border-surface-border bg-surface p-3">
                {ancestors.length > 0 && (
                  <ChainRow
                    label={t(($) => $.detail.aligns_to)}
                    goals={ancestors}
                    hrefFor={(g) => paths.goalDetail(g.id)}
                  />
                )}
                {previous && (
                  <ChainRow
                    label={t(($) => $.detail.continues)}
                    goals={[previous]}
                    hrefFor={(g) => paths.goalDetail(g.id)}
                  />
                )}
              </div>
            )}

            <SeamNotice />

            {/* Under the seam notice on purpose: the sentence above says the
                goal is not something an agent can be handed, and this is the
                evidence of what the agents beneath it actually did. */}
            <div className="mt-4">
              <GoalCostCard goalId={goalId} />
            </div>

            <SectionHeading
              title={t(($) => $.detail.milestones)}
              hint={t(($) => $.detail.milestones_hint)}
              action={
                <Button
                  variant="outline"
                  size="sm"
                  className="gap-1.5"
                  onClick={() => setAddingMilestone(true)}
                >
                  <Plus className="size-3.5" />
                  {t(($) => $.detail.add_milestone)}
                </Button>
              }
            />
            {milestonesQuery.isPending ? (
              <Skeleton className="h-28 rounded-lg" />
            ) : (milestonesQuery.data ?? []).length === 0 ? (
              <Empty className="rounded-lg border border-surface-border bg-surface">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <CircleDashed className="size-4" />
                  </EmptyMedia>
                  <EmptyTitle>{t(($) => $.detail.no_milestones)}</EmptyTitle>
                  <EmptyDescription>
                    {t(($) => $.detail.no_milestones_hint)}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="overflow-hidden rounded-lg border border-surface-border bg-surface">
                {(milestonesQuery.data ?? []).map((milestone) => (
                  <MilestoneRow
                    key={milestone.id}
                    milestone={milestone}
                    goalId={goalId}
                    proposals={proposalsByMilestone.get(milestone.id) ?? []}
                    onReschedule={() => setRescheduling(milestone)}
                  />
                ))}
              </div>
            )}

            <SectionHeading
              title={t(($) => $.detail.issues)}
              hint={t(($) => $.detail.issues_hint)}
            />
            {issuesQuery.isPending ? (
              <Skeleton className="h-20 rounded-lg" />
            ) : (issuesQuery.data ?? []).length === 0 ? (
              <p className="rounded-lg border border-dashed border-surface-border p-4 text-center text-caption text-faint-foreground">
                {t(($) => $.detail.no_issues)}
              </p>
            ) : (
              <div className="overflow-hidden rounded-lg border border-surface-border bg-surface">
                {(issuesQuery.data ?? []).map((issue) => (
                  <IssueRow
                    key={issue.id}
                    issue={issue}
                    href={paths.issueDetail(issue.id)}
                  />
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {addingMilestone && (
        <MilestoneFormDialog goalId={goalId} open onOpenChange={setAddingMilestone} />
      )}
      {rescheduling && (
        <RescheduleDialog
          milestone={rescheduling}
          goalId={goalId}
          open
          onOpenChange={(next) => {
            if (!next) setRescheduling(null);
          }}
        />
      )}
    </div>
  );
}

/**
 * The goals above this one, nearest parent first.
 *
 * Resolved from the workspace listing rather than fetched per level: the
 * panorama already loads every goal, so walking the chain here costs nothing
 * and one refetch keeps both screens consistent. The walk stops on a missing
 * or repeated parent so a filtered response, or data that should not contain a
 * cycle, cannot hang the render.
 */
function useAlignmentChain(goal: Goal | undefined, all: Goal[] | undefined): Goal[] {
  return useMemo(() => {
    if (!goal || !all) return [];
    const byId = new Map(all.map((g) => [g.id, g]));
    const chain: Goal[] = [];
    const seen = new Set<string>([goal.id]);
    let cursor: Goal | undefined = goal;
    while (cursor?.parent_goal_id) {
      const parent: Goal | undefined = byId.get(cursor.parent_goal_id);
      if (!parent || seen.has(parent.id)) break;
      chain.push(parent);
      seen.add(parent.id);
      cursor = parent;
    }
    return chain;
  }, [goal, all]);
}

const TIER_LABEL = { 1: "direction", 2: "product", 3: "cycle" } as const;

function GoalHeading({ goal }: { goal: Goal }) {
  const { t } = useT("goals");
  const tierKey = TIER_LABEL[goal.level as 1 | 2 | 3] ?? "cycle";
  const statusKey = ["not_started", "in_progress", "at_risk", "done", "archived"].includes(
    goal.status,
  )
    ? goal.status
    : "not_started";

  return (
    <header>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="secondary" className="text-micro">
          {t(($) => $.tier[tierKey])}
        </Badge>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.status[statusKey as "in_progress"])}
        </span>
        {goal.kind === "base" || goal.kind === "brk" ? (
          <Badge variant="secondary" className="text-micro">
            {t(($) => $.kind[goal.kind as "base"])}
          </Badge>
        ) : null}
        {goal.is_retro ? (
          <Badge variant="secondary" className="text-micro">
            {t(($) => $.badge.retro)}
          </Badge>
        ) : null}
        {!goal.parent_goal_id && goal.level !== 1 ? (
          <Badge variant="secondary" className="text-micro">
            {t(($) => $.detail.no_alignment)}
          </Badge>
        ) : null}
      </div>
      <h1 className="mt-1.5 text-display-sm font-semibold tracking-tight">{goal.title}</h1>
      {goal.description ? (
        <p className="mt-2 text-body text-muted-foreground">{goal.description}</p>
      ) : null}
      <dl className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-label">
        <Meta label={t(($) => $.detail.owner)}>
          {goal.owner_id ? goal.owner_id.slice(0, 8) : t(($) => $.detail.unassigned)}
        </Meta>
        {goal.cycle ? (
          <Meta label={t(($) => $.detail.cycle)}>{goal.cycle}</Meta>
        ) : null}
        {goal.due_date ? (
          <Meta label={t(($) => $.detail.due)}>{goal.due_date}</Meta>
        ) : null}
      </dl>
      {!goal.parent_goal_id && goal.orphan_reason ? (
        <p className="mt-2 text-caption text-faint-foreground">{goal.orphan_reason}</p>
      ) : null}
    </header>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-1.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{children}</dd>
    </div>
  );
}

function ChainRow({
  label,
  goals,
  hrefFor,
}: {
  label: string;
  goals: Goal[];
  hrefFor: (goal: Goal) => string;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-label">
      <span className="text-caption text-muted-foreground">{label}</span>
      {goals.map((goal, index) => (
        <span key={goal.id} className="flex items-center gap-2">
          {index > 0 && <span className="text-faint-foreground">/</span>}
          <AppLink href={hrefFor(goal)} className="font-medium hover:underline">
            {goal.title}
          </AppLink>
        </span>
      ))}
    </div>
  );
}

/**
 * Says in words what the schema enforces by construction.
 *
 * A reader who has just seen a title, an owner and a status has every reason
 * to look for an assignee picker next. Its absence explains nothing on its
 * own, and the answer — that a goal is a planning unit and the executable
 * thing is the issue below — is the single idea this whole feature rests on.
 */
function SeamNotice() {
  const { t } = useT("goals");
  return (
    <div className="mt-5 flex gap-2.5 rounded-lg border border-dashed border-destructive/40 bg-destructive/5 p-3">
      <Ban className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden />
      <div>
        <p className="text-label font-semibold text-destructive">
          {t(($) => $.detail.seam_title)}
        </p>
        <p className="mt-0.5 text-caption text-muted-foreground">
          {t(($) => $.detail.seam_body)}
        </p>
      </div>
    </div>
  );
}

function SectionHeading({
  title,
  hint,
  action,
}: {
  title: string;
  hint: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="mt-6 mb-2 flex items-end gap-2">
      <h2 className="text-label font-semibold">{title}</h2>
      <p className="text-caption text-faint-foreground">{hint}</p>
      {action ? <div className="ml-auto">{action}</div> : null}
    </div>
  );
}

const MILESTONE_ORDER = { launch: 0, first_use: 1, nth_use: 2 } as const;

function MilestoneRow({
  milestone,
  goalId,
  proposals,
  onReschedule,
}: {
  milestone: Milestone;
  goalId: string;
  proposals: MilestoneProposal[];
  onReschedule: () => void;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const updateStatus = useUpdateMilestoneStatus(wsId);
  // Mirrors the server rule (internal/goal.CanTransitionFor). A launch needs no
  // handoff; an adoption milestone only becomes reachable once it has had one.
  const open = milestone.status !== "achieved" && milestone.status !== "cancelled";
  const canReachDirectly =
    !milestone.requires_acceptance || milestone.status === "pending_accept";
  const typeKey =
    milestone.type in MILESTONE_ORDER
      ? (milestone.type as keyof typeof MILESTONE_ORDER)
      : "launch";
  const achieved = milestone.status === "achieved";
  const adoptionKey =
    milestone.adoption_check === "verifier" ||
    milestone.adoption_check === "analytics" ||
    milestone.adoption_check === "agent"
      ? (`adoption_${milestone.adoption_check}` as const)
      : null;

  return (
    <div className="flex gap-3 border-b p-3 last:border-b-0">
      <span
        aria-hidden
        className={cn(
          "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border",
          achieved
            ? "border-success bg-success text-primary-foreground"
            : milestone.is_delayed
              ? "border-destructive text-destructive"
              : "border-surface-border text-faint-foreground",
        )}
      >
        {achieved ? <CircleCheck className="size-3.5" /> : <Clock className="size-3.5" />}
      </span>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-body font-medium">{milestone.title}</span>
          <Badge variant="secondary" className="text-micro">
            {t(($) => $.milestone[typeKey])}
            {milestone.type === "nth_use" && milestone.n ? ` · ${milestone.n}` : ""}
          </Badge>
          {milestone.is_delayed && !achieved ? (
            <Badge variant="secondary" className="text-micro text-destructive">
              {t(($) => $.detail.delayed)}
            </Badge>
          ) : null}
        </div>

        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-caption text-muted-foreground">
          {milestone.planned_date ? (
            <span>
              {t(($) => $.detail.planned)} {milestone.planned_date}
            </span>
          ) : null}
          {milestone.actual_date ? (
            <span className="text-success">
              {t(($) => $.detail.actual)} {milestone.actual_date}
            </span>
          ) : null}
          {/* The reschedule counter, shown wherever the milestone is: the
              record of a moved date is only useful if it is where the date is,
              not two clicks away in a log nobody opens. */}
          {milestone.date_change_count > 0 ? (
            <span>
              {t(($) => $.timeline.moved_times, { count: milestone.date_change_count })}
            </span>
          ) : null}
          {/* The control sits with the date it moves. A reschedule buried in a
              row menu is one the owner does not find, and an unmoved overdue
              date is worse for the record than a moved one with a reason. */}
          {!achieved && (
            <button
              type="button"
              onClick={onReschedule}
              className="inline-flex items-center gap-1 rounded text-brand hover:underline"
            >
              <CalendarClock className="size-3" aria-hidden />
              {t(($) => $.form.reschedule)}
            </button>
          )}
          {/* A team with no agents still has to be able to finish a milestone;
              without this the only route to `achieved` was accepting an agent's
              proposal, which made the whole arc depend on having agents.
              Which action is offered follows the rule, rather than offering one
              button that fails for half the milestones: a launch is settled by
              the engineering record and can be marked reached outright, while
              an adoption milestone has to be handed to whoever decides it was
              really used. The date is today, not a picker — the common case is
              recording something that just happened, and a backdated one is a
              reschedule followed by this. */}
          {open && canReachDirectly && (
            <button
              type="button"
              disabled={updateStatus.isPending}
              title={t(($) => $.detail.mark_achieved_hint)}
              onClick={() =>
                updateStatus.mutate({
                  id: milestone.id,
                  goalId,
                  status: "achieved",
                  actual_date: new Date().toISOString().slice(0, 10),
                })
              }
              className="inline-flex items-center gap-1 rounded text-success hover:underline disabled:opacity-50"
            >
              <CircleCheckBig className="size-3" aria-hidden />
              {t(($) => $.detail.mark_achieved)}
            </button>
          )}
          {open && !canReachDirectly && milestone.status !== "pending_accept" && (
            <button
              type="button"
              disabled={updateStatus.isPending}
              title={t(($) => $.detail.send_for_acceptance_hint)}
              onClick={() =>
                updateStatus.mutate({
                  id: milestone.id,
                  goalId,
                  status: "pending_accept",
                })
              }
              className="inline-flex items-center gap-1 rounded text-brand hover:underline disabled:opacity-50"
            >
              <CircleCheckBig className="size-3" aria-hidden />
              {t(($) => $.detail.send_for_acceptance)}
            </button>
          )}
          {milestone.verifier_label ? (
            <span>
              {t(($) => $.detail.verifier)} {milestone.verifier_label}
            </span>
          ) : null}
        </div>

        {/* How adoption will be decided, stated on the row rather than hidden
            in an edit form: it is the term of the promise, and two owners who
            mean different things by "used" make the rate meaningless. */}
        {adoptionKey ? (
          <p className="mt-1 text-caption text-faint-foreground">
            {t(($) => $.detail[adoptionKey])}
          </p>
        ) : null}

        {milestone.value_statement ? (
          <p className="mt-1.5 text-caption text-muted-foreground">
            {milestone.value_statement}
          </p>
        ) : null}

        {/* Claims waiting on a person, on the row they are about. A reviewer
            who has to go somewhere else to find them will not find them. */}
        {proposals.length > 0 && (
          <div className="mt-2 space-y-1.5">
            {proposals.map((proposal) => (
              <MilestoneProposalCard
                key={proposal.id}
                proposal={proposal}
                milestoneId={milestone.id}
                goalId={goalId}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

const ISSUE_STATUS_DOT: Record<string, string> = {
  backlog: "bg-faint-foreground",
  todo: "bg-faint-foreground",
  in_progress: "bg-brand",
  in_review: "bg-warning",
  done: "bg-success",
  blocked: "bg-destructive",
  cancelled: "bg-faint-foreground",
};

function IssueRow({ issue, href }: { issue: GoalIssue; href: string }) {
  const { t } = useT("goals");
  return (
    <AppLink
      href={href}
      className="flex items-center gap-2.5 border-b px-3 py-2 last:border-b-0 hover:bg-surface-hover"
    >
      <span
        aria-hidden
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          ISSUE_STATUS_DOT[issue.status] ?? "bg-faint-foreground",
        )}
      />
      <span className="w-12 shrink-0 text-caption text-faint-foreground tabular-nums">
        #{issue.number}
      </span>
      <span className="min-w-0 flex-1 truncate text-body">{issue.title}</span>
      {issue.assignee_type === "agent" ? (
        <Badge variant="secondary" className="shrink-0 text-micro">
          {t(($) => $.detail.assignee_agent)}
        </Badge>
      ) : null}
    </AppLink>
  );
}

function DetailSkeleton({ label }: { label: string }) {
  return (
    <div aria-busy aria-label={label} className="mx-auto max-w-4xl space-y-4">
      <Skeleton className="h-8 w-2/3" />
      <Skeleton className="h-16 w-full rounded-lg" />
      <Skeleton className="h-28 w-full rounded-lg" />
    </div>
  );
}
