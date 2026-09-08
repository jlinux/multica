// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Goal } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { GoalDetailPage } from "./goal-detail-page";

/**
 * MUL-7107, on this page: the rail belongs to the scroll region, not to the
 * branch that happens to be rendering. When only the loaded content carried a
 * column, the page jumped inward the moment the query resolved, and the
 * not-found state sat on a different width again — three readings of one page.
 *
 * The behaviour under test is "every branch reads the same rail", so each
 * branch is driven separately and asked the same question. Kept out of
 * goal-detail-page.test.tsx because the sentinel below has to replace PAGE_RAIL
 * for the whole module graph, and that file asserts on real rendered copy.
 *
 * PAGE_RAIL is overridden with a sentinel because its real value is an ordinary
 * Tailwind class an element could carry by coincidence.
 */
const RAIL = "rail-sentinel";
// The literal is repeated inside the factory rather than referenced: vi.mock is
// hoisted above every const in the file, so RAIL is still in its temporal dead
// zone when the factory runs.
vi.mock("../../layout/page-header", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../layout/page-header")>()),
  PAGE_RAIL: "rail-sentinel",
}));

const state = vi.hoisted(() => ({
  goal: null as Goal | null,
  isPending: false,
  isError: false,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueries: () => [
    { data: state.goal ?? undefined, isPending: state.isPending, isError: state.isError },
    { data: [], isPending: false, isError: false },
    { data: [], isPending: false, isError: false },
    { data: [], isPending: false, isError: false },
    { data: [], isPending: false, isError: false },
  ],
  useQuery: () => ({ data: undefined, isPending: false, isError: false }),
}));

vi.mock("@multica/core/goals", () => ({
  goalDetailOptions: () => ({ queryKey: ["goal"] }),
  goalMilestonesOptions: () => ({ queryKey: ["milestones"] }),
  goalIssuesOptions: () => ({ queryKey: ["issues"] }),
  goalListOptions: () => ({ queryKey: ["goals"] }),
  pendingMilestoneProposalsOptions: () => ({ queryKey: ["proposals", "pending"] }),
  goalUsageOptions: () => ({ queryKey: ["usage"] }),
  useDecideMilestoneProposal: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
  useUpdateMilestoneStatus: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    goals: () => "/test-workspace/goals",
    goalDetail: (id: string) => `/test-workspace/goals/${id}`,
    issueDetail: (id: string) => `/test-workspace/issues/${id}`,
  }),
}));

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/goals/g1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

function renderPage(): HTMLElement[] {
  const { container } = renderWithI18n(
    <NavigationProvider value={adapter()}>
      <GoalDetailPage goalId="g1" />
    </NavigationProvider>,
  );
  return Array.from(container.querySelectorAll<HTMLElement>(`.${RAIL}`));
}

describe("GoalDetailPage rail", () => {
  it("reads on the rail while the goal is loading", () => {
    state.goal = null;
    state.isPending = true;
    state.isError = false;
    expect(renderPage()).toHaveLength(1);
  });

  it("reads on the same rail when the goal is not found", () => {
    state.goal = null;
    state.isPending = false;
    state.isError = true;
    expect(renderPage()).toHaveLength(1);
  });

  it("reads on the same rail once the goal resolves", () => {
    state.goal = {
      id: "g1",
      workspace_id: "workspace-1",
      level: 3,
      title: "Ship the importer",
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
    };
    state.isPending = false;
    state.isError = false;

    const rails = renderPage();
    expect(rails).toHaveLength(1);
    // The rail has to be an ANCESTOR of the goal's own content, not a sibling
    // of it — that is the difference between the page reading on the rail and
    // merely containing an element that does. The page chrome above it (the
    // back link in PageHeader) stays full-bleed on purpose, so this reaches for
    // the title rather than the first header in the document.
    expect(rails[0]).toContainElement(screen.getByText("Ship the importer"));
  });
});
