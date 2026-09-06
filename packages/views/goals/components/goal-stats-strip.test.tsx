import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import type { GoalMetrics } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { GoalStatsStrip } from "./goal-stats-strip";

const mocks = vi.hoisted(() => ({
  data: null as GoalMetrics | null,
  isPending: false,
  isError: false,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: mocks.data ?? undefined,
    isPending: mocks.isPending,
    isError: mocks.isError,
  }),
}));

vi.mock("@multica/core/goals", () => ({
  goalMetricsOptions: () => ({ queryKey: ["metrics"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

function metrics(over: Partial<GoalMetrics> = {}): GoalMetrics {
  return {
    cycle: "",
    goals: 0,
    aligned_upper: 0,
    upper_goals: 0,
    launched: 0,
    adopted: 0,
    achieved_milestones: 0,
    on_time_milestones: 0,
    overdue: 0,
    retro_goals: 0,
    orphan_goals: 0,
    ...over,
  };
}

beforeEach(() => {
  mocks.data = null;
  mocks.isPending = false;
  mocks.isError = false;
});

describe("GoalStatsStrip", () => {
  it("says on screen that nothing here is per person", () => {
    // A reader who cannot tell whether this is being used to measure them will
    // behave as though it is, and then the reasons they write for moving a
    // date stop being true. The sentence belongs on the page, not in a doc.
    mocks.data = metrics({ goals: 4, launched: 2, adopted: 1 });
    renderWithI18n(<GoalStatsStrip />);
    expect(
      screen.getByText(/no breakdown by person, and there is no setting that adds one/),
    ).toBeInTheDocument();
  });

  it("renders nothing at all for a workspace with no goals", () => {
    // A strip of zeroes teaches a new team that this is a dashboard before
    // they have used it as a plan.
    mocks.data = metrics();
    const { container } = renderWithI18n(<GoalStatsStrip />);
    expect(container).toBeEmptyDOMElement();
  });

  it("omits a figure until its denominator exists", () => {
    // An alignment rate with no upper tier, or an on-time rate with nothing
    // achieved, is a confident number computed from nothing.
    mocks.data = metrics({ goals: 3 });
    renderWithI18n(<GoalStatsStrip />);
    expect(screen.queryByText("Picked up")).not.toBeInTheDocument();
    expect(screen.queryByText("On time")).not.toBeInTheDocument();
    expect(screen.queryByText("Someone used it")).not.toBeInTheDocument();
  });

  it("shows adoption against launches, not against every goal", () => {
    // The distance between "launched" and "someone used it" is the finding
    // this whole layer exists to produce.
    mocks.data = metrics({ goals: 9, launched: 4, adopted: 1 });
    renderWithI18n(<GoalStatsStrip />);
    const adopted = screen.getByText("Someone used it").parentElement;
    expect(adopted).toHaveTextContent("1/4");
  });

  it("marks overdue work rather than folding it into a rate", () => {
    mocks.data = metrics({ goals: 5, overdue: 2 });
    renderWithI18n(<GoalStatsStrip />);
    const overdue = screen.getByText("Overdue").parentElement;
    expect(overdue).toHaveTextContent("2");
  });

  it("renders localized copy", () => {
    mocks.data = metrics({ goals: 4, launched: 2, adopted: 2 });
    renderWithI18n(<GoalStatsStrip />, { locale: "zh-Hans" });
    expect(screen.getByText("已上线")).toBeInTheDocument();
    expect(screen.getByText(/不提供按人下钻/)).toBeInTheDocument();
  });
});
