"use client";

import { Label } from "@multica/ui/components/ui/label";

/**
 * One labelled row in the goal forms.
 *
 * The required marker is a word rather than an asterisk. Every field these
 * forms ask for is required by a rule the server will enforce, and a reader
 * who has to learn what the asterisk means before they can tell which fields
 * those are has been given a puzzle instead of a form.
 */
export function Field({
  label,
  required,
  requiredLabel,
  children,
}: {
  label: string;
  required?: boolean;
  requiredLabel?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1">
      <Label className="flex items-center gap-1.5 text-label">
        {label}
        {required && requiredLabel ? (
          <span className="text-micro font-normal text-faint-foreground">
            {requiredLabel}
          </span>
        ) : null}
      </Label>
      {children}
    </div>
  );
}
