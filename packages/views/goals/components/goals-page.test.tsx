import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Goal } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { GoalsPage } from "./goals-page";

const mocks = vi.hoisted(() => ({
  goals: [] as Goal[],
  isPending: false,
  isError: false,
  refetch: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: mocks.goals,
    isPending: mocks.isPending,
    isError: mocks.isError,
    refetch: mocks.refetch,
  }),
}));

vi.mock("@multica/core/goals", () => ({
  goalListOptions: () => ({ queryKey: ["goals", "workspace-1", "list", {}] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
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

/** The three-tier tree the alignment cases below all read from. */
function alignedTree(): Goal[] {
  return [
    goal({ id: "d1", level: 1, title: "Five minute integration", child_count: 1 }),
    goal({
      id: "p1",
      level: 2,
      title: "Console rebuild",
      kind: "base",
      parent_goal_id: "d1",
      child_count: 2,
    }),
    goal({ id: "c1", level: 3, title: "Console 2.0", parent_goal_id: "p1" }),
    goal({ id: "c2", level: 3, title: "Console 2.1", parent_goal_id: "p1" }),
  ];
}

function cardFor(title: string): HTMLElement {
  const card = screen.getByText(title).closest("button");
  if (!card) throw new Error(`no goal card rendered for ${title}`);
  return card;
}

beforeEach(() => {
  mocks.goals = [];
  mocks.isPending = false;
  mocks.isError = false;
});

describe("GoalsPage", () => {
  it("renders one band per tier that actually holds goals", () => {
    // A workspace that has only started filing cycle goals sees one band, not
    // three labelled voids. Opening on the whole empty model is what makes a
    // first-time reader close the tab, so absent tiers stay absent.
    mocks.goals = [goal({ id: "c1", title: "Console 2.1" })];
    renderWithI18n(<GoalsPage />);

    expect(screen.getByText("Cycle goals")).toBeInTheDocument();
    expect(screen.queryByText("Direction")).not.toBeInTheDocument();
    expect(screen.queryByText("Product goals")).not.toBeInTheDocument();
  });

  it("shows an empty state instead of empty bands when there is nothing at all", () => {
    renderWithI18n(<GoalsPage />);

    expect(screen.getByText("No goals yet")).toBeInTheDocument();
    expect(screen.queryByText("Cycle goals")).not.toBeInTheDocument();
    // The footnote is what tells a reader the single tier is a starting point
    // rather than the whole model, so it is asserted rather than decorative.
    expect(
      screen.getByText(/One tier for now/),
    ).toBeInTheDocument();
  });

  it("files unaligned goals in their own section and never a direction", () => {
    // A product tier exists here, so the parentless cycle goal really is a
    // choice not to align rather than an absent tier.
    mocks.goals = [
      goal({ id: "d1", level: 1, title: "Five minute integration", child_count: 1 }),
      goal({ id: "p1", level: 2, title: "Console rebuild", kind: "base", parent_goal_id: "d1" }),
      goal({
        id: "c9",
        title: "Annual event signup tool",
        orphan_reason: "One-off request; no lasting product line.",
      }),
    ];
    renderWithI18n(<GoalsPage />);

    const section = screen.getByRole("heading", { name: "Unaligned goals" });
    expect(section).toBeInTheDocument();
    expect(screen.getByText("One-off request; no lasting product line.")).toBeInTheDocument();
    // A direction has nothing above it by definition. Listing one here would
    // file every direction under a heading that means something is missing.
    expect(screen.getByText("Direction")).toBeInTheDocument();
  });

  it("does not call a goal unaligned when the tier above it does not exist yet", () => {
    // The recommended starting point is one tier, so this is what a new
    // workspace looks like on day one. Heading its whole board "Unaligned"
    // would report a mistake the team has not made.
    mocks.goals = [
      goal({ id: "c1", title: "Console 2.1" }),
      goal({ id: "c2", title: "Billing v1" }),
    ];
    renderWithI18n(<GoalsPage />);

    expect(screen.getByText("Cycle goals")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Unaligned goals" })).not.toBeInTheDocument();
  });

  it("warns only on upper-tier goals nothing has picked up", () => {
    mocks.goals = [
      goal({ id: "d1", level: 1, title: "Unpicked direction", child_count: 0 }),
      goal({ id: "d2", level: 1, title: "Carried direction", child_count: 1 }),
      goal({ id: "c1", title: "Leaf cycle goal", parent_goal_id: "d2", child_count: 0 }),
    ];
    renderWithI18n(<GoalsPage />);

    const warnings = screen.getAllByText("Nothing is picking this up yet");
    expect(warnings).toHaveLength(1);
    expect(cardFor("Unpicked direction")).toHaveTextContent("Nothing is picking this up yet");
    // A cycle goal is the floor of the model; having no children below it is
    // the normal case and must never be flagged.
    expect(cardFor("Leaf cycle goal")).not.toHaveTextContent("Nothing is picking this up yet");
  });

  it("lights the whole chain above and below a selection and dims the rest", async () => {
    const user = userEvent.setup();
    mocks.goals = [
      ...alignedTree(),
      goal({ id: "d2", level: 1, title: "Unrelated direction", child_count: 1 }),
      goal({ id: "p2", level: 2, title: "Unrelated product", kind: "brk", parent_goal_id: "d2" }),
    ];
    renderWithI18n(<GoalsPage />);

    await user.click(cardFor("Console rebuild"));

    // Ancestors AND descendants: a reader clicking a product goal is asking
    // where it came from and what is carrying it, and half an answer costs
    // them a second click.
    expect(cardFor("Five minute integration")).not.toHaveClass("opacity-30");
    expect(cardFor("Console rebuild")).not.toHaveClass("opacity-30");
    expect(cardFor("Console 2.0")).not.toHaveClass("opacity-30");
    expect(cardFor("Console 2.1")).not.toHaveClass("opacity-30");
    expect(cardFor("Unrelated direction")).toHaveClass("opacity-30");
    expect(cardFor("Unrelated product")).toHaveClass("opacity-30");
  });

  it("keeps the selected card distinguishable by something hover does not touch", async () => {
    const user = userEvent.setup();
    mocks.goals = alignedTree();
    renderWithI18n(<GoalsPage />);

    const card = cardFor("Console rebuild");
    await user.click(card);

    // Hover changes the background, so a selection carried by a background
    // tint alone would visually downgrade to plain hover under the pointer.
    // The ring is the dimension hover never touches.
    expect(card).toHaveAttribute("aria-pressed", "true");
    expect(card.className).toContain("ring-2");
  });

  it("clears the highlight when the same goal is clicked again", async () => {
    const user = userEvent.setup();
    mocks.goals = alignedTree();
    renderWithI18n(<GoalsPage />);

    const card = cardFor("Console rebuild");
    await user.click(card);
    expect(cardFor("Console 2.0")).not.toHaveClass("opacity-30");

    await user.click(card);
    expect(card).toHaveAttribute("aria-pressed", "false");
    expect(cardFor("Console 2.0")).not.toHaveClass("opacity-30");
  });

  it("survives a chain whose parent is missing from the response", async () => {
    const user = userEvent.setup();
    // Paging, a filter or a soft-deleted parent can all produce this. The walk
    // has to stop rather than loop; the regression it guards is a hung render,
    // which no assertion below would reach if it came back.
    mocks.goals = [
      goal({ id: "c1", title: "Orphaned by paging", parent_goal_id: "missing-parent" }),
    ];
    renderWithI18n(<GoalsPage />);

    await user.click(cardFor("Orphaned by paging"));
    expect(cardFor("Orphaned by paging")).toHaveAttribute("aria-pressed", "true");
  });

  it("renders an accessible busy state while loading", () => {
    mocks.isPending = true;
    renderWithI18n(<GoalsPage />);
    expect(screen.getByLabelText("Loading goals")).toHaveAttribute("aria-busy");
  });

  it("offers a retry rather than a blank page when the request fails", async () => {
    const user = userEvent.setup();
    mocks.isError = true;
    renderWithI18n(<GoalsPage />);

    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetch).toHaveBeenCalled();
  });

  it("renders localized copy", () => {
    mocks.goals = [goal({ id: "c1", title: "控制台 2.1" })];
    renderWithI18n(<GoalsPage />, { locale: "zh-Hans" });
    expect(screen.getByText("阶段目标")).toBeInTheDocument();
    expect(screen.getByText("新建目标")).toBeInTheDocument();
  });
});
