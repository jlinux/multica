import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import type { Goal, MilestoneProposal } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { IssueGoalSection } from "./issue-goal-section";

const mocks = vi.hoisted(() => ({
  goalsForIssue: [] as Goal[],
  allGoals: [] as Goal[],
  proposals: [] as MilestoneProposal[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQueries: () => [
    { data: mocks.goalsForIssue },
    { data: mocks.allGoals },
    { data: mocks.proposals },
  ],
}));

vi.mock("@multica/core/goals", () => ({
  goalsForIssueOptions: () => ({ queryKey: ["for-issue"] }),
  goalListOptions: () => ({ queryKey: ["goals"] }),
  pendingMilestoneProposalsOptions: () => ({ queryKey: ["pending"] }),
  useDecideMilestoneProposal: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ goalDetail: (id: string) => `/test-workspace/goals/${id}` }),
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

function render() {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/issues/i1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <IssueGoalSection issueId="i1" />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  mocks.goalsForIssue = [];
  mocks.allGoals = [];
  mocks.proposals = [];
});

describe("IssueGoalSection", () => {
  it("renders nothing when the issue serves no goal", () => {
    // An empty card on every issue in a workspace that does not use goals is a
    // permanent tax on the sidebar for a feature that workspace has not
    // adopted.
    const { container } = (() => {
      render();
      return { container: document.body };
    })();
    expect(container.textContent).not.toContain("Goals");
  });

  it("shows the chain direction-first, from inside the work", () => {
    // Standing in the work the reader is asking "what is this ultimately
    // for", so the direction leads. On the goal detail the question is "what
    // is this part of", and the nearest parent leads there instead.
    mocks.goalsForIssue = [goal({ id: "c1", title: "Console 2.1", parent_goal_id: "p1" })];
    mocks.allGoals = [
      goal({ id: "d1", level: 1, title: "Five minute integration" }),
      goal({ id: "p1", level: 2, title: "Console rebuild", parent_goal_id: "d1" }),
      mocks.goalsForIssue[0] as Goal,
    ];
    render();

    const links = screen.getAllByRole("link").map((a) => a.textContent);
    expect(links).toEqual([
      "Five minute integration",
      "Console rebuild",
      "Console 2.1",
    ]);
  });

  it("surfaces only the proposals this issue's runs produced", () => {
    // The person most able to judge a claim made by work on this issue is the
    // one reading this issue. Claims from elsewhere are somebody else's.
    mocks.goalsForIssue = [goal({ id: "c1", title: "Console 2.1" })];
    mocks.proposals = [
      {
        id: "p1",
        milestone_id: "m1",
        proposed_status: "pending_accept",
        proposed_actual_date: null,
        evidence: "From this very issue",
        evidence_refs: [],
        proposed_by_type: "agent",
        proposed_by_id: "a1",
        source_task_id: "t1",
        source_issue_id: "i1",
        state: "pending",
        decided_by_type: null,
        decided_by_id: null,
        decided_at: "",
        decide_note: "",
        created_at: "",
      },
      {
        id: "p2",
        milestone_id: "m2",
        proposed_status: "pending_accept",
        proposed_actual_date: null,
        evidence: "From a different issue entirely",
        evidence_refs: [],
        proposed_by_type: "agent",
        proposed_by_id: "a1",
        source_task_id: "t2",
        source_issue_id: "i2",
        state: "pending",
        decided_by_type: null,
        decided_by_id: null,
        decided_at: "",
        decide_note: "",
        created_at: "",
      },
    ];
    render();

    expect(screen.getByText(/From this very issue/)).toBeInTheDocument();
    expect(screen.queryByText(/From a different issue entirely/)).not.toBeInTheDocument();
  });

  it("appears for a proposal even when the issue is linked to no goal", () => {
    // The link can be removed after a run produced a claim. Hiding the claim
    // with the link would lose it silently.
    mocks.proposals = [
      {
        id: "p1",
        milestone_id: "m1",
        proposed_status: "achieved",
        proposed_actual_date: "2026-10-10",
        evidence: "Orphaned but still awaiting a decision",
        evidence_refs: [],
        proposed_by_type: "agent",
        proposed_by_id: "a1",
        source_task_id: null,
        source_issue_id: "i1",
        state: "pending",
        decided_by_type: null,
        decided_by_id: null,
        decided_at: "",
        decide_note: "",
        created_at: "",
      },
    ];
    render();
    expect(screen.getByText(/Orphaned but still awaiting a decision/)).toBeInTheDocument();
  });
});
