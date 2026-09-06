"use client";

import { useMemo } from "react";
import { Target } from "lucide-react";
import { useQueries } from "@tanstack/react-query";
import {
  goalListOptions,
  goalsForIssueOptions,
  pendingMilestoneProposalsOptions,
  type Goal,
} from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { MilestoneProposalCard } from "./milestone-proposal-card";

/**
 * The goal side of the seam, on an issue.
 *
 * Two things belong here and nowhere else. First, the chain: an issue is where
 * an agent actually works, and this is the only place a reader can see, from
 * inside the work, which direction it is serving. Second, the proposals that
 * this issue's runs produced — the agent finished here, claimed a milestone
 * was reached, and the person most likely to be able to judge that claim is
 * the one reading this issue.
 *
 * Renders nothing when the issue serves no goal. An empty card on every issue
 * in a workspace that does not use goals would be a permanent tax on the
 * sidebar for a feature that workspace has not adopted.
 */
export function IssueGoalSection({ issueId }: { issueId: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const [goalsQuery, allGoalsQuery, proposalsQuery] = useQueries({
    queries: [
      goalsForIssueOptions(wsId, issueId),
      goalListOptions(wsId),
      pendingMilestoneProposalsOptions(wsId),
    ],
  });

  const goals = goalsQuery.data ?? [];

  // Filtered here rather than by a dedicated endpoint: the pending queue is an
  // inbox, bounded by what humans have not yet decided, and it is already
  // fetched once per workspace for the badge. A per-issue endpoint would add a
  // round trip to every issue render to narrow a list that is usually short.
  const proposals = useMemo(
    () => (proposalsQuery.data ?? []).filter((p) => p.source_issue_id === issueId),
    [proposalsQuery.data, issueId],
  );

  if (goals.length === 0 && proposals.length === 0) return null;

  return (
    <section>
      <div className="mb-1.5 flex items-center gap-1.5">
        <Target className="size-3.5 text-muted-foreground" aria-hidden />
        <h3 className="text-caption font-semibold">{t(($) => $.issue_panel.heading)}</h3>
      </div>

      {goals.map((goal) => (
        <GoalChainLine
          key={goal.id}
          goal={goal}
          all={allGoalsQuery.data}
          hrefFor={(g) => paths.goalDetail(g.id)}
        />
      ))}

      {proposals.length > 0 && (
        <div className="mt-2 space-y-1.5">
          {proposals.map((proposal) => (
            <MilestoneProposalCard
              key={proposal.id}
              proposal={proposal}
              milestoneId={proposal.milestone_id}
            />
          ))}
        </div>
      )}
    </section>
  );
}

/**
 * One goal rendered with its ancestors, direction first.
 *
 * Direction first here, and nearest-parent first on the goal detail, because
 * the questions differ: standing inside the work the reader is asking "what is
 * this ultimately for", and standing on a goal they are asking "what is this
 * part of".
 */
function GoalChainLine({
  goal,
  all,
  hrefFor,
}: {
  goal: Goal;
  all: Goal[] | undefined;
  hrefFor: (goal: Goal) => string;
}) {
  const chain = useMemo(() => {
    const byId = new Map((all ?? []).map((g) => [g.id, g]));
    const up: Goal[] = [];
    const seen = new Set<string>([goal.id]);
    let cursor: Goal | undefined = goal;
    while (cursor?.parent_goal_id) {
      const parent: Goal | undefined = byId.get(cursor.parent_goal_id);
      if (!parent || seen.has(parent.id)) break;
      up.push(parent);
      seen.add(parent.id);
      cursor = parent;
    }
    return [...up.reverse(), goal];
  }, [goal, all]);

  return (
    <p className="text-caption leading-relaxed">
      {chain.map((node, index) => (
        <span key={node.id}>
          {index > 0 && <span className="mx-1 text-faint-foreground">›</span>}
          <AppLink
            href={hrefFor(node)}
            className={
              index === chain.length - 1
                ? "font-medium hover:underline"
                : "text-muted-foreground hover:underline"
            }
          >
            {node.title}
          </AppLink>
        </span>
      ))}
    </p>
  );
}
