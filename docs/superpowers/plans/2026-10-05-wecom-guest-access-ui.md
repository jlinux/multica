# WeCom Guest Access Configuration Implementation Plan

> For agentic workers: use superpowers:subagent-driven-development to implement this plan.

**Goal:** Let workspace administrators configure existing WeCom guest access safely from Settings.
**Architecture:** Persist policy under channel_installation.config; one database-aware authorization path used by ingress and task validation. Shared core API and views dialog. No migration or deployment in this task.
**Tech Stack:** Go, PostgreSQL/sqlc, React Query, Zod, React/Base UI, Vitest.

## Global Constraints

Follow the approved spec at docs/superpowers/specs/2026-10-05-wecom-guest-access-ui-design.md and CLAUDE.md. Continue on existing feat/wecom-guest-access branch. No production writes, real bot messages, secrets in output, or unrelated changes. Ordinary member agent owners remain valid execution sponsors. Explicit database disable and malformed database policy must never fall back to environment grants. Preserve existing grant fingerprint on equivalent first save. Keep credentials intact and isolate cross-workspace reads/writes. No wildcard group grants. Fresh installations explicitly start disabled; existing legacy active same-bot rows retain ENV fallback until policy save. New owner does not silently inherit old authorization. Existing running tasks are not automatically stopped.

## Shared HTTP Contract

Admin GET/PUT /api/workspaces/{id}/wecom/installations/{installationId}/guest-access.
PUT body: { enabled: boolean, allowed_group_ids: string[], allow_direct_messages: boolean, version: string }.
Response: { enabled: boolean, allowed_group_ids: string[], allow_direct_messages: boolean, version: string, source: "database"|"environment"|"none", sponsor_user_id: string, updated_by: string|null, updated_at: string|null, groups: {chat_id: string, name: string|null}[] }.
Version is an opaque concurrency token, including environment-only state. Never accept arbitrary sponsor/bot/workspace fields. HTTP 409 for stale version, 400 validation, 403 role, 404 scope mismatch, 5xx load failure; failed loads must not become editable empty defaults.
List installation adds optional guest_access: {status: "enabled"|"disabled"|"unavailable", allowed_group_count: number, allow_direct_messages: boolean}. Details remain admin-only.

## Task 1 — Backend policy, lifecycle and endpoints

Files: server/internal/channelaccess/wecom*.go (central effective policy), server/internal/integrations/wecom/{types,installation,guest_access,wecom_resolvers}.go, server/internal/service/wecom_guest.go, server/internal/handler/wecom_guest_access.go and wecom_web.go, server/cmd/server/router.go, server/pkg/db/queries/channel*.sql and generated queries, adjacent tests.
- Write failing tests for persisted disable overriding ENV, invalid persisted policy, equivalent grant snapshot, stale save and workspace/role boundaries.
- Resolve grants from current installation JSON; validate snapshots against the resolved grant everywhere. Preserve actor membership checks and existing group/sender task isolation.
- Add partial JSON update with transaction/locking and version guard, bound validation (100 groups, each <=256 bytes, no blanks, duplicates or wildcard). Derive sponsor from active agent; preserve legacy order when same set. Preserve policy on same bot rotation, explicitly disable on revoke or bot switch so reinstall cannot revive legacy ENV grants.
- Add admin handlers, router guards and candidate groups from real config.chat_id for synthetic bindings, excluding private bindings. List summaries only, no secrets.
- Verify focused Go tests then actual PostgreSQL/Redis integration tests. Test endpoints and lifecycle/concurrency. Commit server-only changes.

## Task 2 — Shared API and configuration dialog

Files: packages/core/{types,api,wecom}/..., packages/views/settings/components/wecom-tab.tsx and new wecom-guest-access-dialog.tsx, locale en/zh files, adjacent tests.
- Write failing schema/API/UI tests for malformed responses, error state, role gate, group and DM save flows.
- Add typed, schema-validated API, query and mutation hooks; strict handling for malformed configuration loads, invalidate list/detail after save. Do not render cached error data as writable form.
- Add admin-only action and status/count for all members in bot list. Dialog has enabled switch, known/manual groups, private switch with consequence text, current owner/updater/time. Explain @, per-sender separation and group-visible replies. Loading/error/conflict retains edits; no successful-save toast on failed schema.
- Supply Chinese and English copy and use shared UI primitives/tokens. Group IDs support long wrapping and scrollable dialog.
- Run focused Vitest, core/views typecheck and lint. Commit frontend-only changes.

## Task 3 — Review and local acceptance

- Review task implementations for spec and code quality; fix important findings and rerun covering tests.
- Run Go race tests on channelaccess, WeCom integration, guest service/handler; run frontend focused suites, typechecks and formatting/lint.
- Exercise actual local browser layout and configuration interactions against local fixture API or local test backend. No production modification or actual model invocation.
- Record evidence and material limitations. Final branch review, clean git diff checks. Report local result, leave deployment for the subsequent backed-up release.

## Progress

- [x] Backend
- [x] Frontend
- [x] Review and local acceptance
