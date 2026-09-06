package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
		Issues []GoalIssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListGoalIssues,
		withURLParam(newRequest("GET", "/api/goals/"+cycle.ID+"/issues", nil), "id", cycle.ID)).
		Want(http.StatusOK).JSON(&linked)
	if len(linked.Issues) != 1 || linked.Issues[0].ID != issueID {
		t.Fatalf("issues = %v, want exactly the linked issue %s", linked.Issues, issueID)
	}
	// Enough of the issue to draw a row. Returning bare ids, as this did first,
	// forces every caller into a second round trip or a whole-workspace issue
	// list just to find a title.
	if linked.Issues[0].Title != "Keyboard shortcut system" {
		t.Errorf("title = %q, want the issue's own title", linked.Issues[0].Title)
	}
	if linked.Issues[0].Number == 0 {
		t.Error("number is required to render the issue identifier")
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

// Everyone in a workspace shares its plan, so every write to the goal tier has
// to reach the people looking at it — not only whoever happens to reload. These
// are the events the client turns into cache invalidation.
func TestGoalWritesBroadcastToTheWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}

	seen := make(chan events.Event, 8)
	for _, eventType := range []string{
		protocol.EventGoalCreated,
		protocol.EventGoalUpdated,
		protocol.EventGoalDeleted,
		protocol.EventGoalIssuesChanged,
		protocol.EventMilestoneCreated,
	} {
		testHandler.Bus.Subscribe(eventType, func(e events.Event) {
			select {
			case seen <- e:
			default:
			}
		})
	}
	drain := func() []events.Event {
		var out []events.Event
		for {
			select {
			case e := <-seen:
				out = append(out, e)
			default:
				return out
			}
		}
	}
	typesOf := func(list []events.Event) []string {
		out := make([]string, len(list))
		for i, e := range list {
			out[i] = e.Type
		}
		return out
	}
	contains := func(list []string, want string) bool {
		for _, got := range list {
			if got == want {
				return true
			}
		}
		return false
	}

	drain()
	direction := directionGoal(t, "Broadcast direction")
	product := productGoal(t, "Broadcast product", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Broadcast cycle", "parent_goal_id": product.ID,
	})
	if got := typesOf(drain()); !contains(got, protocol.EventGoalCreated) {
		t.Fatalf("creating a goal published %v, want it to include %s", got, protocol.EventGoalCreated)
	}

	// Every event carries the workspace, because that is what the realtime hub
	// routes on: an event without one reaches nobody.
	testutil.Call(t, testHandler.UpdateGoal,
		withURLParam(newRequest("PUT", "/api/goals/"+cycle.ID, map[string]any{
			"title": "Broadcast cycle, renamed",
		}), "id", cycle.ID)).Want(http.StatusOK)
	updates := drain()
	if !contains(typesOf(updates), protocol.EventGoalUpdated) {
		t.Fatalf("updating a goal published %v, want %s", typesOf(updates), protocol.EventGoalUpdated)
	}
	for _, event := range updates {
		if event.WorkspaceID != testWorkspaceID {
			t.Errorf("%s carried workspace %q, want %q — the hub routes on it",
				event.Type, event.WorkspaceID, testWorkspaceID)
		}
	}

	issueID := dbfx.Issue(t, "Broadcast linked issue")
	testutil.Call(t, testHandler.LinkGoalIssue,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/issues", map[string]any{
			"issue_id": issueID,
		}), "id", cycle.ID)).Want(http.StatusOK)
	if got := typesOf(drain()); !contains(got, protocol.EventGoalIssuesChanged) {
		t.Fatalf("linking an issue published %v, want %s", got, protocol.EventGoalIssuesChanged)
	}

	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", map[string]any{
			"type": "launch", "title": "Broadcast launch", "planned_date": "2026-10-10",
		}), "id", cycle.ID)).Want(http.StatusCreated)
	if got := typesOf(drain()); !contains(got, protocol.EventMilestoneCreated) {
		t.Fatalf("creating a milestone published %v, want %s", got, protocol.EventMilestoneCreated)
	}

	// Deletion publishes AFTER the commit. Announcing it inside the transaction
	// would let a listener refetch, see the goal gone, and then have it come
	// back when the transaction rolled back.
	testutil.Call(t, testHandler.DeleteGoal,
		withURLParam(newRequest("DELETE", "/api/goals/"+cycle.ID, nil), "id", cycle.ID)).
		Want(http.StatusOK)
	if got := typesOf(drain()); !contains(got, protocol.EventGoalDeleted) {
		t.Fatalf("deleting a goal published %v, want %s", got, protocol.EventGoalDeleted)
	}
}

// Creating and editing stay open to every member — a plan only some people can
// touch stops being shared. Deleting an upper tier does not, because it
// detaches everything aligned to it across the workspace, which is work other
// people are in the middle of.
func TestDeletingAnUpperTierNeedsMoreThanMembership(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}

	// A plain member of the fixture workspace.
	// Insert quotes the identifier itself, so the table name goes in bare.
	memberUserID := dbfx.Insert(t, "user", testutil.Cols{
		"name": "Goal Delete Member", "email": "goal-delete-member@multica.ai",
	})
	dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": testWorkspaceID, "user_id": memberUserID, "role": "member",
	})
	asMember := func(method, path string, body any) *http.Request {
		return testutil.WithHeaders(newRequest(method, path, body), "X-User-ID", memberUserID)
	}

	direction := directionGoal(t, "Role gate direction")
	product := productGoal(t, "Role gate product", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Role gate cycle", "parent_goal_id": product.ID,
	})

	t.Run("a member may still edit an upper tier", func(t *testing.T) {
		testutil.Call(t, testHandler.UpdateGoal,
			withURLParam(asMember("PUT", "/api/goals/"+direction.ID, map[string]any{
				"title": "Role gate direction, renamed by a member",
			}), "id", direction.ID)).Want(http.StatusOK)
	})

	t.Run("a member may delete a cycle goal", func(t *testing.T) {
		// Its blast radius is its own row, so its owner keeps it.
		own := newGoal(t, map[string]any{
			"level": 3, "title": "Member's own cycle goal", "parent_goal_id": product.ID,
		})
		testutil.Call(t, testHandler.DeleteGoal,
			withURLParam(asMember("DELETE", "/api/goals/"+own.ID, nil), "id", own.ID)).
			Want(http.StatusOK)
	})

	t.Run("a member may not delete a product goal", func(t *testing.T) {
		testutil.Call(t, testHandler.DeleteGoal,
			withURLParam(asMember("DELETE", "/api/goals/"+product.ID, nil), "id", product.ID)).
			Want(http.StatusForbidden)
	})

	t.Run("a member may not delete a direction", func(t *testing.T) {
		testutil.Call(t, testHandler.DeleteGoal,
			withURLParam(asMember("DELETE", "/api/goals/"+direction.ID, nil), "id", direction.ID)).
			Want(http.StatusForbidden)
	})

	t.Run("an admin may", func(t *testing.T) {
		// The fixture user owns the workspace.
		testutil.Call(t, testHandler.DeleteGoal,
			withURLParam(newRequest("DELETE", "/api/goals/"+product.ID, nil), "id", product.ID)).
			Want(http.StatusOK)
		// And the cycle goal beneath it survives, demoted rather than removed.
		testutil.Call(t, testHandler.GetGoal,
			withURLParam(newRequest("GET", "/api/goals/"+cycle.ID, nil), "id", cycle.ID)).
			Want(http.StatusOK)
	})
}

// The metrics are the place a goal layer most easily turns into a performance
// instrument, so the shape is asserted, not just the arithmetic: an owner
// dimension appearing here later would be a change of what the feature is for.
func TestGoalMetricsCarryNoPerPersonDimension(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Metrics direction")
	product := productGoal(t, "Metrics product", direction.ID)
	newGoal(t, map[string]any{
		"level": 3, "title": "Metrics cycle", "parent_goal_id": product.ID,
		"cycle": "metrics-test-cycle",
	})

	var raw map[string]any
	testutil.Call(t, testHandler.GetGoalMetrics,
		newRequest("GET", "/api/goals/metrics", nil)).Want(http.StatusOK).JSON(&raw)

	for key := range raw {
		for _, banned := range []string{"owner", "member", "user", "assignee", "by_person", "actor"} {
			if strings.Contains(strings.ToLower(key), banned) {
				t.Errorf("metrics returned %q: attainment that can be sliced by person is a performance instrument, and the data stops being true once it is one", key)
			}
		}
	}

	// The arithmetic still has to work, or the shape guarantee is protecting
	// nothing worth having.
	var metrics GoalMetricsResponse
	testutil.Call(t, testHandler.GetGoalMetrics,
		newRequest("GET", "/api/goals/metrics?cycle=metrics-test-cycle", nil)).
		Want(http.StatusOK).JSON(&metrics)
	if metrics.Goals != 1 {
		t.Errorf("goals in the named cycle = %d, want 1", metrics.Goals)
	}
	if metrics.Cycle != "metrics-test-cycle" {
		t.Errorf("cycle = %q, want it echoed back", metrics.Cycle)
	}
}

// The dates and the count are the record and everyone sees them. The reasons
// are only worth collecting while the writer is explaining a plan rather than
// defending one, and a log the whole workspace reads fills with "delay".
func TestRescheduleReasonsAreNotWorkspaceReadable(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "ReasonVisibility", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-03",
	})
	testutil.Call(t, testHandler.RescheduleMilestone,
		withURLParam(newRequest("PATCH", "/api/milestones/"+m.ID+"/schedule", map[string]any{
			"planned_date": "2026-10-10", "reason": "Customer rollout slipped a week.",
		}), "milestoneId", m.ID)).Want(http.StatusOK)

	read := func(req *http.Request) map[string]any {
		var out map[string]any
		testutil.Call(t, testHandler.ListMilestoneDateChanges,
			withURLParam(req, "milestoneId", m.ID)).Want(http.StatusOK).JSON(&out)
		return out
	}

	// The workspace owner runs the review and needs the reason to act on it.
	asOwner := read(newRequest("GET", "/api/milestones/"+m.ID+"/date-changes", nil))
	if asOwner["reasons_visible"] != true {
		t.Fatal("a workspace owner must be able to read why a date moved")
	}
	ownerRows, _ := asOwner["date_changes"].([]any)
	if len(ownerRows) != 1 {
		t.Fatalf("date_changes = %d, want 1", len(ownerRows))
	}
	if first, _ := ownerRows[0].(map[string]any); first["reason"] == nil {
		t.Error("the reason is missing for the reviewer who needs it")
	}

	// A plain member sees that it moved, and when, and not why.
	memberUserID := dbfx.Insert(t, "user", testutil.Cols{
		"name": "Reason Reader", "email": "reason-reader@multica.ai",
	})
	dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": testWorkspaceID, "user_id": memberUserID, "role": "member",
	})
	asMember := read(testutil.WithHeaders(
		newRequest("GET", "/api/milestones/"+m.ID+"/date-changes", nil),
		"X-User-ID", memberUserID,
	))
	if asMember["reasons_visible"] != false {
		t.Fatal("a plain member must not read the reasons")
	}
	memberRows, _ := asMember["date_changes"].([]any)
	if len(memberRows) != 1 {
		t.Fatalf("a member must still see THAT it moved: rows = %d, want 1", len(memberRows))
	}
	row, _ := memberRows[0].(map[string]any)
	if row["reason"] != nil {
		t.Error("the reason reached a member it was not written for")
	}
	if row["changed_by_id"] != nil {
		t.Error("who moved it is part of the same answer and must not leak either")
	}
	if row["from_date"] == nil || row["to_date"] == nil {
		t.Error("the dates are the record and must stay visible to everyone")
	}
}
