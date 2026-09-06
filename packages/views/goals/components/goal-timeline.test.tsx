import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import type { Goal, Milestone } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { GoalTimeline } from "./goal-timeline";
import { parseCalendarDate } from "../timeline-window";

const mocks = vi.hoisted(() => ({
  goals: [] as Goal[],
  milestones: [] as Milestone[],
  projects: [] as { id: string; title: string }[],
  isPending: false,
  isError: false,
}));

// useQueries returns one result per query, in the order the component asks:
// goals, milestones, projects.
vi.mock("@tanstack/react-query", () => ({
  useQueries: () => [
    { data: mocks.goals, isPending: mocks.isPending, isError: mocks.isError },
    { data: mocks.milestones, isPending: mocks.isPending, isError: mocks.isError },
    { data: mocks.projects, isPending: false, isError: false },
  ],
}));

vi.mock("@multica/core/goals", () => ({
  goalListOptions: () => ({ queryKey: ["goals"] }),
  milestoneTimelineOptions: () => ({ queryKey: ["milestones", "timeline"] }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const TODAY = parseCalendarDate("2026-10-14") as number;

function goal(over: Partial<Goal> & { id: string }): Goal {
  return {
    workspace_id: "workspace-1",
    level: 3,
    title: over.id,
    description: "",
    parent_goal_id: null,
    orphan_reason: "",
    prev_goal_id: null,
    project_id: null,
    kind: null,
    status: "in_progress",
    owner_type: null,
    owner_id: null,
    cycle: "",
    due_date: null,
    is_retro: false,
    position: 0,
    created_at: "",
    updated_at: "",
    child_count: 0,
    assignable: false,
    ...over,
  };
}

function milestone(over: Partial<Milestone> & { id: string; goal_id: string }): Milestone {
  return {
    workspace_id: "workspace-1",
    type: "launch",
    n: null,
    title: over.id,
    value_statement: "",
    original_planned_date: over.planned_date ?? null,
    planned_date: null,
    actual_date: null,
    status: "planned",
    is_delayed: false,
    adoption_check: null,
    verifier_type: null,
    verifier_id: null,
    verifier_label: "",
    accepted_by_type: null,
    accepted_by_id: null,
    accept_note: "",
    accepted_at: "",
    is_retro: false,
    created_at: "",
    updated_at: "",
    requires_acceptance: false,
    date_change_count: 0,
    ...over,
  };
}

beforeEach(() => {
  mocks.goals = [];
  mocks.milestones = [];
  mocks.projects = [];
  mocks.isPending = false;
  mocks.isError = false;
});

describe("GoalTimeline", () => {
  it("shows nothing but an empty state when no goal has a milestone", () => {
    // A goal with nothing to place would occupy a full row to say nothing, and
    // a chart whose rows are mostly empty teaches the reader to skim.
    mocks.goals = [goal({ id: "g1", title: "Console 2.1" })];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.getByText("Nothing on the roadmap yet")).toBeInTheDocument();
    expect(screen.queryByText("Console 2.1")).not.toBeInTheDocument();
  });

  it("groups rows under their product and sorts goals with no product last", () => {
    mocks.projects = [{ id: "p1", title: "Console" }];
    mocks.goals = [
      goal({ id: "g1", title: "Console 2.0", project_id: "p1" }),
      goal({ id: "g2", title: "Annual signup tool" }),
    ];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "g1", planned_date: "2026-10-03" }),
      milestone({ id: "m2", goal_id: "g2", planned_date: "2026-10-05" }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    // The count sits in the same header element, so match the label node
    // itself rather than the container's combined text.
    const headings = screen
      .getAllByText((_, node) => {
        const own = node?.firstChild;
        return own?.nodeType === Node.TEXT_NODE &&
          /^(Console|No product)$/.test(own.textContent?.trim() ?? "");
      })
      .map((n) => n.firstChild?.textContent?.trim());
    // Goals with no product sort last: they are the exception, and leading
    // with them would open the chart on its least structured rows.
    expect(headings).toEqual(["Console", "No product"]);
  });

  it("reports how long a goal has been shipped with nobody using it", () => {
    // The finding this whole view exists to produce. Every other tool draws
    // this row as a finished bar.
    mocks.goals = [goal({ id: "g1", title: "Export centre" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        type: "launch",
        status: "achieved",
        planned_date: "2026-08-08",
        actual_date: "2026-08-08",
        original_planned_date: "2026-08-08",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    // Exactly once. It used to be drawn in the row label as well, which spent
    // two lines on one fact and pushed the goal title into a truncation.
    expect(screen.getAllByText("Shipped 67 days ago, still unused")).toHaveLength(1);
  });

  it("does not label a distance between two dates that have not happened", () => {
    // Between two plans the gap is an intention. Rendering it in the same type
    // as a measured gap would present that intention as a fact.
    mocks.goals = [goal({ id: "g1", title: "Console 2.1" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        type: "launch",
        status: "in_progress",
        planned_date: "2026-10-10",
        original_planned_date: "2026-10-10",
      }),
      milestone({
        id: "m2",
        goal_id: "g1",
        type: "first_use",
        status: "planned",
        planned_date: "2026-11-20",
        original_planned_date: "2026-11-20",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.queryByText("41 days")).not.toBeInTheDocument();
  });

  it("says nothing about a launch whose adoption date has not come due", () => {
    // A commitment in good standing is not a gap. Flagging it would train
    // people to read the warning as noise and then miss the real one.
    mocks.goals = [goal({ id: "g1", title: "Billing v1" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        type: "launch",
        status: "achieved",
        planned_date: "2026-09-05",
        actual_date: "2026-09-05",
        original_planned_date: "2026-09-05",
      }),
      milestone({
        id: "m2",
        goal_id: "g1",
        type: "first_use",
        status: "planned",
        planned_date: "2026-11-30",
        original_planned_date: "2026-11-30",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.queryByText(/still unused/)).not.toBeInTheDocument();
  });

  it("labels the distance from launch to first real use", () => {
    // Not the length of a bar: the gap. That number is the answer to "how long
    // did this sit shipped before it was worth anything".
    mocks.goals = [goal({ id: "g1", title: "Console 2.0" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        type: "launch",
        status: "achieved",
        planned_date: "2026-09-05",
        actual_date: "2026-09-05",
        original_planned_date: "2026-09-05",
      }),
      milestone({
        id: "m2",
        goal_id: "g1",
        type: "first_use",
        status: "achieved",
        planned_date: "2026-10-10",
        actual_date: "2026-10-10",
        original_planned_date: "2026-10-10",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.getByText("35 days")).toBeInTheDocument();
  });

  it("keeps a ghost at the date a rescheduled milestone was first promised", () => {
    // The visual half of "a date may only move by leaving a reason behind".
    // Without it a rescheduled milestone looks exactly like one that was always
    // planned for its current date, and the reschedule log stops being read.
    mocks.goals = [goal({ id: "g1", title: "Console 2.1" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        planned_date: "2026-10-10",
        original_planned_date: "2026-10-03",
        date_change_count: 1,
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.getByTitle("Was 2026-10-03")).toBeInTheDocument();
  });

  it("draws no ghost for a milestone that never moved", () => {
    mocks.goals = [goal({ id: "g1", title: "Console 2.1" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        planned_date: "2026-10-10",
        original_planned_date: "2026-10-10",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.queryByTitle(/^Was /)).not.toBeInTheDocument();
  });

  it("drops a milestone that falls outside the window instead of pinning it to an edge", () => {
    // A mark clamped to the edge reads as "it happens here", which is a claim
    // the chart has no business making about a date it is not showing.
    mocks.goals = [goal({ id: "g1", title: "Far future" })];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "g1", planned_date: "2028-01-01" }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    // The goal still earns its row — the server returned a milestone for it —
    // but nothing is drawn on the track.
    expect(screen.getByText("Far future")).toBeInTheDocument();
    expect(screen.queryByTitle(/Launch · /)).not.toBeInTheDocument();
  });

  it("marks a delayed milestone as late even while it is in progress", () => {
    mocks.goals = [goal({ id: "g1", title: "Integration wizard" })];
    mocks.milestones = [
      milestone({
        id: "m1",
        goal_id: "g1",
        status: "in_progress",
        is_delayed: true,
        planned_date: "2026-10-20",
        original_planned_date: "2026-09-28",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    const mark = screen.getByTitle("Launch · 2026-10-20");
    expect(mark.className).toContain("border-destructive");
  });

  it("places every mark at its own date, not at the left edge", () => {
    // The regression: position was computed, used for the connecting lines,
    // and never applied to the marks themselves — so the lines were right and
    // every marker sat stacked on the track's left edge.
    mocks.goals = [goal({ id: "g1", title: "Console 2.0" })];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "g1", planned_date: "2026-08-15" }),
      milestone({
        id: "m2",
        goal_id: "g1",
        type: "first_use",
        planned_date: "2026-12-15",
        original_planned_date: "2026-12-15",
      }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    const early = screen.getByTitle("Launch · 2026-08-15");
    const late = screen.getByTitle("First real use · 2026-12-15");
    const earlyLeft = Number(early.getAttribute("data-position"));
    const lateLeft = Number(late.getAttribute("data-position"));

    expect(early.style.left).toBe(`${earlyLeft}%`);
    expect(late.style.left).toBe(`${lateLeft}%`);
    expect(earlyLeft).toBeGreaterThan(0);
    expect(lateLeft).toBeGreaterThan(earlyLeft);
  });

  it("groups a cycle goal under the product its parent names", () => {
    // A cycle goal almost never carries a project of its own; it inherits one
    // from the product goal above it. Reading only its own project_id put
    // nearly every row under "No product" and made the grouping useless.
    mocks.projects = [{ id: "p1", title: "Console" }];
    mocks.goals = [
      goal({ id: "d1", level: 1, title: "Five minute integration" }),
      goal({ id: "p1g", level: 2, title: "Console rebuild", parent_goal_id: "d1", project_id: "p1" }),
      goal({ id: "c1", title: "Console 2.0", parent_goal_id: "p1g" }),
    ];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "c1", planned_date: "2026-10-03" }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);

    expect(screen.getByText("Console")).toBeInTheDocument();
    expect(screen.queryByText("No product")).not.toBeInTheDocument();
  });

  it("does not hang on an alignment chain that loops", () => {
    mocks.goals = [
      goal({ id: "a", title: "A", parent_goal_id: "b" }),
      goal({ id: "b", title: "B", parent_goal_id: "a" }),
    ];
    mocks.milestones = [milestone({ id: "m1", goal_id: "a", planned_date: "2026-10-03" })];
    renderWithI18n(<GoalTimeline today={TODAY} />);
    expect(screen.getByText("A")).toBeInTheDocument();
  });

  it("renders an accessible busy state while loading", () => {
    mocks.isPending = true;
    renderWithI18n(<GoalTimeline today={TODAY} />);
    expect(screen.getByLabelText("Loading goals")).toHaveAttribute("aria-busy");
  });

  it("renders localized copy", () => {
    mocks.goals = [goal({ id: "g1", title: "控制台 2.0" })];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "g1", planned_date: "2026-10-03" }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />, { locale: "zh-Hans" });
    expect(screen.getByText("未关联产品")).toBeInTheDocument();
    expect(screen.getByText("上线")).toBeInTheDocument();
  });

  it("labels months in the app's language, not the machine's", () => {
    // Intl defaults to the system locale, so an English workspace on a zh-CN
    // laptop was rendering its month scale as "7月" next to English copy.
    mocks.goals = [goal({ id: "g1", title: "Console 2.0" })];
    mocks.milestones = [
      milestone({ id: "m1", goal_id: "g1", planned_date: "2026-10-03" }),
    ];
    renderWithI18n(<GoalTimeline today={TODAY} />);
    expect(screen.getByText("Oct")).toBeInTheDocument();
  });
});
