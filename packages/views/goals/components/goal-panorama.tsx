"use client";

import { useCallback, useMemo, useState } from "react";
import { AlertTriangle, Plus, Target, Waypoints } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { goalListOptions, type Goal, type GoalLevel } from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { useWorkspacePaths } from "@multica/core/paths";
import { rowLinkInteractiveProps, useRowLink } from "../../navigation";
import { GoalFormDialog } from "./goal-form-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";

/**
 * The goal panorama: direction, product goals and cycle goals in one column,
 * with the alignment between them made visible.
 *
 * Two things about this page are decisions rather than defaults.
 *
 * A tier renders only when it holds goals. A new workspace therefore opens on
 * one band and one empty state instead of three labelled voids, because three
 * voids ask the reader to learn the whole model before they can do anything —
 * and that is the moment they close the tab. The upper tiers appear when the
 * work actually reaches them.
 *
 * Unaligned goals get their own section rather than a warning badge. They are
 * the one-off work that no direction should have to adopt, and the alternative
 * — forcing a parent so the tree looks tidy — is how a goal tree fills up with
 * alignments nobody believes.
 */
export function GoalPanorama() {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  // Built once here rather than inside the map: useRowLink returns a factory
  // precisely so the hook stays out of a loop.
  const rowLinkFor = useRowLink();
  const { data: goals, isPending, isError, refetch } = useQuery(goalListOptions(wsId));

  // The lit chain, or null when nothing is selected. Local because it is a
  // reading aid for one viewer in one moment: nothing about it belongs in a
  // store, a URL or the server.
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const { tiers, orphans, chain } = useGoalTree(goals, selectedId);

  const toggleSelection = useCallback((id: string) => {
    setSelectedId((current) => (current === id ? null : id));
  }, []);

  return (
    <>
      <p className="mb-5 text-label text-muted-foreground">
        {t(($) => $.page.subtitle)}
      </p>
      {isPending ? (
          <GoalsSkeleton label={t(($) => $.page.loading)} />
        ) : isError ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <AlertTriangle className="size-4" />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.error.title)}</EmptyTitle>
            </EmptyHeader>
            <Button variant="outline" size="sm" onClick={() => void refetch()}>
              {t(($) => $.error.retry)}
            </Button>
          </Empty>
        ) : tiers.length === 0 && orphans.length === 0 ? (
          <GoalsEmptyState />
        ) : (
          <div
            // Clicking the background clears the highlight. It is on the
            // container rather than a dedicated button because dismissing a
            // reading aid should not cost a trip to a control.
            onClick={() => setSelectedId(null)}
          >
            {tiers.map((tier, index) => (
              <div key={tier.level}>
                {index > 0 && <AlignmentSeparator label={t(($) => $.tier.aligns_to)} />}
                <TierBand
                  level={tier.level}
                  goals={tier.goals}
                  chain={chain}
                  selectedId={selectedId}
                  onSelect={toggleSelection}
                  hrefFor={(goal) => paths.goalDetail(goal.id)}
                  rowLinkFor={rowLinkFor}
                />
              </div>
            ))}

            {orphans.length > 0 && (
              <section className="mt-8">
                <h2 className="text-label font-semibold">
                  {t(($) => $.orphan.section)}
                </h2>
                <p className="mt-1 mb-2 text-caption text-faint-foreground">
                  {t(($) => $.orphan.hint)}
                </p>
                <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                  {orphans.map((goal) => (
                    <GoalCard
                      key={goal.id}
                      goal={goal}
                      dimmed={chain !== null && !chain.has(goal.id)}
                      selected={goal.id === selectedId}
                      onSelect={toggleSelection}
                      rowLink={rowLinkFor(paths.goalDetail(goal.id), goal.title)}
                    />
                  ))}
                </div>
              </section>
            )}
          </div>
        )}
    </>
  );
}

const TIER_ORDER: GoalLevel[] = [1, 2, 3];

interface Tier {
  level: GoalLevel;
  goals: Goal[];
}

/**
 * Groups goals into tiers, splits off the unaligned ones, and resolves the
 * chain a selection lights up.
 *
 * The chain is ancestors AND descendants, not just one direction: a reader who
 * clicks a product goal is asking "where did this come from and what is
 * carrying it", and answering half of that leaves them clicking twice.
 */
function useGoalTree(goals: Goal[] | undefined, selectedId: string | null) {
  return useMemo(() => {
    const all = goals ?? [];
    const byId = new Map(all.map((g) => [g.id, g]));

    // "Unaligned" only means something when there is a tier to align to.
    //
    // A direction has nothing above it by definition. Less obviously, neither
    // does a cycle goal in a workspace that has not created any product goals
    // yet — which is every new workspace, since the recommended starting point
    // is one tier. Filing that team's entire board under a heading that means
    // "these are missing a parent" would tell them they had done something
    // wrong on their first day, when they had done exactly the right thing.
    // So a goal is unaligned only if the tier above it actually exists here.
    const occupiedTiers = new Set(all.map((g) => g.level));
    const isUnaligned = (g: Goal) =>
      !g.parent_goal_id && g.level !== 1 && occupiedTiers.has((g.level - 1) as GoalLevel);
    const orphans = all.filter(isUnaligned);
    const aligned = all.filter((g) => !isUnaligned(g));

    const tiers: Tier[] = TIER_ORDER.map((level) => ({
      level,
      goals: aligned
        .filter((g) => g.level === level)
        .sort((a, b) => a.position - b.position || a.title.localeCompare(b.title)),
    })).filter((tier) => tier.goals.length > 0);

    if (!selectedId || !byId.has(selectedId)) {
      return { tiers, orphans, chain: null as Set<string> | null };
    }

    const chain = new Set<string>([selectedId]);
    for (let cursor = byId.get(selectedId); cursor?.parent_goal_id; ) {
      const parent = byId.get(cursor.parent_goal_id);
      // A parent that is missing from this response, or a cycle the data
      // should not contain, ends the walk instead of hanging the render.
      if (!parent || chain.has(parent.id)) break;
      chain.add(parent.id);
      cursor = parent;
    }
    let frontier = [selectedId];
    while (frontier.length > 0) {
      const next: string[] = [];
      for (const goal of all) {
        if (goal.parent_goal_id && frontier.includes(goal.parent_goal_id) && !chain.has(goal.id)) {
          chain.add(goal.id);
          next.push(goal.id);
        }
      }
      frontier = next;
    }
    return { tiers, orphans, chain };
  }, [goals, selectedId]);
}

interface TierBandProps {
  level: GoalLevel;
  goals: Goal[];
  chain: Set<string> | null;
  selectedId: string | null;
  onSelect: (id: string) => void;
  hrefFor: (goal: Goal) => string;
  rowLinkFor: ReturnType<typeof useRowLink>;
}

function TierBand({
  level,
  goals,
  chain,
  selectedId,
  onSelect,
  hrefFor,
  rowLinkFor,
}: TierBandProps) {
  const { t } = useT("goals");
  const tierKey = level === 1 ? "direction" : level === 2 ? "product" : "cycle";
  const hintKey = `${tierKey}_hint` as const;

  return (
    <section>
      <div className="flex items-center gap-2 py-1.5">
        <span
          aria-hidden
          className={cn("h-3 w-[3px] rounded-full", TIER_ACCENT[level])}
        />
        <h2 className="text-caption font-semibold text-muted-foreground">
          {t(($) => $.tier[tierKey])}
        </h2>
        <span className="text-micro text-faint-foreground">
          {t(($) => $.tier[hintKey])}
        </span>
        <span className="ml-auto text-micro text-faint-foreground tabular-nums">
          {t(($) => $.tier.count, { count: goals.length })}
        </span>
      </div>
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {goals.map((goal) => (
          <GoalCard
            key={goal.id}
            goal={goal}
            dimmed={chain !== null && !chain.has(goal.id)}
            selected={goal.id === selectedId}
            onSelect={onSelect}
            rowLink={rowLinkFor(hrefFor(goal), goal.title)}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * Tier accents. Deliberately three hues from the chart ramp rather than three
 * shades of one: the bands are peers stacked vertically, and a light-to-dark
 * ramp would read as importance falling off down the page.
 */
const TIER_ACCENT: Record<GoalLevel, string> = {
  1: "bg-chart-1",
  2: "bg-chart-2",
  3: "bg-chart-3",
};

const STATUS_DOT: Record<string, string> = {
  not_started: "bg-faint-foreground",
  in_progress: "bg-brand",
  at_risk: "bg-warning",
  done: "bg-success",
  archived: "bg-faint-foreground",
};

interface GoalCardProps {
  goal: Goal;
  dimmed: boolean;
  selected: boolean;
  onSelect: (id: string) => void;
  rowLink: ReturnType<ReturnType<typeof useRowLink>>;
}

function GoalCard({ goal, dimmed, selected, onSelect, rowLink }: GoalCardProps) {
  const { t } = useT("goals");
  const statusKey = goal.status in STATUS_DOT ? goal.status : "not_started";
  // An upper-tier goal nothing has picked up yet. Cycle goals are the floor,
  // so the absence of children there is not a gap.
  const unpicked = goal.level !== 3 && goal.child_count === 0;

  return (
    <div
      // Opening the goal is the card's primary action, so the whole card is
      // the link. Highlighting the chain is a reading aid and gets its own
      // control rather than stealing the click: a card with a title on it that
      // does not open is a dead end, and that is what it used to be.
      {...rowLink}
      className={cn(
        "relative flex w-full cursor-pointer items-start gap-2.5 rounded-lg border p-3 text-left transition-opacity",
        "bg-surface border-surface-border",
        // The selected state is carried by a ring and a heavier title, not by
        // a background tint: hover changes the background, so a tint-only
        // selection visually downgrades to plain hover the moment the pointer
        // lands on it.
        selected
          ? "ring-2 ring-brand hover:bg-surface-hover"
          : "hover:bg-surface-hover",
        dimmed && "opacity-30",
      )}
    >
      <span
        aria-hidden
        className={cn("mt-[7px] size-1.5 shrink-0 rounded-full", STATUS_DOT[statusKey])}
      />
      <span className="min-w-0 flex-1">
        <span
          className={cn(
            "block truncate text-body",
            selected ? "font-semibold" : "font-medium",
          )}
        >
          {goal.title}
        </span>
        <span className="mt-1 flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
          <span>{t(($) => $.status[statusKey as "in_progress"])}</span>
          {goal.kind === "base" || goal.kind === "brk" ? (
            <Badge variant="secondary" className="text-micro">
              {t(($) => $.kind[goal.kind as "base"])}
            </Badge>
          ) : null}
          {goal.prev_goal_id ? (
            <Badge variant="secondary" className="text-micro">
              {t(($) => $.badge.continues)}
            </Badge>
          ) : null}
          {goal.is_retro ? (
            <Badge variant="secondary" className="text-micro">
              {t(($) => $.badge.retro)}
            </Badge>
          ) : null}
          {!goal.parent_goal_id && goal.level !== 1 ? (
            <Badge variant="secondary" className="text-micro">
              {t(($) => $.orphan.badge)}
            </Badge>
          ) : null}
        </span>
        {unpicked && (
          <span className="mt-1.5 flex items-center gap-1 text-caption text-warning">
            <AlertTriangle className="size-3" aria-hidden />
            {t(($) => $.warning.unpicked)}
          </span>
        )}
        {!goal.parent_goal_id && goal.orphan_reason ? (
          <span className="mt-1.5 block truncate text-caption text-faint-foreground">
            {goal.orphan_reason}
          </span>
        ) : null}
      </span>
      <button
        type="button"
        aria-pressed={selected}
        aria-label={selected ? t(($) => $.chain.clear) : t(($) => $.chain.focus)}
        title={selected ? t(($) => $.chain.clear) : t(($) => $.chain.focus)}
        {...rowLinkInteractiveProps}
        onClick={(event) => {
          rowLinkInteractiveProps.onClick(event);
          onSelect(goal.id);
        }}
        className={cn(
          "shrink-0 rounded p-1 transition-colors",
          selected
            ? "text-brand"
            : "text-faint-foreground hover:bg-surface-selected hover:text-foreground",
        )}
      >
        <Waypoints className="size-3.5" />
      </button>
    </div>
  );
}

function AlignmentSeparator({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 py-3 text-micro text-faint-foreground">
      <span className="h-px flex-1 bg-border" />
      <span>{label}</span>
      <span className="h-px flex-1 bg-border" />
    </div>
  );
}

function GoalsEmptyState() {
  const { t } = useT("goals");
  const [creating, setCreating] = useState(false);
  return (
    <div className="mx-auto max-w-lg">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Target className="size-5" />
          </EmptyMedia>
          <EmptyTitle>{t(($) => $.empty.title)}</EmptyTitle>
          <EmptyDescription>{t(($) => $.empty.description)}</EmptyDescription>
        </EmptyHeader>
        <Button className="gap-1.5" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {t(($) => $.empty.action)}
        </Button>
      </Empty>
      {creating && <GoalFormDialog open onOpenChange={setCreating} />}
      {/* Says out loud that one tier is the starting point, so the reader is
          not left wondering where the rest of the model went. */}
      <p className="mt-2 text-center text-caption text-faint-foreground">
        {t(($) => $.empty.footnote)}
      </p>
    </div>
  );
}

function GoalsSkeleton({ label }: { label: string }) {
  return (
    <div aria-busy aria-label={label} className="space-y-6">
      {[0, 1].map((band) => (
        <div key={band}>
          <Skeleton className="mb-2 h-4 w-28" />
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((card) => (
              <Skeleton key={card} className="h-[76px] rounded-lg" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
