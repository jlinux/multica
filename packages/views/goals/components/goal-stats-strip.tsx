"use client";

import { Info } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { goalMetricsOptions, type GoalMetrics } from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

/**
 * The workspace's goal numbers, and the sentence that says what they are not.
 *
 * Every figure here is a team-and-period aggregate. There is no breakdown by
 * person, no parameter that would produce one, and no setting that turns one
 * on — the endpoint cannot express it either. That is the product decision the
 * whole layer depends on: attainment that can be sliced by person becomes a
 * performance instrument, and within a quarter the reasons people write for
 * moving a date turn into whatever is safe to write down. The numbers are only
 * worth reading while nobody is scored on them.
 *
 * The note is rendered, not left in a doc, because a reader who cannot tell
 * whether this is being used to measure them will behave as though it is.
 */
export function GoalStatsStrip({ cycle }: { cycle?: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const { data, isPending, isError } = useQuery(goalMetricsOptions(wsId, cycle));

  // Nothing to say about an empty workspace. A strip of zeroes teaches a new
  // team that the feature is a dashboard before they have used it as a plan.
  if (isPending || isError || !data || data.goals === 0) return null;

  const stats = visibleStats(data);
  if (stats.length === 0) return null;

  return (
    <div className="mb-4">
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-surface-border bg-surface-border sm:grid-cols-3 lg:grid-cols-5">
        {stats.map((stat) => (
          <div key={stat.key} className="bg-surface px-3 py-2">
            <div className="text-micro whitespace-nowrap text-faint-foreground">
              {t(($) => $.stats[stat.key])}
            </div>
            <div
              className={cn(
                "text-title font-semibold tabular-nums",
                stat.tone === "warn" && "text-warning",
                stat.tone === "bad" && "text-destructive",
              )}
            >
              {stat.value}
              {stat.of !== undefined && (
                <span className="text-caption font-medium text-muted-foreground">
                  /{stat.of}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
      <p className="mt-1.5 flex items-start gap-1.5 text-micro text-faint-foreground">
        <Info className="mt-0.5 size-3 shrink-0" aria-hidden />
        {t(($) => $.stats.aggregate_note)}
      </p>
    </div>
  );
}

type StatKey = "alignment" | "launched" | "adopted" | "on_time" | "overdue" | "retro";

interface Stat {
  key: StatKey;
  value: number;
  of?: number;
  tone?: "warn" | "bad";
}

/**
 * A figure is shown only once it can mean something.
 *
 * An alignment rate with no upper tier, an on-time rate with nothing achieved,
 * an adoption rate with nothing launched: each of those renders as a confident
 * number computed from nothing. Leaving them out until their denominator
 * exists is the difference between a summary and a dashboard that lies early.
 */
function visibleStats(m: GoalMetrics): Stat[] {
  const stats: Stat[] = [];
  if (m.upper_goals > 0) {
    stats.push({
      key: "alignment",
      value: m.aligned_upper,
      of: m.upper_goals,
      tone: m.aligned_upper < m.upper_goals ? "warn" : undefined,
    });
  }
  if (m.launched > 0) {
    stats.push({ key: "launched", value: m.launched });
    // The distance between launched and adopted is the finding this whole
    // layer exists to produce, so adoption is shown against launches rather
    // than against every goal.
    stats.push({
      key: "adopted",
      value: m.adopted,
      of: m.launched,
      tone: m.adopted < m.launched ? "warn" : undefined,
    });
  }
  if (m.achieved_milestones > 0) {
    stats.push({
      key: "on_time",
      value: m.on_time_milestones,
      of: m.achieved_milestones,
    });
  }
  if (m.overdue > 0) {
    stats.push({ key: "overdue", value: m.overdue, tone: "bad" });
  }
  if (m.retro_goals > 0) {
    stats.push({ key: "retro", value: m.retro_goals, of: m.goals });
  }
  return stats;
}
