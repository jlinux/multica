"use client";

import { useState } from "react";
import { Bot, Check, X } from "lucide-react";
import { useDecideMilestoneProposal, type MilestoneProposal } from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";

/**
 * An agent's claim that a milestone was reached, and the two buttons that
 * settle it.
 *
 * This card is the visible half of the arrangement the whole goal layer rests
 * on: the agent that did the work states what it believes and shows why; a
 * person decides. The server refuses an accept from an agent — including for
 * its own proposal — so the buttons here are not the enforcement, they are
 * where a human meets a claim that has already been made and evidenced.
 *
 * The evidence is shown expanded rather than behind a disclosure. A reviewer
 * asked to accept something on an agent's word, who has to click first to see
 * the word, will start clicking Accept without clicking the disclosure.
 */
export function MilestoneProposalCard({
  proposal,
  milestoneId,
  goalId,
  className,
}: {
  proposal: MilestoneProposal;
  milestoneId: string;
  goalId?: string;
  className?: string;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const decide = useDecideMilestoneProposal(wsId);
  const [note, setNote] = useState("");

  const pending = proposal.state === "pending";
  const byAgent = proposal.proposed_by_type === "agent";

  const settle = (state: "accepted" | "rejected") => {
    decide.mutate({
      proposalId: proposal.id,
      milestoneId,
      goalId,
      state,
      decide_note: note.trim() || undefined,
    });
  };

  return (
    <div
      className={cn(
        "rounded-md border p-2.5",
        pending
          ? "border-brand/30 bg-brand/5"
          : "border-surface-border bg-surface-hover",
        className,
      )}
    >
      <div className="flex items-center gap-1.5 text-caption">
        <Bot className="size-3.5 shrink-0 text-brand" aria-hidden />
        <span className="font-semibold">{t(($) => $.proposal.heading)}</span>
        <span className="text-muted-foreground">
          {proposal.proposed_status === "achieved"
            ? t(($) => $.proposal.proposes_achieved)
            : t(($) => $.proposal.proposes_pending)}
        </span>
        {!pending && (
          <span className="ml-auto shrink-0 font-medium text-muted-foreground">
            {t(($) => $.proposal[`decided_${proposal.state}` as "decided_accepted"])}
          </span>
        )}
      </div>

      {proposal.evidence ? (
        <p className="mt-1.5 text-caption leading-relaxed text-foreground">
          <span className="text-muted-foreground">
            {t(($) => $.proposal.evidence)}:{" "}
          </span>
          {proposal.evidence}
        </p>
      ) : null}

      {/* Provenance back into execution. It is what lets a reviewer open the
          run, the diff and the tokens spent behind a one-line assertion that a
          feature is live, instead of taking the sentence on trust. */}
      {(proposal.source_task_id || proposal.source_issue_id) && (
        <p className="mt-1 text-micro text-faint-foreground">
          {proposal.source_task_id ? t(($) => $.proposal.open_run) : null}
          {proposal.source_task_id && proposal.source_issue_id ? " · " : null}
          {proposal.source_issue_id ? t(($) => $.proposal.open_issue) : null}
        </p>
      )}

      {pending && (
        <>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <Input
              value={note}
              onChange={(event) => setNote(event.target.value)}
              placeholder={t(($) => $.proposal.note_placeholder)}
              className="h-7 min-w-0 flex-1 text-caption"
            />
            <Button
              size="sm"
              className="h-7 gap-1"
              disabled={decide.isPending}
              onClick={() => settle("accepted")}
            >
              <Check className="size-3.5" />
              {decide.isPending
                ? t(($) => $.proposal.accepting)
                : t(($) => $.proposal.accept)}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-7 gap-1"
              disabled={decide.isPending}
              onClick={() => settle("rejected")}
            >
              <X className="size-3.5" />
              {t(($) => $.proposal.reject)}
            </Button>
          </div>

          {/* Why there is no "let the agent decide" option here. Stated rather
              than implied, because an absent control explains nothing. */}
          {byAgent && (
            <p className="mt-1.5 text-micro text-faint-foreground">
              {t(($) => $.proposal.human_only)}
            </p>
          )}

          {/* The server guards the decision on state = 'pending', so a second
              reviewer gets a conflict rather than silently overwriting the
              first. Reporting it here is what makes that guard visible. */}
          {decide.isError && (
            <p className="mt-1.5 text-micro text-destructive">
              {t(($) => $.proposal.conflict)}
            </p>
          )}
        </>
      )}

      {!pending && proposal.decide_note ? (
        <p className="mt-1 text-micro text-muted-foreground">{proposal.decide_note}</p>
      ) : null}
    </div>
  );
}
