import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { goalKeys, milestoneKeys } from "./queries";
import type {
  CreateGoalRequest,
  CreateMilestoneRequest,
  Goal,
  UpdateGoalRequest,
} from "./types";

/**
 * Mutations for the goal layer.
 *
 * Almost nothing here is optimistic, and that is a decision rather than an
 * omission. The house rule allows optimism only when the outcome is locally
 * predictable, the user stays on the same screen, failure is rare and rollback
 * is trivial. Goal writes fail the first test: tier alignment, the milestone
 * state machine and proposal decisions are all validated on the server against
 * rows this client has not loaded, so the local guess would be wrong often
 * enough to matter. `useUpdateGoal` is the one exception, and only for the
 * fields whose outcome really is local.
 *
 * Every hook takes `wsId` explicitly instead of reading the workspace
 * provider, so it stays usable from code that runs outside one.
 */

export function useCreateGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateGoalRequest) => api.createGoal(data),
    // Awaits the server before touching the cache: a goal the server refused
    // on a tier rule must never appear in the tree, even briefly.
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
    },
  });
}

/**
 * Patches the determinate fields locally and lets the server settle the rest.
 *
 * Title, description, status, cycle and position are exactly as they are typed.
 * Alignment is not: changing a parent can change `child_count` on two other
 * goals and can be refused outright, so the whole tree is invalidated on
 * settle rather than guessed at here.
 */
export function useUpdateGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateGoalRequest) =>
      api.updateGoal(id, data),
    onMutate: async ({ id, ...data }) => {
      const key = goalKeys.detail(wsId, id);
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<Goal>(key);
      const local: Partial<Goal> = {};
      if (data.title !== undefined) local.title = data.title;
      if (data.description !== undefined) local.description = data.description;
      if (data.status !== undefined) local.status = data.status;
      if (data.cycle !== undefined) local.cycle = data.cycle;
      if (data.position !== undefined) local.position = data.position;
      qc.setQueryData<Goal>(key, (old) => (old ? { ...old, ...local } : old));
      return { previous, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(goalKeys.detail(wsId, ctx.id), ctx.previous);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: goalKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
    },
  });
}

/**
 * Deleting a goal detaches its children and removes its milestones, on the
 * server, in one transaction. Nothing is removed from the cache first: the
 * caller navigates away from a deleted goal, and an optimistic removal that
 * the server then refuses would leave the user on a page for a goal that still
 * exists.
 */
export function useDeleteGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteGoal(id),
    onSettled: () => {
      // The whole tree, not one key: children became orphan goals and their
      // rows moved sections in the panorama.
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: milestoneKeys.all(wsId) });
    },
  });
}

export function useLinkGoalIssue(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, issueId }: { goalId: string; issueId: string }) =>
      api.linkGoalIssue(goalId, issueId),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: goalKeys.issues(wsId, vars.goalId) });
      qc.invalidateQueries({ queryKey: goalKeys.forIssue(wsId, vars.issueId) });
    },
  });
}

export function useUnlinkGoalIssue(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, issueId }: { goalId: string; issueId: string }) =>
      api.unlinkGoalIssue(goalId, issueId),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: goalKeys.issues(wsId, vars.goalId) });
      qc.invalidateQueries({ queryKey: goalKeys.forIssue(wsId, vars.issueId) });
    },
  });
}

export function useCreateMilestone(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, ...data }: { goalId: string } & CreateMilestoneRequest) =>
      api.createMilestone(goalId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: milestoneKeys.forGoal(wsId, vars.goalId) });
    },
  });
}

/**
 * Moves a milestone through its state machine. Not optimistic: reaching
 * `achieved` also records who accepted it and when, which this client cannot
 * produce, and an adoption milestone is refused outright when the caller is an
 * agent.
 */
export function useUpdateMilestoneStatus(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...data
    }: {
      id: string;
      goalId: string;
      status: string;
      actual_date?: string | null;
      accept_note?: string;
    }) => api.updateMilestoneStatus(id, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: milestoneKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: milestoneKeys.forGoal(wsId, vars.goalId) });
    },
  });
}

/**
 * Moving a date always writes a reason, so the reschedule log and the counter
 * on the card both change with it. Both are invalidated rather than patched:
 * the log is the record that makes an overdue milestone impossible to lose
 * quietly, and it must not be shown from a guess.
 */
export function useRescheduleMilestone(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...data
    }: {
      id: string;
      goalId: string;
      planned_date: string;
      reason: string;
    }) => api.rescheduleMilestone(id, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: milestoneKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: milestoneKeys.dateChanges(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: milestoneKeys.forGoal(wsId, vars.goalId) });
      qc.invalidateQueries({ queryKey: milestoneKeys.timeline(wsId, "", "") });
    },
  });
}

export function useCreateMilestoneRelease(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...data
    }: {
      id: string;
      kind: string;
      repo_url?: string;
      ref?: string;
      tag?: string;
      released_at?: string | null;
      pull_request_id?: string | null;
      pull_request_source?: string | null;
      summary?: string;
    }) => api.createMilestoneRelease(id, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: milestoneKeys.releases(wsId, vars.id) });
    },
  });
}

/**
 * Accepts or rejects an agent's claim that a milestone was reached.
 *
 * Deliberately not optimistic. The server refuses `accepted` from an agent —
 * including for its own proposal — and returns 409 when someone else already
 * decided, so a local guess would routinely show a decision that did not
 * happen. On acceptance the milestone moves in the same transaction, which is
 * why its cache is invalidated here too.
 */
export function useDecideMilestoneProposal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      proposalId,
      ...data
    }: {
      proposalId: string;
      milestoneId: string;
      goalId?: string;
      state: "accepted" | "rejected";
      decide_note?: string;
    }) => api.decideMilestoneProposal(proposalId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: milestoneKeys.proposals(wsId, vars.milestoneId) });
      qc.invalidateQueries({ queryKey: milestoneKeys.pendingProposals(wsId) });
      qc.invalidateQueries({ queryKey: milestoneKeys.detail(wsId, vars.milestoneId) });
      if (vars.goalId) {
        qc.invalidateQueries({ queryKey: milestoneKeys.forGoal(wsId, vars.goalId) });
      }
    },
  });
}
