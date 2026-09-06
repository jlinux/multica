// @vitest-environment node
import { describe, it, expect } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_GOAL,
  EMPTY_GOAL_LIST,
  EMPTY_MILESTONE,
  EMPTY_MILESTONE_LIST,
  EMPTY_MILESTONE_PROPOSAL_LIST,
  GoalListSchema,
  GoalSchema,
  MilestoneListSchema,
  MilestoneProposalListSchema,
  MilestoneSchema,
} from "./schemas";

/**
 * These schemas are the boundary an installed desktop client depends on when
 * it talks to a backend newer than itself. The rule they exist to satisfy is
 * that a drifted response degrades one row, and never blanks the page.
 */
describe("goal layer response schemas", () => {
  it("keeps a goal whose enums the client has never seen", () => {
    // A newer backend adds a tier, a status and a kind. None of them is known
    // here, and all of them must still render: string enums stay z.string()
    // precisely so this parses instead of falling back to an empty goal.
    const fromNewerBackend = {
      id: "g1",
      level: 4,
      title: "Portfolio bet",
      status: "paused_for_review",
      kind: "explore_v2",
      unexpected_field: { nested: true },
    };
    const parsed = parseWithFallback(fromNewerBackend, GoalSchema, EMPTY_GOAL, {
      endpoint: "test",
    });
    expect(parsed.id).toBe("g1");
    expect(parsed.title).toBe("Portfolio bet");
    expect(parsed.status).toBe("paused_for_review");
    expect(parsed.level).toBe(4);
  });

  it("falls back rather than throwing on a malformed goal", () => {
    // id is the one field with no default: a row that cannot be addressed is
    // not a row, and returning it would put an unkeyable entry in the tree.
    expect(
      parseWithFallback({ title: "no id" }, GoalSchema, EMPTY_GOAL, { endpoint: "test" }),
    ).toBe(EMPTY_GOAL);
    expect(parseWithFallback(null, GoalSchema, EMPTY_GOAL, { endpoint: "test" })).toBe(EMPTY_GOAL);
    expect(parseWithFallback("<html>502</html>", GoalSchema, EMPTY_GOAL, { endpoint: "test" })).toBe(
      EMPTY_GOAL,
    );
  });

  it("defaults assignable to false when the backend stops sending it", () => {
    // The safe direction is the only acceptable one here: a goal that looks
    // assignable would offer an agent a three-year direction to work on.
    const parsed = parseWithFallback({ id: "g1" }, GoalSchema, EMPTY_GOAL, { endpoint: "test" });
    expect(parsed.assignable).toBe(false);
  });

  it("returns an empty list rather than undefined when the list key is missing", () => {
    const parsed = parseWithFallback({ total: 3 }, GoalListSchema, EMPTY_GOAL_LIST, {
      endpoint: "test",
    });
    expect(parsed.goals).toEqual([]);
  });

  it("drops the whole list when one goal in it is unaddressable", () => {
    // zod arrays are all-or-nothing, so a single bad row costs the response.
    // Asserted so the behaviour is a decision on record rather than a surprise
    // the day a backfill writes one null id.
    const parsed = parseWithFallback(
      { goals: [{ id: "g1" }, { title: "no id" }], total: 2 },
      GoalListSchema,
      EMPTY_GOAL_LIST,
      { endpoint: "test" },
    );
    expect(parsed).toBe(EMPTY_GOAL_LIST);
  });

  it("keeps a milestone whose type and adoption check are unknown", () => {
    const parsed = parseWithFallback(
      {
        id: "m1",
        type: "third_party_certified",
        adoption_check: "webhook",
        planned_date: "2026-10-10",
      },
      MilestoneSchema,
      EMPTY_MILESTONE,
      { endpoint: "test" },
    );
    expect(parsed.type).toBe("third_party_certified");
    expect(parsed.adoption_check).toBe("webhook");
    expect(parsed.planned_date).toBe("2026-10-10");
  });

  it("keeps original_planned_date distinct from planned_date", () => {
    // On-time attainment is measured against the first date ever planned. If
    // the schema ever collapsed the two, a rescheduled milestone would start
    // reporting as on time and nothing would fail loudly.
    const parsed = parseWithFallback(
      { id: "m1", original_planned_date: "2026-10-03", planned_date: "2026-10-10" },
      MilestoneSchema,
      EMPTY_MILESTONE,
      { endpoint: "test" },
    );
    expect(parsed.original_planned_date).toBe("2026-10-03");
    expect(parsed.planned_date).toBe("2026-10-10");
  });

  it("tolerates a milestone list that arrives as an object without its array", () => {
    const parsed = parseWithFallback({}, MilestoneListSchema, EMPTY_MILESTONE_LIST, {
      endpoint: "test",
    });
    expect(parsed.milestones).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("keeps a proposal's evidence refs whatever shape they take", () => {
    // evidence_refs is deliberately unknown[]: it carries pull request ids,
    // log queries and event counts, and the set grows on the server side.
    const parsed = parseWithFallback(
      {
        proposals: [
          {
            id: "p1",
            evidence: "Three tenants completed an end-to-end import.",
            evidence_refs: [{ kind: "pr", id: 4471 }, "log://query/abc"],
            state: "pending",
          },
        ],
        total: 1,
      },
      MilestoneProposalListSchema,
      EMPTY_MILESTONE_PROPOSAL_LIST,
      { endpoint: "test" },
    );
    expect(parsed.proposals).toHaveLength(1);
    expect(parsed.proposals[0]?.evidence_refs).toHaveLength(2);
  });

  it("falls back on a proposal payload that is not an object at all", () => {
    expect(
      parseWithFallback([], MilestoneProposalListSchema, EMPTY_MILESTONE_PROPOSAL_LIST, {
        endpoint: "test",
      }),
    ).toBe(EMPTY_MILESTONE_PROPOSAL_LIST);
  });
});
