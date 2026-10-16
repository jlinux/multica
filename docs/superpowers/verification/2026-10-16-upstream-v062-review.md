# Upstream v0.6.2 integration review

Reviewed the uncommitted merge in `/Users/yangxq/Code/multica-upstream-v062`, baseline `05ce0641d80e30c042d4cda157e587de3440311c`, upstream `7ab92d7622ecd81d179a8d83ae2dea74835a5069`. Read repository instructions and the implementation plan. Examined the supplied custom-vs-upstream patch inventory and live merge integration files, including the subsequent full-identity migration test selection fix. Review was read-only apart from this standalone report; no database/service/production mutation or broad duplicate tests.

## Verdict

**Acceptable from this integration review, with no outstanding Critical or Important findings.** No unresolved merge-induced regression remains in the reviewed integration paths; the locale omissions found by the full frontend run were subsequently corrected and reviewed below. One Important preexisting authorization issue affecting administrator-controlled guest delegation was reproduced and fixed during review; the narrow fix and passing regression evidence were independently inspected. The root agent has now reported completion of the broader verification results; independent review evidence and root-reported results are distinguished below. This is not deployment approval.

## Critical

None identified.

## Important

### I1 — RESOLVED: machine credentials could manage the guest grant that authorizes external access

**Location:** `server/internal/handler/wecom_guest_access.go:14-23`; routes `server/cmd/server/router.go:1852-1853`.

Both guest-policy operations authorize solely from workspace membership/role. A running task's `mat_` credential is translated into the runtime owner's user identity by `server/internal/handler/daemon.go:2052-2055` and `server/internal/middleware/auth.go:122-135`. `RequireWorkspaceRoleFromURL` checks that user's role, not whether this is a human request (`server/internal/middleware/workspace.go:244-260`). There is no global human-only gate. Therefore a guest-prompted agent on an owner/admin-owned runtime can GET the policy/version and PUT an expanded group/DM grant. This includes an ordinary-member agent sponsor using an admin-owned shared runtime: the task token's identity is the runtime owner, not necessarily the sponsor.

**Why it matters:** Explicit administrator authorization must remain outside the external caller's control. Grant revision/fingerprint checks do not prevent changing the policy through the authorized management endpoint itself. The documentation does disclaim general runtime API sandboxing, so this is preexisting hardening rather than a newly introduced merge regression; the task owner confirmed that machine callers must not expand their own grant.

**Concrete fix:** Reject machine credential actors at the shared guest-access handler scope (using the existing authoritative `isMachineCredentialActor`/human-actor convention). Cover both GET and PUT with task-token and cloud-PAT regression cases, including an effective admin user; retain ordinary-member sponsorship and successful human owner/admin configuration. Keep the change limited to guest grant management.

**Resolution verified:** `server/internal/handler/wecom_guest_access.go:15-20` now rejects `isMachineCredentialActor` before parsing/loading policy scope. Both GET and PUT call that shared scope. The test `TestWecomGuestAccessRequiresAuthenticatedHumanAdmin` exercises real Auth with a DB-backed task token, a Fleet-verified cloud-node PAT, and a successful human PAT control; it also checks that Auth replaces a forged actor-source header. Documentation explicitly states the narrow human-only grant-management rule.

Independently read `/tmp/multica-v062-wecom-human-guard-red.log`: all four machine GET/PUT cases returned 200 before the fix, and PUT demonstrably widened groups and enabled DMs; human controls passed. Independently read `/tmp/multica-v062-wecom-human-guard-green.log`: all six method/credential cases passed after the fix, alongside the existing guest policy and claim-classification suites (10 top-level tests, handler package PASS, 0.904s). This closes I1 without requiring the sponsor to become an admin or changing other runtime API permissions.

## Minor

None identified with sufficient evidence to require a change.

## Reviewed integration properties

- Guest routing retains the upstream invocation permission gate and evaluates the resolved sponsor. The sponsor must own the active agent and remain a workspace member; sponsorship does not require promotion to admin.
- Guest command denial, durable guest metadata, per-sender group keys, grant generations, revoked/reinstalled installation tombstones, and database-over-environment policy precedence remain wired.
- The new upstream task delivery snapshot carries binding configuration. Shared `taskAddress` recovers the transport chat ID from that snapshot, and retry paths copy delivery rows. Guest enqueue/claim/retry checks retain attribution and reject nonempty personal connected-app overlays.
- Goals web/desktop routes, capability flag initialization/schema fallback, workspace-scoped Query keys, reconnect/event invalidation, and explicit workspace deletion cleanup remain integrated.
- Migration aliases use complete identities, immutable checksums and relation validity rather than numeric prefix skipping. The test helper now selects those same full identities, avoiding collision with 18 upstream migration prefixes.
- Read-only scan found 114 new up migrations relative to the baseline: every migration-created index is a standalone single-statement CONCURRENTLY build, and no new REFERENCES, FOREIGN KEY or CASCADE statements occur in those files. `git diff --check` completed without output during review.

## Evidence and CannotVerify

Authoritative rehearsal artifacts are under `/tmp/multica-v062-rehearsal/final/`. The parent directory contains stale exploratory results, including an earlier historical-451 difference, and must not be used as final evidence.

Inspected final `upgrade-result.json`, `backup-restore-result.json`, `fresh-result-multica_upgrade_formal_fresh.json`, and the rehearsal harness. The final artifacts report 502 to 616 ledger entries, preservation of 23 synthetic fixture rows and old ledger timestamps, no historical migration SQL differences, repeat-upgrade idempotence, a separate synthetic fixture backup restore preserving 502 ledger rows, and a fresh 616-migration installation with 23 fixture rows and an idempotent second run. Final `schema-convergence.json` reports identical fresh/upgraded schemas and zero unusable indexes in both. Independently recomputed all 22 files in `artifact-sha256.json`; no digest mismatches. Candidate migration tree SHA-256: `420bfdcaac77cf5b4a39574e797178f7585f962df78be462dcb4310c4b85fadc`.

The harness targets the fixed dedicated container and loopback port, validates the database prefix, refuses overwriting its evidence/database, and never executes down/drop. Fixture backup restore is correctly distinct from restoring production data.

**Independent-review boundary:** Full Go/frontend/build checks were coordinated by the root agent and were not rerun by this reviewer; their final root-reported completion is recorded below. No live WeCom transport, production schema/data, production restore, or deployed-runtime verification was performed. Synthetic preservation demonstrates the covered fixture and ledger cases, not all possible deployed data distributions. I1 is now resolved based on the inspected source and red/green evidence above.


## Final locale addendum

**Verdict unchanged: acceptable, with no outstanding Critical or Important findings.** Reviewed all seven locale additions/changes after the full frontend run exposed the fork/upstream translation integration omissions: French Goals, French navigation/tab labels, French/Japanese/Korean guest-access settings, bundle registration, and Goals coverage tests.

The new French namespace contains 170 genuine French translations, retaining the task/run distinction (`tâches` / `exécutions`). The guest-access copy in French, Japanese and Korean preserves the permission-sensitive meanings: sender-isolated conversations with group-visible replies, unrestricted external DM scope when enabled, ownership-change invalidation, immediate policy application, preserved edits, and conflict reload discarding edits. Japanese/Korean have only the applicable `_other` plural form; French has both forms.

Independently compared against upstream `7ab92d7622ecd81d179a8d83ae2dea74835a5069`: all 51 existing French layout values, 1,197 French settings values, and 1,187 existing settings values in each of Japanese and Korean remain unchanged. Additions are exactly 2 French layout keys and 40/39/39 guest-access strings. Independently checked exact English/French Goals key and interpolation parity for all 170 values, plus normalized plural-key and interpolation parity for all 118 guest-access translations.

`packages/views/locales/goals-coverage.test.ts` now derives locales from the shipped `RESOURCES` bundle (all five shipped locales), rather than omitting newly added languages. The seven French cognate exceptions are narrowly tied to individual keys (`Agent`, two `Cycle` labels, `Description`, `Type`, `Direction`, `Panorama`) and are normal French labels. They do not exempt arbitrary matching text or the French namespace; copied English prose still fails. The existing bidirectional parity and dead-plural-key suite remains intact.

Independently inspected `/tmp/multica-v062-locale-red.log` and `/tmp/multica-v062-locale-green.log`: the two focused suites move from 8 failed / 221 passed to **229 / 229 passed**. No broad duplicate test run was performed. Full Go race testing was ongoing at the time of the locale addendum; its later root-reported completion is recorded below.


## Migration lint compatibility addendum

**Verdict unchanged: acceptable, with no outstanding Critical or Important findings.** Reviewed `server/internal/migrations/migrations_lint_test.go` and the accompanying AGENTS.md migration rule. The upstream uniqueness lint rejects the 18 historical Goals/upstream prefix collisions even though the production migrator identifies migrations by full stem. The fix exempts only 18 explicit pairs of complete stems; it does not skip a numeric range, change production migration execution, alter historical SQL, or rename ledger identities.

Independently verified that every exception's Goals stem exists at the deployed fork baseline and in the existing alias map, its paired stem exists in the upstream release, both files exist in the candidate, and both share the intended prefix. The comparison is symmetric, so either lexical order works. `stemByNumber` retains the first encountered stem: any third distinct migration cannot match the only allowed pair and remains an error. A read-only model check covered both permitted pair orders, all **108** third-collision permutations across the 18 pairs, and an unrelated new duplicate prefix; all unexpected collisions were rejected.

Inspected the original full-race failure in `/tmp/multica-v062-go-race-final.log`, focused green package result in `/tmp/multica-v062-migrations-lint-green.log` (**PASS, 2.072s**), and actual Go-lint negative probe in `/tmp/multica-v062-migration-collision-negative.log`: temporary `456_zz_upgrade_rehearsal_collision` was rejected. Confirmed the temporary migration is absent and `git diff --check` remains clean. The new contributor rule requires preserving deployed names/SQL and assigning future migration numbers above the combined maximum, consistent with the plan.

The parent subsequently completed its aggregate race verification; that aggregate accounting remains root-owned, while the final agent package evidence was independently inspected below.


## Agent fixture isolation addendum (source and final execution evidence reviewed)

Reviewed the narrow `server/pkg/agent/codex_test.go` patch against upstream and the accompanying AGENTS.md test-isolation rule. **The test-only change is acceptable; no new Critical or Important issue found in the patch.** `isolateFakeCodexConfig` clones caller Env, preserves explicitly provided fixture homes, creates a temporary home with an existing empty `sessions/`, and passes it through Config.Env without changing the parent environment. The production usage-fallback source confirms that this isolates fake thread IDs from ambient developer Codex session scans. Existing missing-home MCP configuration tests remain deliberately outside the helper.

The two cancellation fixtures now fork their blocking child before publishing readiness, then wait for it. This retains the unresponsive process-group test scenario and original deadlines while preventing the test's own ready signal from racing child creation. No production agent code, timeout, assertion, or user profile data was changed. The broader production SIGKILL/fork race described in the investigation remains outside this fix; this change must not be represented as a production cancellation fix.

Read `/tmp/multica-v062-agent-tests-investigation.md` and inspected the minimal live patch. Serialized final package execution is now verified from the final log below; this pending item is closed. No heavy or duplicate tests were started by this reviewer.

Also inspected `/tmp/multica-v062-wecom-human-guard-final.log`: the refined public/admin-owned runtime fixture passes the real task-token/cloud-PAT denial and human-PAT success cases (handler package PASS, 1.009s), reinforcing the resolved I1 evidence.


## Final execution evidence and acceptance

Independently inspected `/tmp/multica-v062-agent-final-serialized.log`: it ends with **PASS, pkg/agent 175.437s**, with no FAIL entries or `WARNING: DATA RACE`. The unchanged timing/concurrency regression assertions pass, including concurrent launch (0.70s), cancellation during initialize (0.26s), cancellation before turn start (0.27s), near-deadline interrupt (0.41s), legacy first-turn progress (0.60s), and continuous-delta progress (0.67s). The fixture-only addendum is fully accepted. Final `git diff --check` is clean.

The root agent separately reports successful final frontend verification (9,398 tests across 765 files), 10 typecheck tasks, 7 lint tasks, production web build, native Go server build, and successful accounting for all 84 regular Go race-tested packages. It also reports a production-build Playwright smoke covering login, three Goals, task detail, inbox summary without full inbox fetch, and upload/download (4.1s), with screenshots inspected. These aggregate/browser results are root-reported rather than independently rerun here.

**Final review verdict: acceptable for the authorized local integration/merge; no outstanding Critical or Important findings.** All issues raised in this review are resolved or, for the preexisting general process-group kill/fork race, explicitly identified as outside the test-only change. Production deployment, real WeCom transport, production data validation and production restore remain unperformed and are not implied by this acceptance.
