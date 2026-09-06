"use client";

import { useState } from "react";
import {
  useCreateMilestone,
  type AdoptionCheck,
  type MilestoneType,
} from "@multica/core/goals";
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

const ADOPTION: AdoptionCheck[] = ["verifier", "analytics", "agent"];

/**
 * Adding a milestone.
 *
 * The adoption question appears for the two stages that need it and is asked
 * as a choice between three concrete methods, not as an optional note. It is
 * the term of the promise: two owners who mean different things by "used" make
 * the adoption rate meaningless, and the moment to settle that is now, while
 * someone is thinking about this particular delivery.
 *
 * A launch milestone is not asked, because a launch is settled by the
 * engineering record — a merged pull request, a tag — and inventing a
 * confirmation step for it would be ceremony.
 */
export function MilestoneFormDialog({
  goalId,
  open,
  onOpenChange,
}: {
  goalId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const create = useCreateMilestone(wsId);

  const [type, setType] = useState<MilestoneType>("launch");
  const [title, setTitle] = useState("");
  const [plannedDate, setPlannedDate] = useState("");
  const [valueStatement, setValueStatement] = useState("");
  const [adoption, setAdoption] = useState<AdoptionCheck>("verifier");
  const [verifierLabel, setVerifierLabel] = useState("");
  const [n, setN] = useState(3);

  const needsAdoption = type !== "launch";
  const needsVerifier = needsAdoption && adoption === "verifier";
  const canSubmit =
    title.trim() !== "" &&
    plannedDate !== "" &&
    (!needsVerifier || verifierLabel.trim() !== "") &&
    !create.isPending;

  const submit = () => {
    create.mutate(
      {
        goalId,
        type,
        title: title.trim(),
        planned_date: plannedDate,
        value_statement: valueStatement.trim(),
        n: type === "nth_use" ? n : undefined,
        adoption_check: needsAdoption ? adoption : null,
        // An external verifier has no seat, so the label is the only thing
        // identifying them and the server requires it.
        verifier_type: needsVerifier ? "external" : null,
        verifier_label: needsVerifier ? verifierLabel.trim() : "",
      },
      {
        onSuccess: () => {
          onOpenChange(false);
          setTitle("");
          setPlannedDate("");
          setValueStatement("");
          setVerifierLabel("");
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.form.create_milestone)}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3">
          <Field label={t(($) => $.form.milestone_type)}>
            <div className="flex flex-wrap gap-1">
              {(["launch", "first_use", "nth_use"] as MilestoneType[]).map((candidate) => (
                <Button
                  key={candidate}
                  type="button"
                  size="sm"
                  variant={type === candidate ? "default" : "outline"}
                  onClick={() => setType(candidate)}
                >
                  {t(($) => $.milestone[candidate])}
                </Button>
              ))}
            </div>
          </Field>

          {type === "nth_use" && (
            <Field label={t(($) => $.form.nth)}>
              <Input
                type="number"
                min={2}
                value={n}
                onChange={(event) => setN(Math.max(2, Number(event.target.value) || 2))}
                className="w-24"
              />
            </Field>
          )}

          <Field label={t(($) => $.form.title)} required requiredLabel={t(($) => $.form.required)}>
            <Input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={t(($) => $.form.milestone_title_placeholder)}
              autoFocus
            />
          </Field>

          <Field
            label={t(($) => $.form.planned_date)}
            required
            requiredLabel={t(($) => $.form.required)}
          >
            <Input
              type="date"
              value={plannedDate}
              onChange={(event) => setPlannedDate(event.target.value)}
              className="w-48"
            />
          </Field>

          {needsAdoption && (
            <>
              <Field
                label={t(($) => $.form.adoption)}
                required
                requiredLabel={t(($) => $.form.required)}
              >
                <div className="flex flex-col gap-1">
                  {ADOPTION.map((candidate) => (
                    <Button
                      key={candidate}
                      type="button"
                      size="sm"
                      variant={adoption === candidate ? "default" : "outline"}
                      className="justify-start"
                      onClick={() => setAdoption(candidate)}
                    >
                      {t(($) => $.detail[`adoption_${candidate}` as "adoption_agent"])}
                    </Button>
                  ))}
                </div>
              </Field>
              {needsVerifier && (
                <Field
                  label={t(($) => $.form.verifier_label)}
                  required
                  requiredLabel={t(($) => $.form.required)}
                >
                  <Input
                    value={verifierLabel}
                    onChange={(event) => setVerifierLabel(event.target.value)}
                    placeholder={t(($) => $.form.verifier_label_placeholder)}
                  />
                </Field>
              )}
            </>
          )}

          <Field label={t(($) => $.form.value_statement)}>
            <Textarea
              value={valueStatement}
              onChange={(event) => setValueStatement(event.target.value)}
              placeholder={t(($) => $.form.value_statement_placeholder)}
              rows={2}
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
              : t(($) => $.form.create_milestone_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
