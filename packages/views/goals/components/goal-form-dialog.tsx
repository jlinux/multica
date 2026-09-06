"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  goalListOptions,
  useCreateGoal,
  type GoalKind,
  type GoalLevel,
} from "@multica/core/goals";
import { projectListOptions } from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Field } from "./form-field";

/**
 * Creating a goal.
 *
 * The form asks for what the tier rules require and nothing else, and it asks
 * in the order the rules are checked, so a refusal from the server is never
 * about a field the reader has not reached yet. Everything optional — owner,
 * description, dates — is either absent or last.
 *
 * The alignment picker offers only goals one tier up, because that is the only
 * alignment the server will accept. Offering the rest and rejecting the choice
 * afterwards would teach the tier rule through an error message.
 */
export function GoalFormDialog({
  open,
  onOpenChange,
  defaultLevel = 3,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultLevel?: GoalLevel;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const create = useCreateGoal(wsId);
  const { data: goals } = useQuery(goalListOptions(wsId));
  const { data: projects } = useQuery(projectListOptions(wsId));

  const [level, setLevel] = useState<GoalLevel>(defaultLevel);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [parentId, setParentId] = useState("");
  const [orphanReason, setOrphanReason] = useState("");
  const [kind, setKind] = useState<GoalKind>("base");
  const [projectId, setProjectId] = useState("");
  const [cycle, setCycle] = useState("");

  const parentCandidates = useMemo(
    () => (goals ?? []).filter((goal) => goal.level === level - 1),
    [goals, level],
  );

  const needsOrphanReason = level !== 1 && parentId === "";
  const canSubmit =
    title.trim() !== "" &&
    (level !== 2 || projectId !== "") &&
    (!needsOrphanReason || orphanReason.trim() !== "") &&
    !create.isPending;

  const submit = () => {
    create.mutate(
      {
        level,
        title: title.trim(),
        description: description.trim(),
        parent_goal_id: parentId || null,
        orphan_reason: needsOrphanReason ? orphanReason.trim() : "",
        kind: level === 2 ? kind : null,
        project_id: projectId || null,
        cycle: cycle.trim(),
      },
      {
        // Closed only once the server has accepted it. A dialog that closes
        // optimistically and then fails a tier rule leaves the reader with no
        // form to correct and no idea what happened.
        onSuccess: () => {
          onOpenChange(false);
          setTitle("");
          setDescription("");
          setOrphanReason("");
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.form.create_goal)}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3">
          <Field label={t(($) => $.form.tier)}>
            <div className="flex gap-1">
              {([1, 2, 3] as GoalLevel[]).map((candidate) => (
                <Button
                  key={candidate}
                  type="button"
                  size="sm"
                  variant={level === candidate ? "default" : "outline"}
                  onClick={() => {
                    setLevel(candidate);
                    // The old parent belongs to the old tier's rule set, so
                    // keeping it would submit an alignment the server refuses.
                    setParentId("");
                  }}
                >
                  {t(($) =>
                    $.tier[candidate === 1 ? "direction" : candidate === 2 ? "product" : "cycle"],
                  )}
                </Button>
              ))}
            </div>
          </Field>

          <Field label={t(($) => $.form.title)} required requiredLabel={t(($) => $.form.required)}>
            <Input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={t(($) => $.form.title_placeholder)}
              autoFocus
            />
          </Field>

          {level === 2 && (
            <>
              <Field label={t(($) => $.form.kind)}>
                <div className="flex gap-1">
                  {(["base", "brk"] as GoalKind[]).map((candidate) => (
                    <Button
                      key={candidate}
                      type="button"
                      size="sm"
                      variant={kind === candidate ? "default" : "outline"}
                      onClick={() => setKind(candidate)}
                    >
                      {t(($) => $.kind[candidate])}
                    </Button>
                  ))}
                </div>
              </Field>
              <Field
                label={t(($) => $.form.project)}
                required
                requiredLabel={t(($) => $.form.required)}
              >
                <select
                  value={projectId}
                  onChange={(event) => setProjectId(event.target.value)}
                  className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-body"
                >
                  <option value="">{t(($) => $.form.project_none)}</option>
                  {(projects ?? []).map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.title}
                    </option>
                  ))}
                </select>
              </Field>
            </>
          )}

          {level !== 1 && (
            <Field label={t(($) => $.form.parent)}>
              <select
                value={parentId}
                onChange={(event) => setParentId(event.target.value)}
                className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-body"
              >
                <option value="">{t(($) => $.form.parent_none)}</option>
                {parentCandidates.map((goal) => (
                  <option key={goal.id} value={goal.id}>
                    {goal.title}
                  </option>
                ))}
              </select>
            </Field>
          )}

          {/* Only when the goal really is standing alone. Asking for a reason
              while a parent is selected would read as a second, contradictory
              question. */}
          {needsOrphanReason && (
            <Field
              label={t(($) => $.form.orphan_reason)}
              required
              requiredLabel={t(($) => $.form.required)}
            >
              <Input
                value={orphanReason}
                onChange={(event) => setOrphanReason(event.target.value)}
                placeholder={t(($) => $.form.orphan_reason_placeholder)}
              />
            </Field>
          )}

          <Field label={t(($) => $.form.description)}>
            <Textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              rows={2}
            />
          </Field>

          <Field label={t(($) => $.form.cycle)}>
            <Input
              value={cycle}
              onChange={(event) => setCycle(event.target.value)}
              placeholder={t(($) => $.form.cycle_placeholder)}
            />
          </Field>

          {create.isError && (
            <p className="text-caption text-destructive">
              {(create.error as Error | undefined)?.message ?? t(($) => $.error.title)}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.form.cancel)}
          </Button>
          <Button disabled={!canSubmit} onClick={submit}>
            {create.isPending
              ? t(($) => $.form.saving)
              : t(($) => $.form.create_goal_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
