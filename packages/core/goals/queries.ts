import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Query keys for the goal layer.
 *
 * Every key carries `wsId`, so switching workspaces cannot serve one
 * workspace's goal tree from another's cache. Hooks take it as an argument
 * rather than reading a provider, so they stay usable outside one.
 */
export interface GoalListFilters {
  level?: number;
  status?: string;
  project_id?: string;
  owner_id?: string;
  parent_goal_id?: string;
  orphan_only?: boolean;
}

export const goalKeys = {
  all: (wsId: string) => ["goals", wsId] as const,
  list: (wsId: string, filters?: GoalListFilters) =>
    [...goalKeys.all(wsId), "list", filters ?? {}] as const,
  detail: (wsId: string, id: string) =>
    [...goalKeys.all(wsId), "detail", id] as const,
  issues: (wsId: string, id: string) =>
    [...goalKeys.all(wsId), "issues", id] as const,
  forIssue: (wsId: string, issueId: string) =>
    [...goalKeys.all(wsId), "for-issue", issueId] as const,
};

export const milestoneKeys = {
  all: (wsId: string) => ["milestones", wsId] as const,
  forGoal: (wsId: string, goalId: string) =>
    [...milestoneKeys.all(wsId), "goal", goalId] as const,
  detail: (wsId: string, id: string) =>
    [...milestoneKeys.all(wsId), "detail", id] as const,
  timeline: (wsId: string, from: string, to: string) =>
    [...milestoneKeys.all(wsId), "timeline", from, to] as const,
  dateChanges: (wsId: string, id: string) =>
    [...milestoneKeys.all(wsId), "date-changes", id] as const,
  releases: (wsId: string, id: string) =>
    [...milestoneKeys.all(wsId), "releases", id] as const,
  proposals: (wsId: string, id: string) =>
    [...milestoneKeys.all(wsId), "proposals", id] as const,
  pendingProposals: (wsId: string) =>
    [...milestoneKeys.all(wsId), "proposals", "pending"] as const,
};

export function goalListOptions(wsId: string, filters?: GoalListFilters) {
  return queryOptions({
    queryKey: goalKeys.list(wsId, filters),
    queryFn: () => api.listGoals(filters),
    select: (data) => data.goals,
  });
}

export function goalDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: goalKeys.detail(wsId, id),
    queryFn: () => api.getGoal(id),
    enabled: id !== "",
  });
}

export function goalIssueIdsOptions(wsId: string, goalId: string) {
  return queryOptions({
    queryKey: goalKeys.issues(wsId, goalId),
    queryFn: () => api.listGoalIssueIds(goalId),
    enabled: goalId !== "",
  });
}

/** "Which goals does this issue serve?", read on the issue detail. */
export function goalsForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: goalKeys.forIssue(wsId, issueId),
    queryFn: () => api.listGoalsForIssue(issueId),
    enabled: issueId !== "",
  });
}

export function goalMilestonesOptions(wsId: string, goalId: string) {
  return queryOptions({
    queryKey: milestoneKeys.forGoal(wsId, goalId),
    queryFn: () => api.listGoalMilestones(goalId),
    select: (data) => data.milestones,
    enabled: goalId !== "",
  });
}

export function milestoneDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: milestoneKeys.detail(wsId, id),
    queryFn: () => api.getMilestone(id),
    enabled: id !== "",
  });
}

/**
 * The roadmap. Keyed by the window because two windows are two different
 * answers, not one answer the client can slice: the server filters by
 * planned_date and a wider window can contain rows a narrower one never saw.
 */
export function milestoneTimelineOptions(wsId: string, from: string, to: string) {
  return queryOptions({
    queryKey: milestoneKeys.timeline(wsId, from, to),
    queryFn: () => api.listMilestonesTimeline(from, to),
    select: (data) => data.milestones,
    enabled: from !== "" && to !== "",
  });
}

export function milestoneDateChangesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: milestoneKeys.dateChanges(wsId, id),
    queryFn: () => api.listMilestoneDateChanges(id),
    enabled: id !== "",
  });
}

export function milestoneReleasesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: milestoneKeys.releases(wsId, id),
    queryFn: () => api.listMilestoneReleases(id),
    enabled: id !== "",
  });
}

export function milestoneProposalsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: milestoneKeys.proposals(wsId, id),
    queryFn: () => api.listMilestoneProposals(id),
    enabled: id !== "",
  });
}

/** Every agent claim still waiting on a human, workspace-wide. */
export function pendingMilestoneProposalsOptions(wsId: string) {
  return queryOptions({
    queryKey: milestoneKeys.pendingProposals(wsId),
    queryFn: () => api.listPendingMilestoneProposals(),
  });
}
