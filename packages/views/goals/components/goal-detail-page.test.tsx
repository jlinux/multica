import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  Goal,
  GoalIssue,
  Milestone,
  MilestoneProposal,
} from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { GoalDetailPage } from "./goal-detail-page";

const mocks = vi.hoisted(() => ({
  goal: null as Goal | null,
  milestones: [] as Milestone[],
  issues: [] as GoalIssue[],
  allGoals: [] as Goal[],
  proposals: [] as MilestoneProposal[],
  updateStatus: vi.fn(),
  isPending: false,
  isError: false,
}));

// useQueries returns one result per query, in the order the page asks:
// goal, milestones, issues, all goals.
vi.mock("@tanstack/react-query", () => ({
  useQueries: () => [
    { data: mocks.goal ?? undefined, isPending: mocks.isPending, isError: mocks.isError },
    { data: mocks.milestones, isPending: false, isError: false },
    { data: mocks.issues, isPending: false, isError: false },
    { data: mocks.allGoals, isPending: false, isError: false },
    { data: mocks.proposals, isPending: false, isError: false },
  ],
  useQuery: () => ({ data: undefined }),
}));

vi.mock("@multica/core/goals", () => ({
  goalDetailOptions: () => ({ queryKey: ["goal"] }),
  goalMilestonesOptions: () => ({ queryKey: ["milestones"] }),
  goalIssuesOptions: () => ({ queryKey: ["issues"] }),
  goalListOptions: () => ({ queryKey: ["goals"] }),
  pendingMilestoneProposalsOptions: () => ({ queryKey: ["proposals", "pending"] }),
  useDecideMilestoneProposal: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
  useUpdateMilestoneStatus: () => ({ mutate: mocks.updateStatus, isPending: false, isError: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    goals: () => "/test-workspace/goals",
    goalDetail: (id: string) => `/test-workspace/goals/${id}`,
    issueDetail: (id: string) => `/test-workspace/issues/${id}`,
  }),
}));

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

function milestone(over: Partial<Milestone> & { id: string }): Milestone {
  return {
    workspace_id: "workspace-1",
    goal_id: "c1",
    type: "launch",
    n: null,
    title: over.id,
    value_statement: "",
    original_planned_date: null,
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

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/goals/c1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderDetail(locale?: "zh-Hans") {
  renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <GoalDetailPage goalId="c1" />
    </NavigationProvider>,
    locale ? { locale } : {},
  );
}

beforeEach(() => {
  mocks.goal = goal({ id: "c1", title: "Console 2.1" });
  mocks.milestones = [];
  mocks.issues = [];
  mocks.allGoals = [];
  mocks.proposals = [];
  mocks.updateStatus.mockClear();
  mocks.isPending = false;
  mocks.isError = false;
});

describe("GoalDetailPage", () => {
  it("states in words that the goal cannot be handed to an agent", () => {
    // A reader who has just seen a title, an owner and a status looks for an
    // assignee picker next. Its absence explains nothing on its own, and the
    // reason is the single idea the whole feature rests on.
    renderDetail();
    expect(
      screen.getByText("This goal cannot be assigned to an agent"),
    ).toBeInTheDocument();
    expect(screen.getByText(/never appear in My Issues/)).toBeInTheDocument();
  });

  it("shows the alignment chain nearest parent first", () => {
    mocks.goal = goal({ id: "c1", title: "Console 2.1", parent_goal_id: "p1" });
    mocks.allGoals = [
      goal({ id: "d1", level: 1, title: "Five minute integration" }),
      goal({ id: "p1", level: 2, title: "Console rebuild", parent_goal_id: "d1" }),
      mocks.goal,
    ];
    renderDetail();

    const links = screen.getAllByRole("link").map((a) => a.textContent);
    expect(links).toContain("Console rebuild");
    expect(links).toContain("Five minute integration");
    expect(links.indexOf("Console rebuild")).toBeLessThan(
      links.indexOf("Five minute integration"),
    );
  });

  it("does not hang on an alignment chain that loops", () => {
    mocks.goal = goal({ id: "c1", title: "A", parent_goal_id: "c2" });
    mocks.allGoals = [mocks.goal, goal({ id: "c2", title: "B", parent_goal_id: "c1" })];
    renderDetail();
    expect(screen.getByRole("heading", { name: "A" })).toBeInTheDocument();
  });

  it("says how adoption will be decided, on the milestone row itself", () => {
    // It is the term of the promise. Two owners who mean different things by
    // "used" make the adoption rate meaningless, and burying the method in an
    // edit form is how they end up meaning different things.
    mocks.milestones = [
      milestone({
        id: "m1",
        type: "first_use",
        title: "First real batch import",
        planned_date: "2026-10-10",
        adoption_check: "agent",
        requires_acceptance: true,
      }),
    ];
    renderDetail();
    expect(
      screen.getByText("An agent checks and reports evidence"),
    ).toBeInTheDocument();
  });

  it("shows the reschedule counter where the date is", () => {
    // A record of a moved date is only useful next to the date. Two clicks
    // away in a log, nobody opens it.
    mocks.milestones = [
      milestone({
        id: "m1",
        title: "Batch actions available",
        planned_date: "2026-10-10",
        original_planned_date: "2026-10-03",
        date_change_count: 2,
      }),
    ];
    renderDetail();
    expect(screen.getByText("Moved 2 times")).toBeInTheDocument();
  });

  it("lets a person finish a milestone without an agent in the loop", async () => {
    // Before this the only route to `achieved` was accepting an agent's
    // proposal, which made the whole three-stage arc depend on having agents.
    const user = userEvent.setup();
    mocks.milestones = [
      milestone({ id: "m1", title: "Ship it", planned_date: "2026-10-10" }),
    ];
    renderDetail();

    await user.click(screen.getByRole("button", { name: "Mark reached" }));
    expect(mocks.updateStatus).toHaveBeenCalledWith(
      expect.objectContaining({ id: "m1", goalId: "c1", status: "achieved" }),
    );
    // Today, not a blank: the common case is recording something that just
    // happened, and a backdated one is a reschedule followed by this.
    const call = mocks.updateStatus.mock.calls[0]?.[0] as { actual_date?: string };
    expect(call?.actual_date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it("offers a handoff, not a finish, on a milestone that needs acceptance", async () => {
    // One button that fails for half the milestones is worse than two that
    // each say what they do. An adoption claim has to reach whoever decides it.
    const user = userEvent.setup();
    mocks.milestones = [
      milestone({
        id: "m1",
        type: "first_use",
        title: "First real use",
        planned_date: "2026-10-10",
        adoption_check: "verifier",
        requires_acceptance: true,
      }),
    ];
    renderDetail();

    expect(screen.queryByRole("button", { name: "Mark reached" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Hand over for acceptance" }));
    expect(mocks.updateStatus).toHaveBeenCalledWith(
      expect.objectContaining({ id: "m1", status: "pending_accept" }),
    );
  });

  it("offers the finish once an adoption milestone has been handed over", () => {
    mocks.milestones = [
      milestone({
        id: "m1",
        type: "first_use",
        title: "First real use",
        status: "pending_accept",
        planned_date: "2026-10-10",
        adoption_check: "verifier",
        requires_acceptance: true,
      }),
    ];
    renderDetail();
    expect(screen.getByRole("button", { name: "Mark reached" })).toBeInTheDocument();
  });

  it("offers no finish action on a milestone already reached", () => {
    mocks.milestones = [
      milestone({
        id: "m1",
        title: "Ship it",
        status: "achieved",
        planned_date: "2026-10-10",
        actual_date: "2026-10-09",
      }),
    ];
    renderDetail();
    expect(screen.queryByRole("button", { name: "Mark reached" })).not.toBeInTheDocument();
  });

  it("guides a goal with no milestones toward a launch date", () => {
    renderDetail();
    expect(screen.getByText("No milestones yet")).toBeInTheDocument();
    expect(screen.getByText(/Give this goal a launch date/)).toBeInTheDocument();
  });

  it("lists the work below the seam and marks the rows an agent holds", () => {
    mocks.issues = [
      {
        id: "i1",
        number: 826,
        title: "Keyboard shortcut system",
        status: "in_progress",
        priority: "high",
        assignee_type: "agent",
        assignee_id: "a1",
        linked_at: "",
      },
      {
        id: "i2",
        number: 841,
        title: "Release notes",
        status: "todo",
        priority: "none",
        assignee_type: "member",
        assignee_id: "u1",
        linked_at: "",
      },
    ];
    renderDetail();

    const agentRow = screen.getByText("Keyboard shortcut system").closest("a");
    const memberRow = screen.getByText("Release notes").closest("a");
    expect(agentRow).not.toBeNull();
    expect(within(agentRow as HTMLElement).getByText("Agent")).toBeInTheDocument();
    expect(within(memberRow as HTMLElement).queryByText("Agent")).not.toBeInTheDocument();
    expect(agentRow).toHaveAttribute("href", "/test-workspace/issues/i1");
  });

  it("reports a goal it cannot load instead of rendering an empty shell", () => {
    mocks.goal = null;
    mocks.isError = true;
    renderDetail();
    expect(
      screen.getByText("That goal does not exist, or is not in this workspace"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/cannot be assigned/)).not.toBeInTheDocument();
  });

  it("renders an accessible busy state while loading", () => {
    mocks.isPending = true;
    renderDetail();
    expect(screen.getByLabelText("Loading goals")).toHaveAttribute("aria-busy");
  });

  it("renders localized copy", () => {
    renderDetail("zh-Hans");
    expect(screen.getByText("此目标不能指派给 agent")).toBeInTheDocument();
  });
});
