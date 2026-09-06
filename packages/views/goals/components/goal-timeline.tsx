"use client";

import { useMemo } from "react";
import { AlertTriangle, CalendarRange } from "lucide-react";
import { useQueries } from "@tanstack/react-query";
import {
  goalListOptions,
  milestoneTimelineOptions,
  type Goal,
  type Milestone,
} from "@multica/core/goals";
import { projectListOptions } from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useLocale, useT } from "../../i18n";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import {
  daysBetween,
  defaultWindow,
  effectiveDate,
  monthTicks,
  parseCalendarDate,
  positionOf,
  shippedWithoutUseDays,
  type TimelineWindow,
} from "../timeline-window";

/**
 * The roadmap: every goal's milestones laid out on one date axis, grouped by
 * product.
 *
 * Three things make this deliberately not a Gantt chart.
 *
 * It does not end at delivery. A Gantt bar stops when the work ships; here
 * launch is the first of three marks, and the two after it are the ones that
 * say whether shipping was worth anything.
 *
 * A moved date is not erased. When a milestone was rescheduled, a hollow ghost
 * stays at the date it was first promised, tied to its current date by a dotted
 * line. The reschedule is free and un-blamed — it just does not get to rewrite
 * what was said.
 *
 * It draws no effort, no dependencies and no resources. Those belong to the
 * work under a goal, not to the goal, and putting them here would turn a map of
 * outcomes into another schedule to defend.
 *
 * What the reader is meant to look at is the DISTANCE between marks: how long
 * a thing sat shipped before anyone used it. A row with one mark and a long
 * red tail is the finding this whole view exists to produce.
 */
export function GoalTimeline({ today = Date.now() }: { today?: number }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const window = useMemo(() => defaultWindow(today), [today]);
  const from = toCalendarDate(window.start);
  const to = toCalendarDate(window.end);

  const [goalsQuery, milestonesQuery, projectsQuery] = useQueries({
    queries: [
      goalListOptions(wsId),
      milestoneTimelineOptions(wsId, from, to),
      projectListOptions(wsId),
    ],
  });

  const groups = useTimelineGroups(
    goalsQuery.data,
    milestonesQuery.data,
    projectsQuery.data,
    t(($) => $.timeline.no_product),
  );

  const isPending = goalsQuery.isPending || milestonesQuery.isPending;
  const isError = goalsQuery.isError || milestonesQuery.isError;
  const todayPosition = positionOf(startOfUTCDay(today), window);

  return (
    <>
      <p className="mb-4 text-label text-muted-foreground">
        {t(($) => $.timeline.subtitle)}
      </p>

      {isPending ? (
        <TimelineSkeleton label={t(($) => $.page.loading)} />
      ) : isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <AlertTriangle className="size-4" />
            </EmptyMedia>
            <EmptyTitle>{t(($) => $.error.title)}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : groups.length === 0 ? (
        <div className="mx-auto max-w-lg">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <CalendarRange className="size-5" />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.timeline.empty_title)}</EmptyTitle>
              <EmptyDescription>
                {t(($) => $.timeline.empty_description)}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      ) : (
        <div className="rounded-lg border border-surface-border bg-surface shadow-xs">
          {/* Horizontal scroll lives on the chart, never on the page: a body
              that scrolls sideways loses the nav and the header with it. */}
          <div className="overflow-x-auto">
            <div className="min-w-[860px]">
              <MonthScale window={window} />
              {groups.map((group) => (
                <div key={group.key}>
                  <div className="flex items-center gap-2 border-b bg-muted px-3 py-1.5 text-caption font-semibold text-muted-foreground">
                    {group.label}
                    <span className="ml-auto text-micro font-normal text-faint-foreground">
                      {t(($) => $.tier.count, { count: group.rows.length })}
                    </span>
                  </div>
                  {group.rows.map((row) => (
                    <TimelineRow
                      key={row.goal.id}
                      row={row}
                      window={window}
                      todayPosition={todayPosition}
                      today={today}
                    />
                  ))}
                </div>
              ))}
            </div>
          </div>
          <Legend />
        </div>
      )}
    </>
  );
}

interface TimelineRowData {
  goal: Goal;
  milestones: Milestone[];
}

interface TimelineGroup {
  key: string;
  label: string;
  rows: TimelineRowData[];
}

/**
 * Buckets milestones under their goal, and goals under their product.
 *
 * Only goals that actually have a milestone inside the window appear. A goal
 * with nothing to place would occupy a full row to say nothing, and a chart
 * whose rows are mostly empty teaches the reader to skim past the rows that
 * are not.
 */
function useTimelineGroups(
  goals: Goal[] | undefined,
  milestones: Milestone[] | undefined,
  projects: { id: string; title: string }[] | undefined,
  noProductLabel: string,
): TimelineGroup[] {
  return useMemo(() => {
    if (!goals || !milestones) return [];
    const byGoal = new Map<string, Milestone[]>();
    for (const milestone of milestones) {
      const list = byGoal.get(milestone.goal_id);
      if (list) list.push(milestone);
      else byGoal.set(milestone.goal_id, [milestone]);
    }

    const projectNames = new Map((projects ?? []).map((p) => [p.id, p.title]));
    const byId = new Map(goals.map((g) => [g.id, g]));
    const groups = new Map<string, TimelineGroup>();

    for (const goal of goals) {
      const own = byGoal.get(goal.id);
      if (!own || own.length === 0) continue;
      const projectId = resolveProjectId(goal, byId);
      const key = projectId ?? "__none__";
      const label = projectId
        ? (projectNames.get(projectId) ?? noProductLabel)
        : noProductLabel;
      const group = groups.get(key) ?? { key, label, rows: [] };
      group.rows.push({
        goal,
        milestones: [...own].sort(
          (a, b) => (effectiveDate(a) ?? 0) - (effectiveDate(b) ?? 0),
        ),
      });
      groups.set(key, group);
    }

    const ordered = [...groups.values()];
    for (const group of ordered) {
      group.rows.sort((a, b) => {
        const first = effectiveDate(a.milestones[0] as Milestone) ?? 0;
        const second = effectiveDate(b.milestones[0] as Milestone) ?? 0;
        return first - second || a.goal.title.localeCompare(b.goal.title);
      });
    }
    // Goals with no product sort last: they are the exception, and leading
    // with them would make the chart open on its least structured rows.
    return ordered.sort((a, b) => {
      if (a.key === "__none__") return 1;
      if (b.key === "__none__") return -1;
      return a.label.localeCompare(b.label);
    });
  }, [goals, milestones, projects, noProductLabel]);
}

/**
 * The product a goal belongs to, following its alignment upwards.
 *
 * A cycle goal almost never names a project itself: it inherits the product
 * from the product goal above it, which is where the binding lives. Reading
 * only the goal's own project_id put nearly every cycle goal under "No
 * product" and made the grouping useless — the thing it exists to do.
 *
 * The walk stops on a missing or repeated parent so a gap in the response, or
 * a cycle the data should not contain, cannot hang the render.
 */
function resolveProjectId(goal: Goal, byId: Map<string, Goal>): string | null {
  const seen = new Set<string>([goal.id]);
  let cursor: Goal | undefined = goal;
  while (cursor) {
    if (cursor.project_id) return cursor.project_id;
    if (!cursor.parent_goal_id) return null;
    const parent: Goal | undefined = byId.get(cursor.parent_goal_id);
    if (!parent || seen.has(parent.id)) return null;
    seen.add(parent.id);
    cursor = parent;
  }
  return null;
}

const ROW_LABEL_WIDTH = "w-[206px]";

function MonthScale({ window }: { window: TimelineWindow }) {
  const locale = useLocale();
  const ticks = monthTicks(window);
  return (
    <div className="flex border-b bg-surface">
      <div className={cn(ROW_LABEL_WIDTH, "shrink-0 border-r")} />
      <div className="relative h-7 flex-1">
        {ticks.map((tick) => (
          <span
            key={tick.at}
            className="absolute top-1.5 -translate-x-1/2 text-micro whitespace-nowrap text-muted-foreground"
            style={{ left: `${tick.position}%` }}
          >
            {formatMonth(tick.at, locale)}
          </span>
        ))}
      </div>
    </div>
  );
}

interface TimelineRowProps {
  row: TimelineRowData;
  window: TimelineWindow;
  todayPosition: number | null;
  today: number;
}

function TimelineRow({ row, window, todayPosition, today }: TimelineRowProps) {
  const { t } = useT("goals");
  const ticks = monthTicks(window);
  const placed = row.milestones
    .map((milestone) => {
      const at = effectiveDate(milestone);
      const position = at === null ? null : positionOf(at, window);
      return position === null || at === null ? null : { milestone, at, position };
    })
    .filter((entry): entry is { milestone: Milestone; at: number; position: number } => entry !== null);

  const unusedDays = shippedWithoutUseDays(row.milestones, today);
  const launch = placed.find((p) => p.milestone.type === "launch");
  const adoption = placed.find((p) => p.milestone.type === "first_use");
  // Only once the launch has actually happened. Between two planned dates the
  // distance is a plan, and rendering it in the same type as a measured gap
  // would present an intention as a fact.
  const gapDays =
    launch?.milestone.status === "achieved" && adoption
      ? daysBetween(launch.at, adoption.at)
      : null;

  return (
    <div className="flex border-b last:border-b-0">
      <div
        className={cn(
          ROW_LABEL_WIDTH,
          "flex shrink-0 flex-col justify-center gap-0.5 border-r px-3 py-2",
        )}
      >
        {/* The unused warning is not repeated here: it is drawn on the track,
            positioned over the gap it is describing, where the reader can see
            how wide that gap actually is. */}
        <span className="truncate text-label font-medium">{row.goal.title}</span>
      </div>

      <div className="relative min-h-11 flex-1">
        <div aria-hidden className="pointer-events-none absolute inset-0">
          {ticks.map((tick) => (
            <span
              key={tick.at}
              className="absolute inset-y-0 w-px bg-border"
              style={{ left: `${tick.position}%` }}
            />
          ))}
        </div>

        {todayPosition !== null && (
          <span
            aria-hidden
            className="absolute inset-y-0 z-[3] w-px bg-destructive"
            style={{ left: `${todayPosition}%` }}
          />
        )}

        {/* The red tail. A launch with nothing after it grows this until
            someone either confirms a first use or admits the feature is not
            being adopted; every other tool draws that row as a finished bar. */}
        {unusedDays !== null && launch && todayPosition !== null && (
          <>
            <span
              aria-hidden
              className="absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-destructive/40"
              style={{
                left: `${launch.position}%`,
                width: `${Math.max(todayPosition - launch.position, 0)}%`,
              }}
            />
            <span
              className="absolute top-[calc(50%-17px)] -translate-x-1/2 rounded bg-surface px-1 text-micro font-semibold whitespace-nowrap text-destructive"
              style={{ left: `${(launch.position + todayPosition) / 2}%` }}
            >
              {t(($) => $.timeline.unused_days, { count: unusedDays })}
            </span>
          </>
        )}

        {/* Segments between consecutive marks. Solid once both ends have
            actually happened, dashed while either is still a plan. */}
        {placed.slice(0, -1).map((entry, index) => {
          const next = placed[index + 1];
          if (!next) return null;
          const settled =
            entry.milestone.status === "achieved" && next.milestone.status === "achieved";
          return (
            <span
              key={`${entry.milestone.id}-seg`}
              aria-hidden
              className={cn(
                "absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full",
                settled ? "bg-success" : "bg-warning/50",
              )}
              style={{
                left: `${entry.position}%`,
                width: `${Math.max(next.position - entry.position, 0)}%`,
              }}
            />
          );
        })}

        {/* The distance the reader is meant to look at: how long the thing sat
            shipped before anyone used it. */}
        {launch && adoption && gapDays !== null && gapDays > 0 && (
          <span
            className="absolute top-[calc(50%-17px)] -translate-x-1/2 rounded bg-surface px-1 text-micro whitespace-nowrap text-faint-foreground"
            style={{ left: `${(launch.position + adoption.position) / 2}%` }}
          >
            {t(($) => $.timeline.gap_days, { count: gapDays })}
          </span>
        )}

        {placed.map((entry) => (
          <MilestoneMark
            key={entry.milestone.id}
            milestone={entry.milestone}
            position={entry.position}
            window={window}
          />
        ))}
      </div>
    </div>
  );
}

/**
 * One milestone, plus the ghost of where it was first promised.
 *
 * The ghost is the visual half of the rule that a date may only move by
 * leaving a reason behind. Without it a rescheduled milestone looks exactly
 * like one that was always planned for its current date, and the reschedule
 * log becomes something nobody has a reason to open.
 */
function MilestoneMark({
  milestone,
  position,
  window,
}: {
  milestone: Milestone;
  position: number;
  window: TimelineWindow;
}) {
  const { t } = useT("goals");
  const original = parseCalendarDate(milestone.original_planned_date);
  const current = parseCalendarDate(milestone.planned_date);
  const moved = original !== null && current !== null && original !== current;
  const ghostPosition = moved ? positionOf(original, window) : null;

  const label = [
    t(($) => $.milestone[milestoneTypeKey(milestone.type)]),
    milestone.actual_date ?? milestone.planned_date ?? "",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      {ghostPosition !== null && (
        <>
          <span
            aria-hidden
            className="absolute top-1/2 h-px -translate-y-1/2 bg-faint-foreground/50"
            style={{
              left: `${Math.min(ghostPosition, position)}%`,
              width: `${Math.abs(position - ghostPosition)}%`,
            }}
          />
          <span
            title={t(($) => $.timeline.moved_from, {
              date: milestone.original_planned_date ?? "",
            })}
            className="absolute top-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full border border-dashed border-faint-foreground"
            style={{ left: `${ghostPosition}%` }}
          />
        </>
      )}
      <span
        title={label}
        data-position={position}
        className={cn(
          "absolute top-1/2 z-[2] block size-[11px] -translate-x-1/2 -translate-y-1/2 border-2",
          milestone.type === "launch" ? "rounded-full" : "rotate-45 rounded-[2px]",
          MARK_TONE[markTone(milestone)],
        )}
        style={{ left: `${position}%` }}
      />
    </>
  );
}

/**
 * Mark tones. Delay wins over everything else: a milestone that is both in
 * progress and late is, to a reader scanning the chart, late.
 */
function markTone(milestone: Milestone): keyof typeof MARK_TONE {
  if (milestone.status === "achieved") return "done";
  if (milestone.is_delayed) return "late";
  if (milestone.status === "pending_accept" || milestone.status === "in_progress") {
    return "active";
  }
  return "planned";
}

const MARK_TONE = {
  done: "border-success bg-success",
  active: "border-warning bg-warning",
  late: "border-destructive bg-destructive",
  planned: "border-faint-foreground bg-surface",
} as const;

function milestoneTypeKey(type: string): "launch" | "first_use" | "nth_use" {
  if (type === "first_use" || type === "nth_use") return type;
  return "launch";
}

function Legend() {
  const { t } = useT("goals");
  return (
    <div className="flex flex-wrap gap-4 border-t px-3 py-2 text-micro text-muted-foreground">
      <LegendItem className="rounded-full border-success bg-success">
        {t(($) => $.timeline.legend_launch)}
      </LegendItem>
      <LegendItem className="rotate-45 rounded-[2px] border-success bg-success">
        {t(($) => $.timeline.legend_first_use)}
      </LegendItem>
      <LegendItem className="rotate-45 rounded-[2px] border-faint-foreground bg-surface">
        {t(($) => $.timeline.legend_nth_use)}
      </LegendItem>
      <LegendItem className="rounded-full border-dashed border-faint-foreground bg-surface">
        {t(($) => $.timeline.legend_moved)}
      </LegendItem>
      <span className="flex items-center gap-1.5">
        <span aria-hidden className="h-0.5 w-4 rounded-full bg-destructive/40" />
        {t(($) => $.timeline.legend_unused)}
      </span>
    </div>
  );
}

function LegendItem({
  className,
  children,
}: {
  className: string;
  children: React.ReactNode;
}) {
  return (
    <span className="flex items-center gap-1.5">
      <span aria-hidden className={cn("block size-2.5 border-2", className)} />
      {children}
    </span>
  );
}

function TimelineSkeleton({ label }: { label: string }) {
  return (
    <div aria-busy aria-label={label} className="space-y-2">
      <Skeleton className="h-7 w-full rounded-lg" />
      {[0, 1, 2, 3].map((row) => (
        <Skeleton key={row} className="h-11 w-full rounded-lg" />
      ))}
    </div>
  );
}

function startOfUTCDay(at: number): number {
  const date = new Date(at);
  return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
}

function toCalendarDate(at: number): string {
  return new Date(at).toISOString().slice(0, 10);
}

function formatMonth(at: number, locale: string): string {
  // The app's chosen language, not the machine's: an English workspace on a
  // zh-CN laptop was labelling its months "7月".
  //
  // Formatted in UTC because the tick IS a UTC-midnight day: rendering it in
  // the reader's zone would label the first of the month as the last of the
  // previous one for anyone west of Greenwich.
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    timeZone: "UTC",
  }).format(new Date(at));
}
