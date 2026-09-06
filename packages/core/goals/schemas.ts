import { z } from "zod";
import type {
  Goal,
  GoalListResponse,
  Milestone,
  MilestoneListResponse,
  MilestoneProposal,
  MilestoneRelease,
} from "./types";

/**
 * Response schemas for the goal layer.
 *
 * These live beside the domain rather than in `api/schemas.ts` for the same
 * reason the goal tables are their own tables: this feature is developed
 * against a moving upstream and every line it adds to a shared file is a
 * conflict waiting to happen. `parseWithFallback` does not care where the
 * schema is declared.
 *
 * Every schema is deliberately LENIENT. String enums stay `z.string()` so an
 * unknown value from a newer backend still parses instead of blanking the
 * page, and every object is `.loose()` so added fields survive. An installed
 * desktop client talks to backends newer than itself; the job here is to keep
 * rendering, not to police the contract.
 */

const nullableString = z.string().nullable().default(null);

export const GoalSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    // Not z.union([z.literal(1), ...]): a tier this client does not know about
    // should reach the UI as a number it can group under "other", not blank
    // the whole panorama.
    level: z.number().default(3),
    title: z.string().default(""),
    description: z.string().default(""),
    parent_goal_id: nullableString,
    orphan_reason: z.string().default(""),
    prev_goal_id: nullableString,
    project_id: nullableString,
    kind: nullableString,
    status: z.string().default("not_started"),
    owner_type: nullableString,
    owner_id: nullableString,
    cycle: z.string().default(""),
    due_date: nullableString,
    is_retro: z.boolean().default(false),
    position: z.number().default(0),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
    child_count: z.number().default(0),
    // Defaulting to false is the safe direction: a backend that stops sending
    // the field must not make goals look assignable.
    assignable: z.boolean().default(false),
  })
  .loose();

export const GoalListSchema = z
  .object({
    goals: z.array(GoalSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const MilestoneSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    goal_id: z.string().default(""),
    type: z.string().default("launch"),
    n: z.number().nullable().default(null),
    title: z.string().default(""),
    value_statement: z.string().default(""),
    original_planned_date: nullableString,
    planned_date: nullableString,
    actual_date: nullableString,
    status: z.string().default("planned"),
    is_delayed: z.boolean().default(false),
    adoption_check: nullableString,
    verifier_type: nullableString,
    verifier_id: nullableString,
    verifier_label: z.string().default(""),
    accepted_by_type: nullableString,
    accepted_by_id: nullableString,
    accept_note: z.string().default(""),
    accepted_at: z.string().default(""),
    is_retro: z.boolean().default(false),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
    requires_acceptance: z.boolean().default(false),
    date_change_count: z.number().default(0),
  })
  .loose();

export const MilestoneListSchema = z
  .object({
    milestones: z.array(MilestoneSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const MilestoneReleaseSchema = z
  .object({
    id: z.string(),
    milestone_id: z.string().default(""),
    kind: z.string().default("main"),
    repo_url: z.string().default(""),
    ref: z.string().default(""),
    tag: z.string().default(""),
    released_at: nullableString,
    pull_request_id: nullableString,
    pull_request_source: nullableString,
    summary: z.string().default(""),
    summary_by_type: nullableString,
    summary_by_id: nullableString,
    created_at: z.string().default(""),
  })
  .loose();

export const MilestoneReleaseListSchema = z
  .object({
    releases: z.array(MilestoneReleaseSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const MilestoneProposalSchema = z
  .object({
    id: z.string(),
    milestone_id: z.string().default(""),
    proposed_status: z.string().default("pending_accept"),
    proposed_actual_date: nullableString,
    evidence: z.string().default(""),
    evidence_refs: z.array(z.unknown()).default([]),
    proposed_by_type: z.string().default("agent"),
    proposed_by_id: z.string().default(""),
    source_task_id: nullableString,
    source_issue_id: nullableString,
    state: z.string().default("pending"),
    decided_by_type: nullableString,
    decided_by_id: nullableString,
    decided_at: z.string().default(""),
    decide_note: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const MilestoneProposalListSchema = z
  .object({
    proposals: z.array(MilestoneProposalSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const MilestoneDateChangeListSchema = z
  .object({
    date_changes: z
      .array(
        z
          .object({
            id: z.string(),
            from_date: nullableString,
            to_date: nullableString,
            reason: z.string().default(""),
            changed_by_type: z.string().default("member"),
            changed_by_id: z.string().default(""),
            created_at: z.string().default(""),
          })
          .loose(),
      )
      .default([]),
    total: z.number().default(0),
  })
  .loose();

export const GoalIssueIDsSchema = z
  .object({
    issue_ids: z.array(z.string()).default([]),
    total: z.number().default(0),
  })
  .loose();

/**
 * Fallbacks. Each one renders as an empty-but-valid row rather than a hole:
 * a goal with no title still occupies its place in the tree, which is what
 * lets the rest of the panorama draw when one response drifts.
 */
export const EMPTY_GOAL: Goal = {
  id: "",
  workspace_id: "",
  level: 3,
  title: "",
  description: "",
  parent_goal_id: null,
  orphan_reason: "",
  prev_goal_id: null,
  project_id: null,
  kind: null,
  status: "not_started",
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

export const EMPTY_GOAL_LIST: GoalListResponse = { goals: [], total: 0 };

export const EMPTY_MILESTONE: Milestone = {
  id: "",
  workspace_id: "",
  goal_id: "",
  type: "launch",
  n: null,
  title: "",
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
};

export const EMPTY_MILESTONE_LIST: MilestoneListResponse = {
  milestones: [],
  total: 0,
};

export const EMPTY_MILESTONE_RELEASE_LIST: { releases: MilestoneRelease[]; total: number } = {
  releases: [],
  total: 0,
};

export const EMPTY_MILESTONE_PROPOSAL_LIST: {
  proposals: MilestoneProposal[];
  total: number;
} = { proposals: [], total: 0 };
