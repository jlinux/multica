# WeCom external access and sender isolation implementation plan

> Execute with superpowers:subagent-driven-development and test-driven-development. Scope supersedes the larger customer-service draft: no new ticket engine or UI in this change.

Goal: permit explicitly authorized WeCom visitors without creating Multica users, and isolate group conversations by sender.

Subsequent approved adjustment (2026-10-05): the server operator configures the grant; its sponsor is the agent owner and an existing workspace member, not necessarily an owner/admin. This supersedes the sponsor-role requirement below. Deployment and enabling the existing groups were subsequently authorized. See `docs/wecom-guest-access.md` for the current contract.

Architecture: reuse installation, binding config, chat/task queue and outbound transport. Operator-managed MULTICA_WECOM_GUEST_ACCESS grants bind exact bot/workspace/agent/sponsor and allowed chats. Grants default off; runtime capabilities remain administrator-managed. Guest sessions retain a separate sender and grant snapshot; task provenance identifies delegated channel access rather than a human-authored request. Revalidate before task launch and reject revoked grants. Never silently substitute an installer as a guest.

Global constraints: preserve members-only defaults; do not modify production or merge upstream; no database migration; no arbitrary guest /issue command; /new and /clear retain per-sender boundaries; all outbound text/files/relay use the real chat ID. Grant sponsor must be the agent owner and a workspace owner/admin. Guest runs do not inherit personal connected-app overlays. Default runtime tools are NOT sandboxed by this feature; enable only after administrator configures the selected agent/runtime appropriately.

## Tasks

- [x] Sender isolation: add collision-safe group binding key and real chat_id config; reject missing sender; test two senders, p2p, /new, legacy outbound and files/relay. Files: wecom/session_routing.go, wecom_resolvers.go session methods, outbound.go, resolver/routing tests.
- [x] Grant policy: pure parsing/validation and allowlist tests, then environment-backed policy. Files: internal/channelaccess/wecom.go and tests. Test disabled, malformed, unknown fields, mismatched bot/workspace/agent, wrong sponsor, missing group, p2p switch, revoked fingerprint.
- [x] Inbound guest identity: only unbound valid senders within explicit grant; validate current sponsor membership/admin role and agent ownership. Store source metadata in binding config and keep guest and member sessions distinct. Block direct /issue guest commands with clear response. Files: wecom_resolvers.go identity/session bridge, engine/resolvers.go/router.go, wecom/replier.go, tests.
- [x] Task authorization: load persisted guest binding; apply explicit delegated provenance and empty personal apps; validate snapshot at enqueue and claim including retry. Files: service/wecom_guest.go, service/task.go, handler/daemon.go, tests. Do not modify generic member token behavior.
- [x] Operator docs/config: env example and compose passthrough; explain exact grants, sender session resets, administrator runtime permissions, no production activation, no separate work-item isolation for multiple topics by same sender.
- [x] Verification: Go package tests (including isolated database tests), race tests of changed channel paths, go vet, diff check, independent security/correctness review. Resolve findings before completion.

Expected behavior examples: user A and user B in GROUP create distinct bindings while replies both target GROUP. Unbound user gets normal chat only for a valid configured grant; absent policy preserves needs_binding. Invalid/revoked grants never launch a guest task. Direct /issue from visitor does not create an issue outside the managed agent flow.

## Verification outcome

- `go test -race` passed for channelaccess, channel/engine, wecom, service and handler packages (database tests separately enabled below).
- Dedicated local PostgreSQL database, fully migrated: real resolver/router with two concurrent unregistered senders, independent /new and /clear; real ordinary and prepared guest enqueue; automatic retry; direct-chat rejection; grant, installation and administrator revocation; temporary database failure requeue versus authorization denial. All passed.
- `go vet` passed for all five changed packages. Compose configuration validation and `git diff --check` passed.
- Independent review identified and resolved retry initiator inheritance, temporary database error classification, and the direct-chat send bypass. No remaining blocking findings.
- Production was not changed. Enabling requires an explicit administrator grant and a deployment compatibility check for the existing goals migration ledger. Existing runtime permissions remain administrator-managed.
