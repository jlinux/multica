"use client";

import { useCallback } from "react";
import { Plus } from "lucide-react";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { PageHeader, PAGE_GUTTER } from "../../layout/page-header";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { GoalPanorama } from "./goal-panorama";
import { GoalTimeline } from "./goal-timeline";

const VIEWS = ["panorama", "timeline"] as const;
type GoalView = (typeof VIEWS)[number];

function isGoalView(value: string | null): value is GoalView {
  return value !== null && (VIEWS as readonly string[]).includes(value);
}

/**
 * The shell both goal views share: one header, one scroll container, and the
 * tabs between them.
 *
 * The active view lives in the URL rather than in component state so it can be
 * linked, refreshed and reopened in a desktop tab. It is written with `replace`
 * rather than `push`: switching between two readings of the same data is not a
 * place a reader expects the back button to return them to, and pushing would
 * bury the page they actually arrived from under a stack of tab flips.
 *
 * The default view carries no parameter at all, so the plain `/goals` URL stays
 * the canonical address of the page.
 */
export function GoalsPage() {
  const { t } = useT("goals");
  const navigation = useNavigation();
  const urlView = navigation.searchParams.get("view");
  const view: GoalView = isGoalView(urlView) ? urlView : "panorama";

  const selectView = useCallback(
    (next: GoalView) => {
      const params = new URLSearchParams(navigation.searchParams);
      if (next === "panorama") params.delete("view");
      else params.set("view", next);
      const query = params.toString();
      navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
    },
    [navigation],
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <h1 className="text-title-sm font-semibold">{t(($) => $.page.title)}</h1>
        <div className="flex-1" />
        <Button size="sm" className="gap-1.5">
          <Plus className="size-4" />
          {t(($) => $.page.new_goal)}
        </Button>
      </PageHeader>

      <div className={cn("flex-1 overflow-y-auto pt-3 pb-16", PAGE_GUTTER)}>
        <div role="tablist" className="mb-4 flex gap-1 border-b">
          {VIEWS.map((candidate) => (
            <button
              key={candidate}
              type="button"
              role="tab"
              aria-selected={view === candidate}
              onClick={() => selectView(candidate)}
              className={cn(
                "-mb-px border-b-2 px-3 py-1.5 text-label transition-colors",
                // Weight and text colour carry the active state, not a
                // background: hover owns the background here too, so a
                // tint-only tab would read as merely hovered under the pointer.
                view === candidate
                  ? "border-foreground font-semibold text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t(($) => $.view[candidate])}
            </button>
          ))}
        </div>

        {view === "timeline" ? <GoalTimeline /> : <GoalPanorama />}
      </div>
    </div>
  );
}
