"use client";

import { useState } from "react";
import { useRescheduleMilestone, type Milestone } from "@multica/core/goals";
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
 * Moving a milestone's date.
 *
 * The reason is required, and the dialog says why in a line the reader can
 * check: the first date planned stays on the milestone and attainment is
 * measured against it. That sentence is doing real work. Without it the
 * mandatory reason reads as an accusation, people write "delay" to get past
 * the field, and the reschedule log — the record that keeps an overdue
 * milestone from disappearing quietly — fills with nothing.
 */
export function RescheduleDialog({
  milestone,
  goalId,
  open,
  onOpenChange,
}: {
  milestone: Milestone;
  goalId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const reschedule = useRescheduleMilestone(wsId);

  const [plannedDate, setPlannedDate] = useState(milestone.planned_date ?? "");
  const [reason, setReason] = useState("");

  const canSubmit =
    plannedDate !== "" &&
    plannedDate !== milestone.planned_date &&
    reason.trim() !== "" &&
    !reschedule.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.form.reschedule)}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3">
          <Field label={t(($) => $.form.planned_date)}>
            <Input
              type="date"
              value={plannedDate}
              onChange={(event) => setPlannedDate(event.target.value)}
              className="w-48"
            />
          </Field>

          <Field
            label={t(($) => $.form.reschedule_reason)}
            required
            requiredLabel={t(($) => $.form.required)}
          >
            <Textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder={t(($) => $.form.reschedule_reason_placeholder)}
              rows={2}
              autoFocus
            />
          </Field>

          <p className="text-caption text-muted-foreground">
            {t(($) => $.form.reschedule_hint)}
          </p>

          {reschedule.isError && (
            <p className="text-caption text-destructive">
              {(reschedule.error as Error | undefined)?.message ?? t(($) => $.error.title)}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.form.cancel)}
          </Button>
          <Button
            disabled={!canSubmit}
            onClick={() =>
              reschedule.mutate(
                { id: milestone.id, goalId, planned_date: plannedDate, reason: reason.trim() },
                { onSuccess: () => onOpenChange(false) },
              )
            }
          >
            {reschedule.isPending
              ? t(($) => $.form.saving)
              : t(($) => $.form.reschedule_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
