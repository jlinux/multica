# Goals migration compatibility

The early `feat/goals` image shipped Goals migrations as `451_goal` through
`468_milestone_proposal_milestone_index`. After merging five upstream migrations,
the same 18 SQL files were renumbered to 456–473. Their SHA-256 digests were
compared against the deployed image on 2026-10-05 and were identical.

The migrator recognizes only the explicit mappings in
`server/cmd/migrate/goal_alias.go`. While holding its existing advisory lock,
it checks an exact legacy ledger entry, verifies the current SQL digest, and
checks that the expected table or valid, ready index exists in the current
schema. It then records the current name with the original application timestamp.
It does not execute the aliased SQL again or modify Goals rows. Existing legacy
records remain intact. This is not a general schema drift detector: the ledger
is still the authority for previously executed SQL, as for other migrations.

The upstream migrations 451–455 (task comment-thread queue scope and search-index
retirement) remain pending on the early deployment and execute normally. Fresh
installs execute all current migrations normally. Partially migrated legacy
installs adopt only recorded aliases and execute the rest normally. Restarting
an upgraded installation is idempotent.

## Deployment and recovery

Use the new migrator together with its matching migration files. The normal
container entrypoint runs it before starting the API; no manual ledger edits,
blanket migration skips, or changes to historical SQL are needed. Take the usual
pre-deployment database backup and retain the previous image reference.

If an alias fails its checksum or relation check, startup fails with the specific
legacy migration and reason. Check that the image contains the original SQL and
inspect the named database object. Resolve the mismatch before retrying; do not
mark unexecuted migrations as applied to bypass the check.

Retaining legacy records avoids making the old image believe its Goals migrations
are pending. It does not by itself certify an application rollback: review the
five upstream schema changes as well. Reverting an image does not require running
`migrate down`. That command is destructive and drops application tables. If an
intentional Goals schema rollback is performed, the new migrator removes both
names for each rolled-back migration so a later upgrade cannot falsely skip it.

## Verification

`migrate_goal_alias_test.go` covers fresh, partial legacy, and full legacy
upgrades; preservation of existing Goals data and timestamps; repeated execution;
rollback/reapply; old-name-only rollback; and refusal of altered SQL, missing
objects, invalid indexes, and existing tables without a legacy ledger entry.

On 2026-10-05, a separate local rehearsal used migration files extracted from the
running production image to create an old database, inserted synthetic goal and
milestone rows, and upgraded using the current migrator and migration directory.
All 502 current migration names were recorded, 18 legacy records were retained,
the task-thread column and valid queue index were present, and the synthetic rows
were byte-for-byte unchanged. A second upgrade was a no-op. No production data
was copied or changed. The full migrator and internal migration test packages
also passed with PostgreSQL and the Go race detector.
