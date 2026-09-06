import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MilestoneProposal } from "@multica/core/goals";
import { renderWithI18n } from "../../test/i18n";
import { MilestoneProposalCard } from "./milestone-proposal-card";

const mocks = vi.hoisted(() => ({
  decide: vi.fn(),
  isPending: false,
  isError: false,
}));

vi.mock("@multica/core/goals", () => ({
  useDecideMilestoneProposal: () => ({
    mutate: mocks.decide,
    isPending: mocks.isPending,
    isError: mocks.isError,
  }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

function proposal(over: Partial<MilestoneProposal> = {}): MilestoneProposal {
  return {
    id: "p1",
    milestone_id: "m1",
    proposed_status: "pending_accept",
    proposed_actual_date: null,
    evidence: "Three tenants completed an end-to-end batch import.",
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
    ...over,
  };
}

beforeEach(() => {
  mocks.decide.mockClear();
  mocks.isPending = false;
  mocks.isError = false;
});

describe("MilestoneProposalCard", () => {
  it("shows the evidence without asking to be expanded", () => {
    // A reviewer asked to accept something on an agent's word, who has to
    // click first to see the word, starts clicking Accept without clicking the
    // disclosure. The evidence is the whole point of the card.
    renderWithI18n(<MilestoneProposalCard proposal={proposal()} milestoneId="m1" />);
    expect(
      screen.getByText(/Three tenants completed an end-to-end batch import/),
    ).toBeInTheDocument();
  });

  it("says why there is no option to let the agent decide", () => {
    // An absent control explains nothing. This one is absent on purpose.
    renderWithI18n(<MilestoneProposalCard proposal={proposal()} milestoneId="m1" />);
    expect(
      screen.getByText("An agent cannot accept its own proposal. A person decides."),
    ).toBeInTheDocument();
  });

  it("sends the decision with the milestone it belongs to", async () => {
    const user = userEvent.setup();
    renderWithI18n(
      <MilestoneProposalCard proposal={proposal()} milestoneId="m1" goalId="g1" />,
    );

    await user.click(screen.getByRole("button", { name: "Accept" }));
    expect(mocks.decide).toHaveBeenCalledWith(
      expect.objectContaining({ proposalId: "p1", milestoneId: "m1", goalId: "g1", state: "accepted" }),
    );
  });

  it("carries the reviewer's note through to the decision", async () => {
    const user = userEvent.setup();
    renderWithI18n(<MilestoneProposalCard proposal={proposal()} milestoneId="m1" />);

    await user.type(screen.getByPlaceholderText("Add a note (optional)"), "Checked with the customer");
    await user.click(screen.getByRole("button", { name: "Reject" }));
    expect(mocks.decide).toHaveBeenCalledWith(
      expect.objectContaining({ state: "rejected", decide_note: "Checked with the customer" }),
    );
  });

  it("reports the race instead of leaving the reviewer guessing", () => {
    // The server guards the decision on state = 'pending', so a second
    // reviewer gets a conflict rather than silently overwriting the first.
    // Saying so is what makes that guard visible.
    mocks.isError = true;
    renderWithI18n(<MilestoneProposalCard proposal={proposal()} milestoneId="m1" />);
    expect(screen.getByText("Someone else already decided this one")).toBeInTheDocument();
  });

  it("offers no decision on a proposal that is already settled", () => {
    renderWithI18n(
      <MilestoneProposalCard
        proposal={proposal({ state: "superseded" })}
        milestoneId="m1"
      />,
    );
    expect(screen.queryByRole("button", { name: "Accept" })).not.toBeInTheDocument();
    expect(screen.getByText("Superseded by a newer proposal")).toBeInTheDocument();
  });

  it("renders localized copy", () => {
    renderWithI18n(<MilestoneProposalCard proposal={proposal()} milestoneId="m1" />, {
      locale: "zh-Hans",
    });
    expect(screen.getByText("Agent 提议")).toBeInTheDocument();
    expect(screen.getByText("agent 不能确认自己的提议,必须由人裁决。")).toBeInTheDocument();
  });
});
