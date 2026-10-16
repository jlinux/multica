package migrations

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestMigrationNumericPrefixesAreUnique(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")

	// Migrations through 128 contain historical duplicate numeric prefixes.
	// From 129 onward, keep new numeric prefixes unique. These exact pairs
	// predate this fork's v0.6.2 merge: renumbering deployed Goals migrations
	// would change their ledger identities and risk replaying historical DDL.
	// The runner identifies migrations by their complete stem. No other
	// collision, including a third migration at these numbers, is allowed.
	historicalGoalCollisions := map[string]string{
		"456_goal":                                  "456_cancel_comment_assignee_fallbacks",
		"457_goal_issue":                            "457_task_message_output_truncated",
		"458_milestone":                             "458_agent_task_cancellation_actor",
		"459_milestone_date_change":                 "459_chat_message_assistant_task_index",
		"460_milestone_release":                     "460_agent_task_queue_autopilot_run_created_at_index",
		"461_milestone_proposal":                    "461_channel_trigger_snapshot",
		"462_goal_workspace_level_index":            "462_delete_reference_only_pr_links",
		"463_goal_parent_index":                     "463_drop_issue_description_bigm_index",
		"464_goal_project_index":                    "464_drop_issue_description_trgm_index",
		"465_goal_prev_index":                       "465_agent_task_queue_chat_with_session_index",
		"466_goal_issue_issue_index":                "466_activity_log_member_assignee_frequency_index",
		"467_milestone_goal_index":                  "467_autopilot_trigger_creator_from_autopilot",
		"468_milestone_workspace_planned_index":     "468_drop_reference_only_column",
		"469_milestone_overdue_index":               "469_issue_status_lifecycle_categories",
		"470_milestone_date_change_milestone_index": "470_issue_status_icon",
		"471_milestone_release_milestone_index":     "471_comment_deleted_at",
		"472_milestone_proposal_pending_index":      "472_agent_task_queue_chat_session_index",
		"473_milestone_proposal_milestone_index":    "473_drop_agent_task_queue_chat_with_session_index",
	}
	const firstUniqueMigrationNumber = 129
	stemByNumber := make(map[int]string)
	for _, file := range files {
		stem, _, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			continue
		}
		prefix, _, ok := strings.Cut(stem, "_")
		if !ok {
			continue
		}
		number, err := strconv.Atoi(prefix)
		if err != nil || number < firstUniqueMigrationNumber {
			continue
		}
		if previous, exists := stemByNumber[number]; exists {
			if historicalGoalCollisions[stem] == previous || historicalGoalCollisions[previous] == stem {
				continue
			}
			t.Errorf("migrations %s and %s share numeric prefix %s", previous, stem, prefix)
			continue
		}
		stemByNumber[number] = stem
	}
}

func TestMigrationFilesHaveMatchingDirections(t *testing.T) {
	files := migrationFilesForLint(t, "*.sql")

	directionsByStem := make(map[string]map[string]bool)
	for _, file := range files {
		stem, direction, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			continue
		}
		if directionsByStem[stem] == nil {
			directionsByStem[stem] = make(map[string]bool)
		}
		directionsByStem[stem][direction] = true
	}

	for stem, directions := range directionsByStem {
		if !directions["up"] || !directions["down"] {
			t.Errorf("migration %s must have both .up.sql and .down.sql files", stem)
		}
	}
}

func migrationFilesForLint(t *testing.T, pattern string) []string {
	t.Helper()

	dir := realMigrationsDir(t)
	files, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no migration files matched %s in %s", pattern, dir)
	}
	sort.Strings(files)
	return files
}

func realMigrationsDir(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration lint test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "migrations"))
}

func splitMigrationFilename(name string) (stem, direction string, ok bool) {
	for _, candidateDirection := range []string{"up", "down"} {
		suffix := fmt.Sprintf(".%s.sql", candidateDirection)
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), candidateDirection, true
		}
	}
	return "", "", false
}
