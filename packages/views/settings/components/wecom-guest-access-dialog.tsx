"use client";

import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import {
  wecomGuestAccessOptions,
  useUpdateWecomGuestAccess,
  validateWecomGuestGroup,
} from "@multica/core/wecom";
import type { UpdateWecomGuestAccessRequest } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function WecomGuestAccessDialog({ wsId, installationId, onClose }: {
  wsId: string;
  installationId: string;
  onClose: () => void;
}) {
  const { t, i18n } = useT("settings");
  const { getMemberName } = useActorName();
  const query = useQuery(wecomGuestAccessOptions(wsId, installationId));
  const mutation = useUpdateWecomGuestAccess(wsId, installationId);
  const [draft, setDraft] = useState<UpdateWecomGuestAccessRequest | null>(null);
  const [groupId, setGroupId] = useState("");
  const [groupError, setGroupError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<"conflict" | "save" | null>(null);
  const prefix = useId();

  // A draft records the policy version the administrator actually reviewed.
  // Cache updates never replace unsaved edits, and cached data is not enough
  // to initialize this form until this dialog's fresh request succeeds.
  useEffect(() => {
    if (draft === null && query.data && query.isFetchedAfterMount && !query.isFetching && !query.isError) {
      setDraft({
        enabled: query.data.enabled,
        allowedGroupIds: [...query.data.allowedGroupIds],
        allowDirectMessages: query.data.allowDirectMessages,
        version: query.data.version,
      });
    }
  }, [draft, query.data, query.isFetchedAfterMount, query.isFetching, query.isError]);

  const busy = mutation.isPending;
  const blocked = busy || query.isError || query.isFetching;
  const emptyScope = draft?.enabled === true && draft.allowedGroupIds.length === 0 && !draft.allowDirectMessages;
  const knownGroups = new Map((query.data?.groups ?? []).map((group) => [group.chatId, group.name]));
  const groupIds = [...new Set([...(draft?.allowedGroupIds ?? []), ...knownGroups.keys()])];

  async function reload() {
    const result = await query.refetch();
    if (result.isError || !result.data) return;
    setDraft({
      enabled: result.data.enabled,
      allowedGroupIds: [...result.data.allowedGroupIds],
      allowDirectMessages: result.data.allowDirectMessages,
      version: result.data.version,
    });
    setSaveError(null);
    setGroupError(null);
    setGroupId("");
  }

  function addGroup() {
    if (!draft || blocked) return;
    const id = groupId.trim();
    const error = validateWecomGuestGroup(id, draft.allowedGroupIds);
    if (error) {
      switch (error) {
        case "empty": setGroupError(t(($) => $.wecom.guest_access.validation_empty)); break;
        case "duplicate": setGroupError(t(($) => $.wecom.guest_access.validation_duplicate)); break;
        case "wildcard": setGroupError(t(($) => $.wecom.guest_access.validation_wildcard)); break;
        case "too_long": setGroupError(t(($) => $.wecom.guest_access.validation_too_long)); break;
        case "too_many": setGroupError(t(($) => $.wecom.guest_access.validation_group_limit)); break;
        default: return;
      }
      return;
    }
    setDraft({ ...draft, allowedGroupIds: [...draft.allowedGroupIds, id] });
    setGroupId("");
    setGroupError(null);
  }

  async function save() {
    if (!draft || blocked || emptyScope || saveError === "conflict") return;
    setSaveError(null);
    try {
      await mutation.mutateAsync(draft);
      toast.success(t(($) => $.wecom.guest_access.saved));
      onClose();
    } catch (error) {
      setSaveError(error instanceof ApiError && error.status === 409 ? "conflict" : "save");
    }
  }

  function sourceLabel() {
    switch (query.data?.source) {
      case "environment": return t(($) => $.wecom.guest_access.source_environment);
      case "database": return t(($) => $.wecom.guest_access.source_database);
      case "none": return t(($) => $.wecom.guest_access.source_none);
      default: return null;
    }
  }

  const updatedAt = query.data?.updatedAt;
  const updatedDate = updatedAt ? new Date(updatedAt) : null;

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-xl" showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.wecom.guest_access.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.wecom.guest_access.description)}</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 space-y-5 overflow-y-auto pr-1">
          {query.isError && (
            <div className="space-y-2">
              <p role="alert" className="text-body text-destructive">{t(($) => $.wecom.guest_access.load_failed)}</p>
              <Button variant="outline" size="sm" onClick={draft ? () => void query.refetch() : reload} disabled={query.isFetching}>
                {t(($) => $.wecom.guest_access.retry)}
              </Button>
            </div>
          )}
          {!draft && !query.isError && <p role="status" className="text-body text-muted-foreground">{t(($) => $.wecom.loading)}</p>}
          {draft && (
            <>
              <div className="flex items-center justify-between gap-4">
                <Label htmlFor={`${prefix}-enabled`}>{t(($) => $.wecom.guest_access.enabled)}</Label>
                <Switch id={`${prefix}-enabled`} checked={draft.enabled} disabled={blocked}
                  onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
              </div>
              <p className="text-caption text-muted-foreground">{sourceLabel()}</p>
              <fieldset disabled={blocked} className="min-w-0 space-y-3">
                <legend className="mb-1 text-body font-medium">{t(($) => $.wecom.guest_access.groups)}</legend>
                <p className="text-caption text-muted-foreground">{t(($) => $.wecom.guest_access.groups_hint)}</p>
                <div className="max-h-56 space-y-3 overflow-y-auto p-1">
                  {groupIds.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.wecom.guest_access.empty_groups)}</p>}
                  {groupIds.map((id) => (
                    <div key={id} className="flex items-start gap-3">
                      <Checkbox id={`${prefix}-group-${encodeURIComponent(id)}`} checked={draft.allowedGroupIds.includes(id)} disabled={blocked}
                        aria-label={t(($) => $.wecom.guest_access.group_toggle, { group: id })}
                        onCheckedChange={(checked) => {
                          if (checked) {
                            const error = validateWecomGuestGroup(id, draft.allowedGroupIds);
                            if (error) { setGroupError(t(($) => $.wecom.guest_access.validation_group_limit)); return; }
                          }
                          setDraft({ ...draft, allowedGroupIds: checked ? [...draft.allowedGroupIds, id] : draft.allowedGroupIds.filter((group) => group !== id) });
                          setGroupError(null);
                        }} />
                      <Label htmlFor={`${prefix}-group-${encodeURIComponent(id)}`} className="min-w-0 flex-1 flex-col items-start gap-0.5">
                        {knownGroups.get(id) && <span className="break-words">{knownGroups.get(id)}</span>}
                        <span className="text-caption font-mono [overflow-wrap:anywhere]">{id}</span>
                      </Label>
                    </div>
                  ))}
                </div>
                <Label htmlFor={`${prefix}-manual`}>{t(($) => $.wecom.guest_access.group_id)}</Label>
                <div className="flex flex-wrap gap-2">
                  <Input id={`${prefix}-manual`} className="min-w-0 flex-1" value={groupId} disabled={blocked}
                    placeholder={t(($) => $.wecom.guest_access.group_placeholder)} aria-invalid={!!groupError}
                    onChange={(event) => { setGroupId(event.target.value); setGroupError(null); }}
                    onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addGroup(); } }} />
                  <Button variant="outline" size="sm" onClick={addGroup} disabled={blocked}>{t(($) => $.wecom.guest_access.add_group)}</Button>
                </div>
                {groupError && <p role="alert" className="text-caption text-destructive">{groupError}</p>}
              </fieldset>
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-4">
                  <Label htmlFor={`${prefix}-direct`}>{t(($) => $.wecom.guest_access.allow_direct_messages)}</Label>
                  <Switch id={`${prefix}-direct`} checked={draft.allowDirectMessages} disabled={blocked}
                    onCheckedChange={(allowDirectMessages) => setDraft({ ...draft, allowDirectMessages })} />
                </div>
                <p className="text-caption text-muted-foreground">{t(($) => $.wecom.guest_access.direct_warning)}</p>
              </div>
              <dl className="space-y-2 text-caption">
                <div><dt className="text-muted-foreground">{t(($) => $.wecom.guest_access.sponsor)}</dt>
                  <dd className="break-words">{getMemberName(query.data?.sponsorUserId ?? "")}</dd>
                  <dd className="font-mono text-micro text-muted-foreground [overflow-wrap:anywhere]">{query.data?.sponsorUserId}</dd></div>
                <div><dt className="text-muted-foreground">{t(($) => $.wecom.guest_access.updated_by)}</dt>
                  <dd className="break-words">{query.data?.updatedBy ? getMemberName(query.data.updatedBy) : t(($) => $.wecom.guest_access.not_saved)}</dd></div>
                <div><dt className="text-muted-foreground">{t(($) => $.wecom.guest_access.updated_at)}</dt>
                  <dd>{updatedDate && !Number.isNaN(updatedDate.getTime()) ? <time dateTime={updatedAt ?? undefined}>{updatedDate.toLocaleString(i18n.resolvedLanguage)}</time> : t(($) => $.wecom.guest_access.not_saved)}</dd></div>
              </dl>
            </>
          )}
          {emptyScope && <p role="alert" className="text-body text-destructive">{t(($) => $.wecom.guest_access.validation_empty_scope)}</p>}
          {saveError && (
            <div className="space-y-2">
              <p role="alert" className="text-body text-destructive">{saveError === "conflict" ? t(($) => $.wecom.guest_access.conflict) : t(($) => $.wecom.guest_access.save_failed)}</p>
              {saveError === "conflict" && <Button variant="outline" size="sm" onClick={reload} disabled={busy || query.isFetching}>{t(($) => $.wecom.guest_access.reload)}</Button>}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={busy}>{t(($) => $.wecom.guest_access.cancel)}</Button>
          {draft && <Button size="sm" onClick={save} disabled={blocked || emptyScope || saveError === "conflict"}>{busy ? t(($) => $.wecom.guest_access.saving) : t(($) => $.wecom.guest_access.save)}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
