package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Goals first shipped as 451–468 on feat/goals. Merging upstream's five
// migrations moved the identical SQL to 456–473. Only these verified aliases
// are recognized; numeric ranges and same-named relations are not sufficient.
// Digests were compared with the deployed image on 2026-10-05. Keep these
// historical SQL files immutable; subsequent changes need new migrations.
var goalMigrationAliases = map[string]struct {
	legacy   string
	sha256   string
	relation string
	table    bool
}{
	"456_goal":                                  {"451_goal", "fdce05d4b0ee1724bd725340e0498a71abe209b5f50489b64fdd1e0492f18c77", "goal", true},
	"457_goal_issue":                            {"452_goal_issue", "2ec7bed347b9e1b7697eae5747b5f3e1f3544776205c1dc9d5b9e02ffc08cfd0", "goal_issue", true},
	"458_milestone":                             {"453_milestone", "1fd761e20fe049adbc0f588ced47a8fd600eaf4befcefa18f2004facf4d1a83b", "milestone", true},
	"459_milestone_date_change":                 {"454_milestone_date_change", "d1f256ccbce2e11584fd75aebf0807083d7570cf47260f59c05fd8519f18448f", "milestone_date_change", true},
	"460_milestone_release":                     {"455_milestone_release", "7262f10dee7e22a9097e74184da3059883b6fcdf5e8d16663ebcc29149d5da81", "milestone_release", true},
	"461_milestone_proposal":                    {"456_milestone_proposal", "c290bcf0137553b1dbe0e690a5936dd09fe7659a62a3cdc2b55fbe15214ebb9d", "milestone_proposal", true},
	"462_goal_workspace_level_index":            {"457_goal_workspace_level_index", "0d36fac4916bed6b3cc9b96a9b37e775e4e677a44d3f6d8887e48227051f929b", "idx_goal_workspace_level", false},
	"463_goal_parent_index":                     {"458_goal_parent_index", "31bef3997e291ebe2112623377e642cf9710afdc1a3845fe690984ecc4f86e08", "idx_goal_parent", false},
	"464_goal_project_index":                    {"459_goal_project_index", "c5558df93275983881894630e7cd47dedd935b3bfe674a91c73acbfd78f663b3", "idx_goal_project", false},
	"465_goal_prev_index":                       {"460_goal_prev_index", "3f20d3022594b5c6fe3049faeb75477b797769376b1653418066e0a5b4a5efd0", "idx_goal_prev", false},
	"466_goal_issue_issue_index":                {"461_goal_issue_issue_index", "b8bee8dd9bd58989a2ab830f1d186325c03d67db5a422555d27baec9d0689cd1", "idx_goal_issue_issue", false},
	"467_milestone_goal_index":                  {"462_milestone_goal_index", "5e51a21277f6b4f0d24fa7a3bd89832a0fe3f0fd2091a1743f6a96a5e9edbc42", "idx_milestone_goal", false},
	"468_milestone_workspace_planned_index":     {"463_milestone_workspace_planned_index", "c4f911396c1c97dd5dd4d91d48d2f7e3ab518ae565905a6cdda7cc40c39f7288", "idx_milestone_workspace_planned", false},
	"469_milestone_overdue_index":               {"464_milestone_overdue_index", "a61da684195481674da8e4a96b5fc54a86dbde72f192444acdd59eb204a931b6", "idx_milestone_open_planned", false},
	"470_milestone_date_change_milestone_index": {"465_milestone_date_change_milestone_index", "569116f7d2ea5c7e031bc784f03f4cf0ded1c59912de297ee933cc9200d280a1", "idx_milestone_date_change_milestone", false},
	"471_milestone_release_milestone_index":     {"466_milestone_release_milestone_index", "9a90cbd0763250c24d354f9df6056aafe7f21beaec9d3cebd0c0b2e7f9d370df", "idx_milestone_release_milestone", false},
	"472_milestone_proposal_pending_index":      {"467_milestone_proposal_pending_index", "773c1c752f736328e062bf5fc3ab042e9d3da48b41fe5642c725d998780b22b3", "idx_milestone_proposal_pending", false},
	"473_milestone_proposal_milestone_index":    {"468_milestone_proposal_milestone_index", "64c97a7c56856bda1c803d53a722d7e7eebc3c7ac30db19baf9d3718524f7896", "idx_milestone_proposal_milestone", false},
}

// adoptLegacyGoalMigration runs under the runner's pinned advisory lock.
// Preserve the old record for old-image readiness and copy its timestamp.
// Never claim that an unrecorded legacy migration or a missing object ran.
func adoptLegacyGoalMigration(ctx context.Context, conn *pgxpool.Conn, table, version, file string) (bool, error) {
	alias, ok := goalMigrationAliases[version]
	if !ok {
		return false, nil
	}
	var exists bool
	if err := conn.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version=$1)", table), alias.legacy).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	sql, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(sql)) != alias.sha256 {
		return false, fmt.Errorf("legacy Goals migration %s: SQL checksum differs from deployed %s", version, alias.legacy)
	}
	// Concurrent indexes must be valid and ready, not merely present. Resolve
	// only in the current schema, avoiding an accidental match in public.
	var usable bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (
  SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  LEFT JOIN pg_index i ON i.indexrelid=c.oid
  WHERE n.nspname=current_schema() AND c.relname=$1
   AND ((c.relkind='r' AND $2) OR (c.relkind='i' AND NOT $2 AND i.indisvalid AND i.indisready))
 )`, alias.relation, alias.table).Scan(&usable)
	if err != nil {
		return false, err
	}
	if !usable {
		return false, fmt.Errorf("legacy Goals migration %s: expected usable relation %s is missing or invalid; repair schema before retrying", alias.legacy, alias.relation)
	}
	_, err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO %s(version, applied_at) SELECT $1, applied_at FROM %s WHERE version=$2", table, table), version, alias.legacy)
	if err != nil {
		return false, err
	}
	fmt.Printf("  alias %s (already applied as %s)\n", version, alias.legacy)
	return true, nil
}
