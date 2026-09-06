/**
 * The goal layer: a planning tier above issues.
 *
 * A goal is never assignable and never enters the task queue. The only thing
 * connecting it to execution is an explicit issue link, which is stored on the
 * goal side alone — see server/internal/goal/README.md for why that shape
 * matters. `assignable` is sent by the server on every goal and is always
 * false, so a client building an assignee picker from a mixed list is told so
 * by the payload rather than by a convention it has to remember.
 */

/** 1 direction (1-3y), 2 product goal (6-12m), 3 cycle goal (2w-3m). */
export type GoalLevel = 1 | 2 | 3;

/** Level 2 only: sustain the base product vs explore a new capability. */
export type GoalKind = "base" | "brk";

export type GoalStatus =
  | "not_started"
  | "in_progress"
  | "at_risk"
  | "done"
  | "archived";

export type GoalActorType = "member" | "agent";

export interface Goal {
  id: string;
  workspace_id: string;
  level: GoalLevel;
  title: string;
  description: string;
  /** The goal one tier up. Null means an orphan goal, a displayed category. */
  parent_goal_id: string | null;
  orphan_reason: string;
  /** Previous cycle goal in a continuation chain (2.0 -> 2.1). */
  prev_goal_id: string | null;
  project_id: string | null;
  kind: GoalKind | null;
  status: GoalStatus;
  owner_type: GoalActorType | null;
  owner_id: string | null;
  cycle: string;
  due_date: string | null;
  /** Recorded after the work already shipped. */
  is_retro: boolean;
  position: number;
  created_at: string;
  updated_at: string;
  /** How many live goals align to this one; drives the unpicked-up warning. */
  child_count: number;
  /** Always false. Present so no caller has to re-derive the rule. */
  assignable: boolean;
}

/** Launch is the first of three stages, not the finish line. */
export type MilestoneType = "launch" | "first_use" | "nth_use";

export type MilestoneStatus =
  | "planned"
  | "in_progress"
  | "pending_accept"
  | "achieved"
  | "cancelled";

/** How "it was really used" gets decided, fixed when the milestone is made. */
export type AdoptionCheck = "verifier" | "analytics" | "agent";

/** `external` is a verifier with no seat, confirming through a signed link. */
export type VerifierType = "member" | "agent" | "external";

export interface Milestone {
  id: string;
  workspace_id: string;
  goal_id: string;
  type: MilestoneType;
  /** Which N an nth_use milestone verifies; null for the other two types. */
  n: number | null;
  title: string;
  value_statement: string;
  /**
   * The first date ever planned, never rewritten. On-time attainment is
   * measured against this, so rescheduling cannot turn a slip into a hit.
   */
  original_planned_date: string | null;
  planned_date: string | null;
  actual_date: string | null;
  status: MilestoneStatus;
  /** An overlay, not a status: a milestone can be in progress AND late. */
  is_delayed: boolean;
  adoption_check: AdoptionCheck | null;
  verifier_type: VerifierType | null;
  verifier_id: string | null;
  verifier_label: string;
  accepted_by_type: VerifierType | null;
  accepted_by_id: string | null;
  accept_note: string;
  accepted_at: string;
  is_retro: boolean;
  created_at: string;
  updated_at: string;
  requires_acceptance: boolean;
  /** Backs the "moved N times" counter on the card. */
  date_change_count: number;
}

export interface MilestoneDateChange {
  id: string;
  from_date: string | null;
  to_date: string | null;
  reason: string;
  changed_by_type: GoalActorType;
  changed_by_id: string;
  created_at: string;
}

export type ReleaseKind = "main" | "hotfix" | "pending";
export type PullRequestSource = "github" | "vcs";

export interface MilestoneRelease {
  id: string;
  milestone_id: string;
  kind: ReleaseKind;
  repo_url: string;
  ref: string;
  tag: string;
  released_at: string | null;
  /**
   * Points at a pull request row this server already holds. Multica stores
   * GitHub App PRs and forgejo/gitea/gitlab PRs in separate tables, so the id
   * alone is ambiguous and the source says which.
   */
  pull_request_id: string | null;
  pull_request_source: PullRequestSource | null;
  /** Release notes for the people who will use the feature. */
  summary: string;
  summary_by_type: GoalActorType | null;
  summary_by_id: string | null;
  created_at: string;
}

export type ProposalState = "pending" | "accepted" | "rejected" | "superseded";

/**
 * An agent's claim that a milestone was reached. It never applies itself: a
 * human accepts. `source_task_id` and `source_issue_id` are what make the
 * claim auditable — they lead back to the run, the diff and the tokens spent
 * behind a one-line assertion that a feature is live.
 */
export interface MilestoneProposal {
  id: string;
  milestone_id: string;
  proposed_status: Extract<MilestoneStatus, "pending_accept" | "achieved">;
  proposed_actual_date: string | null;
  evidence: string;
  evidence_refs: unknown[];
  proposed_by_type: GoalActorType;
  proposed_by_id: string;
  source_task_id: string | null;
  source_issue_id: string | null;
  state: ProposalState;
  decided_by_type: VerifierType | null;
  decided_by_id: string | null;
  decided_at: string;
  decide_note: string;
  created_at: string;
}

/**
 * One issue delivering a goal: a summary, not the whole issue. This list sits
 * under a goal, and a client that needs everything about a row follows it to
 * the issue itself.
 */
export interface GoalIssue {
  id: string;
  number: number;
  title: string;
  status: string;
  priority: string;
  assignee_type: GoalActorType | null;
  assignee_id: string | null;
  linked_at: string;
}

export interface CreateGoalRequest {
  level: GoalLevel;
  title: string;
  description?: string;
  parent_goal_id?: string | null;
  orphan_reason?: string;
  prev_goal_id?: string | null;
  project_id?: string | null;
  kind?: GoalKind | null;
  status?: GoalStatus;
  owner_type?: GoalActorType | null;
  owner_id?: string | null;
  cycle?: string;
  due_date?: string | null;
  is_retro?: boolean;
  position?: number;
}

/**
 * Every field is optional and the server re-validates the MERGED goal, not the
 * fields that arrived — patching one at a time is what would otherwise let an
 * invariant break in two legal-looking steps. `level` is absent because a
 * goal's tier is fixed at creation.
 *
 * An explicit empty string on `parent_goal_id` means "unalign this goal",
 * which is a different request from omitting the field.
 */
export type UpdateGoalRequest = Partial<Omit<CreateGoalRequest, "level" | "is_retro">>;

export interface CreateMilestoneRequest {
  type: MilestoneType;
  n?: number;
  title: string;
  value_statement?: string;
  planned_date: string;
  actual_date?: string | null;
  status?: MilestoneStatus;
  adoption_check?: AdoptionCheck | null;
  adoption_config?: Record<string, unknown>;
  verifier_type?: VerifierType | null;
  verifier_id?: string | null;
  verifier_label?: string;
  is_retro?: boolean;
}

/**
 * Workspace goal metrics.
 *
 * There is no per-person field and no parameter that would produce one. That
 * is the product decision: attainment that can be sliced by person becomes a
 * performance instrument, and the data stops being true the moment it is one.
 */
export interface GoalMetrics {
  cycle: string;
  goals: number;
  aligned_upper: number;
  upper_goals: number;
  launched: number;
  adopted: number;
  achieved_milestones: number;
  on_time_milestones: number;
  overdue: number;
  retro_goals: number;
  orphan_goals: number;
}

export interface GoalListResponse {
  goals: Goal[];
  total: number;
}

export interface MilestoneListResponse {
  milestones: Milestone[];
  total: number;
}
