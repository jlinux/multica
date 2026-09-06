package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// newGoal creates a goal through the handler and returns its id. Tests that
// need a goal to act on say so in one line; the cases below are then only
// about the rule each of them names.
func newGoal(t *testing.T, body map[string]any) GoalResponse {
	t.Helper()
	var g GoalResponse
	testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", body)).
		Want(http.StatusCreated).JSON(&g)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM goal WHERE id = $1`, g.ID)
	})
	return g
}

func directionGoal(t *testing.T, title string) GoalResponse {
	return newGoal(t, map[string]any{"level": 1, "title": title})
}

func productGoal(t *testing.T, title, parentID string) GoalResponse {
	projectID := dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"title":        title + " project",
	})
	return newGoal(t, map[string]any{
		"level": 2, "title": title, "kind": "base",
		"parent_goal_id": parentID, "project_id": projectID,
	})
}

// The tier rules are what keep the goal map readable, so they are asserted
// through the handler as well as in internal/goal: this is the layer that has
// to resolve a referenced goal to its tier before the rule can even run, and
// that resolution is where a workspace leak would live.
func TestCreateGoalEnforcesTierAlignment(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Five minute integration")
	product := productGoal(t, "Console rebuild", direction.ID)

	t.Run("cycle goal aligns to a product goal", func(t *testing.T) {
		g := newGoal(t, map[string]any{
			"level": 3, "title": "Console 2.1", "parent_goal_id": product.ID,
		})
		if g.ParentGoalID == nil || *g.ParentGoalID != product.ID {
			t.Fatalf("parent_goal_id = %v, want %s", g.ParentGoalID, product.ID)
		}
		if g.Assignable {
			t.Error("a goal must never report itself as assignable")
		}
	})

	t.Run("skipping a tier is refused", func(t *testing.T) {
		// The refusal exists to force the intermediate product goal into
		// existence, which is the conversation the map is for.
		testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
			"level": 3, "title": "Console 2.1", "parent_goal_id": direction.ID,
		})).Want(http.StatusBadRequest)
	})

	t.Run("unaligned goal must say why", func(t *testing.T) {
		testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
			"level": 3, "title": "Annual event signup tool",
		})).Want(http.StatusBadRequest)

		g := newGoal(t, map[string]any{
			"level": 3, "title": "Annual event signup tool",
			"orphan_reason": "One-off request; no lasting product line to align to.",
		})
		if g.ParentGoalID != nil {
			t.Fatalf("parent_goal_id = %v, want nil for an orphan goal", g.ParentGoalID)
		}
	})

	t.Run("product goal needs a kind and a project", func(t *testing.T) {
		testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
			"level": 2, "title": "Console rebuild", "parent_goal_id": direction.ID,
		})).Want(http.StatusBadRequest)
	})

	t.Run("a goal in another workspace is not a parent", func(t *testing.T) {
		otherWS := dbfx.Insert(t, "workspace", testutil.Cols{
			"name": "Other", "slug": "goal-tier-other", "issue_prefix": "OTH",
		})
		foreign := dbfx.Insert(t, "goal", testutil.Cols{
			"workspace_id": otherWS, "level": 1, "title": "Foreign direction",
			"created_by_type": "member", "created_by_id": testUserID,
		})
		// A 400 naming the field, not a 404: the missing thing is an input the
		// caller sent, and it must not be distinguishable from a bad id.
		testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
			"level": 2, "title": "Leak", "kind": "base", "parent_goal_id": foreign,
		})).Want(http.StatusBadRequest)
	})
}

// An edit is validated against the MERGED goal, not the fields that arrived.
// Patching one field at a time is what lets an invariant break in two
// legal-looking steps.
func TestUpdateGoalRevalidatesTheWholeGoal(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Direction for update")
	product := productGoal(t, "Product for update", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Cycle 1.0", "parent_goal_id": product.ID,
	})

	// Unaligning leaves a goal that could not have been created this way: no
	// parent and no reason. The merged shape is what gets checked.
	testutil.Call(t, testHandler.UpdateGoal,
		withURLParam(newRequest("PUT", "/api/goals/"+cycle.ID, map[string]any{
			"parent_goal_id": "",
		}), "id", cycle.ID)).Want(http.StatusBadRequest)

	var updated GoalResponse
	testutil.Call(t, testHandler.UpdateGoal,
		withURLParam(newRequest("PUT", "/api/goals/"+cycle.ID, map[string]any{
			"parent_goal_id": "",
			"orphan_reason":  "Pulled out of the product line after review.",
		}), "id", cycle.ID)).Want(http.StatusOK).JSON(&updated)
	if updated.ParentGoalID != nil {
		t.Fatalf("parent_goal_id = %v, want nil after unaligning", updated.ParentGoalID)
	}
}

// No foreign keys exist in this schema, so nothing cleans up on its own and
// this ordering IS the contract. Children are demoted, not deleted; milestones
// go with their goal.
func TestDeleteGoalDetachesChildrenAndRemovesMilestones(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Direction for delete")
	product := productGoal(t, "Product for delete", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Cycle for delete", "parent_goal_id": product.ID,
	})

	var milestone MilestoneResponse
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", map[string]any{
			"type": "launch", "title": "Ship it", "planned_date": "2026-10-10",
		}), "id", cycle.ID)).Want(http.StatusCreated).JSON(&milestone)

	testutil.Call(t, testHandler.DeleteGoal,
		withURLParam(newRequest("DELETE", "/api/goals/"+product.ID, nil), "id", product.ID)).
		Want(http.StatusOK)

	// The cycle goal is somebody's real work. Losing its parent demotes it to
	// an unaligned goal; it must not disappear with the parent.
	var survivor GoalResponse
	testutil.Call(t, testHandler.GetGoal,
		withURLParam(newRequest("GET", "/api/goals/"+cycle.ID, nil), "id", cycle.ID)).
		Want(http.StatusOK).JSON(&survivor)
	if survivor.ParentGoalID != nil {
		t.Errorf("parent_goal_id = %v, want nil: a deleted parent must detach its children, not keep pointing at itself",
			survivor.ParentGoalID)
	}

	testutil.Call(t, testHandler.GetGoal,
		withURLParam(newRequest("GET", "/api/goals/"+product.ID, nil), "id", product.ID)).
		Want(http.StatusNotFound)

	// The milestone belonged to the cycle goal, which survived, so it survives
	// too. Deleting the cycle goal is what would take it.
	testutil.Call(t, testHandler.DeleteGoal,
		withURLParam(newRequest("DELETE", "/api/goals/"+cycle.ID, nil), "id", cycle.ID)).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.GetMilestone,
		withURLParam(newRequest("GET", "/api/milestones/"+milestone.ID, nil), "milestoneId", milestone.ID)).
		Want(http.StatusNotFound)
}

// The seam: a goal points at issues, and the issue side stores nothing. This
// is what keeps a goal out of the assignable pool, my-issues and every
// autopilot scan without any of them having to filter for it.
func TestGoalIssueLinkIsStoredOnlyOnTheGoalSide(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Direction for linking")
	product := productGoal(t, "Product for linking", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Cycle for linking", "parent_goal_id": product.ID,
	})
	issueID := dbfx.Issue(t, "Keyboard shortcut system")

	testutil.Call(t, testHandler.LinkGoalIssue,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/issues", map[string]any{
			"issue_id": issueID,
		}), "id", cycle.ID)).Want(http.StatusOK)

	// Linking twice is the same link. An agent may propose one and a person
	// confirm it, and neither should fail on the other's write.
	testutil.Call(t, testHandler.LinkGoalIssue,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/issues", map[string]any{
			"issue_id": issueID,
		}), "id", cycle.ID)).Want(http.StatusOK)

	var linked struct {
		IssueIDs []string `json:"issue_ids"`
	}
	testutil.Call(t, testHandler.ListGoalIssues,
		withURLParam(newRequest("GET", "/api/goals/"+cycle.ID+"/issues", nil), "id", cycle.ID)).
		Want(http.StatusOK).JSON(&linked)
	if len(linked.IssueIDs) != 1 || linked.IssueIDs[0] != issueID {
		t.Fatalf("issue_ids = %v, want exactly [%s]", linked.IssueIDs, issueID)
	}

	var fromIssue struct {
		Goals []GoalResponse `json:"goals"`
	}
	testutil.Call(t, testHandler.ListGoalsForIssue,
		withURLParam(newRequest("GET", "/api/issues/"+issueID+"/goals", nil), "id", issueID)).
		Want(http.StatusOK).JSON(&fromIssue)
	if len(fromIssue.Goals) != 1 || fromIssue.Goals[0].ID != cycle.ID {
		t.Fatalf("goals for issue = %v, want exactly the linked cycle goal", fromIssue.Goals)
	}

	// The issue row itself learned nothing. Asserted directly against the
	// column list because the guarantee is structural, not behavioural: if a
	// goal reference ever appears on issue, every issue query in the product
	// becomes responsible for excluding goals again.
	var goalColumns int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'issue' AND column_name IN ('goal_id', 'level', 'goal_level')
	`).Scan(&goalColumns)
	if goalColumns != 0 {
		t.Errorf("the issue table gained %d goal-related column(s); the goal layer must stay off the issue table", goalColumns)
	}

	testutil.Call(t, testHandler.UnlinkGoalIssue,
		withURLParams(newRequest("DELETE", "/api/goals/"+cycle.ID+"/issues/"+issueID, nil),
			"id", cycle.ID, "issueId", issueID)).Want(http.StatusOK)
}

func TestGoalEndpointsRejectMalformedBodies(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	// Truncated JSON, not merely wrong JSON: the decoder has to fail rather
	// than leave a half-populated request struct to reach the rules.
	malformed := testutil.WithHeaders(
		testutil.JSONRequest("POST", "/api/goals", `{"level": `),
		"X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID,
	)
	testutil.Call(t, testHandler.CreateGoal, malformed).Want(http.StatusBadRequest)
}
