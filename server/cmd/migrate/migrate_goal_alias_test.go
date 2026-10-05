package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/migrations"
)

// These are the real SQL files shipped under numbers 451–468 before the
// upstream merge moved Goals to 456–473. Their contents did not change.
func goalMigrationFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, f := range files {
		n, _ := strconv.Atoi(filepath.Base(f)[:3])
		if n >= 456 && n <= 473 {
			result = append(result, f)
		}
	}
	if len(result) != 18 {
		t.Fatalf("got %d goal migrations", len(result))
	}
	return result
}

func legacyGoalVersion(file string) string {
	v := migrations.ExtractVersion(file)
	n, _ := strconv.Atoi(v[:3])
	return fmt.Sprintf("%03d%s", n-5, v[3:])
}

func TestGoalMigrationUpgrade(t *testing.T) {
	for _, legacyCount := range []int{0, 6, 18} {
		t.Run(fmt.Sprintf("legacy_%d", legacyCount), func(t *testing.T) {
			ctx := context.Background()
			admin := openTestPool(t)
			schema := createScratchSchema(t, ctx, admin, "goal_upgrade_")
			pool := openTestPoolWithSearchPath(t, schema)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
    CREATE TABLE comment (id uuid PRIMARY KEY, parent_id uuid, issue_id uuid);
    CREATE TABLE agent_task_queue (id uuid PRIMARY KEY, issue_id uuid, agent_id uuid, trigger_comment_id uuid, status text, context jsonb)`)
			goals := goalMigrationFiles(t)
			for _, file := range goals[:legacyCount] {
				sql, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				exec(string(sql))
				exec(`INSERT INTO schema_migrations(version, applied_at) VALUES ($1, '2026-09-08 UTC')`, legacyGoalVersion(file))
			}
			if legacyCount > 0 {
				exec(`INSERT INTO goal(workspace_id,level,title,created_by_type,created_by_id) VALUES (gen_random_uuid(),1,'preserve existing goal','member',gen_random_uuid())`)
			}
			versions := []string{"451_agent_task_comment_thread", "452_agent_task_pending_thread_unique", "453_drop_pending_issue_agent_unique", "454_drop_comment_content_bigm_index", "455_drop_comment_content_trgm_index"}
			files := append(realMigrationFiles(t, versions, "up"), goals...)
			opts := runOptions{Direction: "up", Files: files, SchemaMigrationsTable: schema + ".schema_migrations"}
			for i := 0; i < 2; i++ {
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatalf("upgrade run %d: %v", i, err)
				}
			}
			for _, file := range files {
				assertMigrationVersionRecorded(t, ctx, pool, schema, migrations.ExtractVersion(file), true)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM goal WHERE title='preserve existing goal'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if legacyCount > 0 && count != 1 {
				t.Fatalf("existing rows: %d", count)
			}
			for _, file := range goals[:legacyCount] {
				assertMigrationVersionRecorded(t, ctx, pool, schema, legacyGoalVersion(file), true)
				var same bool
				if err := pool.QueryRow(ctx, `SELECT a.applied_at=b.applied_at FROM schema_migrations a, schema_migrations b WHERE a.version=$1 AND b.version=$2`, legacyGoalVersion(file), migrations.ExtractVersion(file)).Scan(&same); err != nil || !same {
					t.Fatalf("lost applied timestamp: %v", err)
				}
			}
			// Roll back Goals only, then reapply: stale legacy records must not cause
			// a future upgrade to skip the now-missing tables.
			down := make([]string, len(goals))
			for i, f := range goals {
				down[i] = strings.Replace(f, ".up.sql", ".down.sql", 1)
			}
			slices.Reverse(down)
			opts.Direction = "down"
			opts.Files = down
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			for _, f := range goals {
				assertMigrationVersionRecorded(t, ctx, pool, schema, legacyGoalVersion(f), false)
			}
			opts.Direction = "up"
			opts.Files = goals
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatalf("reapply: %v", err)
			}
		})
	}
}

func TestGoalMigrationAliasRefusesUnverifiedState(t *testing.T) {
	for _, scenario := range []string{"checksum", "missing_table", "missing_index", "unrecorded_table", "invalid_index"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			admin := openTestPool(t)
			schema := createScratchSchema(t, ctx, admin, "goal_alias_invalid_")
			pool := openTestPoolWithSearchPath(t, schema)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE TABLE schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
			file := goalMigrationFiles(t)[0]
			sql, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "missing_table" {
				exec(string(sql))
			}
			if scenario == "missing_index" || scenario == "invalid_index" {
				file = goalMigrationFiles(t)[6]
			}
			version := migrations.ExtractVersion(file)
			if scenario != "unrecorded_table" {
				exec(`INSERT INTO schema_migrations(version) VALUES ($1)`, legacyGoalVersion(file))
			}
			want := "missing or invalid"
			if scenario == "checksum" {
				file = filepath.Join(t.TempDir(), filepath.Base(file))
				if err := os.WriteFile(file, append(sql, []byte("\n-- changed SQL\n")...), 0600); err != nil {
					t.Fatal(err)
				}
				want = "checksum differs"
			}
			if scenario == "unrecorded_table" {
				want = "already exists"
			}
			if scenario == "invalid_index" {
				exec(`INSERT INTO goal(workspace_id,level,title,created_by_type,created_by_id) SELECT gen_random_uuid(),1,'duplicate level','member',gen_random_uuid() FROM generate_series(1,2)`)
				if _, err := pool.Exec(ctx, `CREATE UNIQUE INDEX CONCURRENTLY idx_goal_workspace_level ON goal(level)`); err == nil {
					t.Fatal("expected invalid index fixture")
				}
			}
			err = runMigrations(ctx, pool, runOptions{Direction: "up", Files: []string{file}, SchemaMigrationsTable: schema + ".schema_migrations"})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
			assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
		})
	}
}

func TestGoalMigrationLegacyOnlyRollback(t *testing.T) {
	ctx := context.Background()
	admin := openTestPool(t)
	schema := createScratchSchema(t, ctx, admin, "goal_alias_down_")
	pool := openTestPoolWithSearchPath(t, schema)
	file := goalMigrationFiles(t)[0]
	sql, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now()); INSERT INTO schema_migrations(version) VALUES ('451_goal')`); err != nil {
		t.Fatal(err)
	}
	down := strings.Replace(file, ".up.sql", ".down.sql", 1)
	opts := runOptions{Direction: "down", Files: []string{down}, SchemaMigrationsTable: schema + ".schema_migrations"}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	assertMigrationVersionRecorded(t, ctx, pool, schema, "451_goal", false)
	opts.Direction = "up"
	opts.Files = []string{file}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
}
