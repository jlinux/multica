import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import type { GoalUsage } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { GoalCostCard } from "./goal-cost-card";

const mocks = vi.hoisted(() => ({
  data: null as GoalUsage | null,
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
  goalUsageOptions: () => ({ queryKey: ["usage"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

const TICK = 10_000_000_000;

function usage(over: Partial<GoalUsage> = {}): GoalUsage {
  return {
    cost_usd_ticks: 0,
    input_tokens: 0,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
    uncosted_input_tokens: 0,
    uncosted_output_tokens: 0,
    agent_runs: 0,
    issues: 0,
    goals: 1,
    by_provider: [],
    ...over,
  };
}

beforeEach(() => {
  mocks.data = null;
  mocks.isPending = false;
  mocks.isError = false;
});

describe("GoalCostCard", () => {
  it("renders nothing when nothing has run", () => {
    // A card reading $0.00 states that the work was free rather than absent.
    mocks.data = usage();
    const { container } = renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the spend and what it was derived from", () => {
    mocks.data = usage({ cost_usd_ticks: 42.5 * TICK, agent_runs: 64, issues: 4 });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    // Cents matter at this scale: rounding $42.50 to $43 quietly loses money
    // at exactly the size a small team notices.
    expect(screen.getByText("$42.50")).toBeInTheDocument();
    expect(screen.getByText("64")).toBeInTheDocument();
    // The claim that makes this number trustworthy at all.
    expect(screen.getByText(/nothing here is entered by hand/)).toBeInTheDocument();
  });

  it("drops cents once the figure is large enough not to need them", () => {
    mocks.data = usage({ cost_usd_ticks: 1247.6 * TICK, agent_runs: 300 });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(screen.getByText("$1,248")).toBeInTheDocument();
  });

  it("reports unpriced tokens instead of folding them in as free", () => {
    // The one reading of this number that would actively mislead: a goal whose
    // spend is unknown must not render as a goal that was cheap.
    mocks.data = usage({
      cost_usd_ticks: 5 * TICK,
      agent_runs: 3,
      uncosted_input_tokens: 400,
      uncosted_output_tokens: 100,
    });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(
      screen.getByText("Plus 500 tokens the provider did not price"),
    ).toBeInTheDocument();
  });

  it("appears for an unpriced goal even with no dollar figure", () => {
    mocks.data = usage({ agent_runs: 2, uncosted_input_tokens: 900 });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(screen.getByText(/did not price/)).toBeInTheDocument();
  });

  it("says when the number came from the tier beneath", () => {
    // Without this an upper-tier figure reads as its own spend rather than as
    // everything under it.
    mocks.data = usage({ cost_usd_ticks: 100 * TICK, agent_runs: 9, goals: 5 });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(screen.getByText("Rolled up from 5 goals")).toBeInTheDocument();
  });

  it("splits by what ran, never by who ran it", () => {
    mocks.data = usage({
      cost_usd_ticks: 78 * TICK,
      agent_runs: 12,
      by_provider: [
        { provider: "anthropic", model: "claude-opus", cost_usd_ticks: 50 * TICK, tokens: 1000, agent_runs: 7 },
        { provider: "openai", model: "codex", cost_usd_ticks: 28 * TICK, tokens: 600, agent_runs: 5 },
      ],
    });
    renderWithI18n(<GoalCostCard goalId="g1" />);
    expect(screen.getByText("claude-opus")).toBeInTheDocument();
    expect(screen.getByText("codex")).toBeInTheDocument();
    // Nothing person-shaped reaches this card, because nothing person-shaped
    // reaches the endpoint behind it.
    expect(document.body.textContent).not.toMatch(/owner|assignee/i);
  });

  it("renders localized copy", () => {
    mocks.data = usage({ cost_usd_ticks: 12 * TICK, agent_runs: 4 });
    renderWithI18n(<GoalCostCard goalId="g1" />, { locale: "zh-Hans" });
    expect(screen.getByText("这个目标花了多少")).toBeInTheDocument();
  });
});
