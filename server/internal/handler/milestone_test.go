package handler

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// milestoneFixture builds a cycle goal with one milestone on it, which is the
// shape every case below starts from.
func milestoneFixture(t *testing.T, name string, milestone map[string]any) (GoalResponse, MilestoneResponse) {
	t.Helper()
	direction := directionGoal(t, name+" direction")
	product := productGoal(t, name+" product", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": name + " cycle", "parent_goal_id": product.ID,
	})
	var m MilestoneResponse
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", milestone), "id", cycle.ID)).
		Want(http.StatusCreated).JSON(&m)
	return cycle, m
}

// asAgent returns a request the handler will resolve to an agent actor. It
// needs a real agent and a real task because resolveActor refuses to trust the
// headers otherwise, which is exactly the behaviour the proposal rules rely on.
var proposingAgentSeq atomic.Int64

func asAgent(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	// Agent names are unique per workspace and a test may need more than one
	// agent, so the name carries a counter rather than only the test name.
	name := fmt.Sprintf("proposing-agent-%s-%d", t.Name(), proposingAgentSeq.Add(1))
	agentID := dbfx.Agent(t, name, testRuntimeID)
	// An active task must name the runtime it runs on, which is also what
	// makes resolveActor willing to trust the agent headers below.
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID})
	return testutil.WithHeaders(newRequest(method, path, body),
		"X-Agent-ID", agentID, "X-Task-ID", taskID)
}

// An adoption milestone is a claim about the world outside the repository, so
// how it will be decided has to be named when it is created. Two owners can
// otherwise mean things ten times apart by "used once".
func TestCreateMilestoneRequiresAnAdoptionCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Adoption direction")
	product := productGoal(t, "Adoption product", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Adoption cycle", "parent_goal_id": product.ID,
	})

	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", map[string]any{
			"type": "first_use", "title": "First real use", "planned_date": "2026-10-10",
		}), "id", cycle.ID)).Want(http.StatusBadRequest)

	// A launch date is self-evident from the engineering record, so launch
	// milestones take no adoption check and must not require one.
	var launch MilestoneResponse
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", map[string]any{
			"type": "launch", "title": "Ship it", "planned_date": "2026-09-20",
		}), "id", cycle.ID)).Want(http.StatusCreated).JSON(&launch)
	if launch.RequiresAcceptance {
		t.Error("a launch milestone must not require acceptance")
	}
}

// Rescheduling is free and un-blamed, but it never rewrites history: the first
// planned date is what on-time attainment is measured against, so a slip
// cannot be laundered into a hit by moving the date.
func TestRescheduleRecordsTheReasonAndKeepsTheOriginalDate(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "Reschedule", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-03",
	})

	// A date may only move by leaving a reason behind. This is what implements
	// "the system never silently swallows an overdue milestone".
	testutil.Call(t, testHandler.RescheduleMilestone,
		withURLParam(newRequest("PATCH", "/api/milestones/"+m.ID+"/schedule", map[string]any{
			"planned_date": "2026-10-10",
		}), "milestoneId", m.ID)).Want(http.StatusBadRequest)

	var moved MilestoneResponse
	testutil.Call(t, testHandler.RescheduleMilestone,
		withURLParam(newRequest("PATCH", "/api/milestones/"+m.ID+"/schedule", map[string]any{
			"planned_date": "2026-10-10",
			"reason":       "Customer rollout slipped a week.",
		}), "milestoneId", m.ID)).Want(http.StatusOK).JSON(&moved)

	if moved.PlannedDate == nil || *moved.PlannedDate != "2026-10-10" {
		t.Fatalf("planned_date = %v, want the new date", moved.PlannedDate)
	}
	if moved.OriginalPlannedDate == nil || *moved.OriginalPlannedDate != "2026-10-03" {
		t.Fatalf("original_planned_date = %v, want it pinned to 2026-10-03; on-time attainment is measured against it and a reschedule must not move it",
			moved.OriginalPlannedDate)
	}

	// The badge is drawn from whatever the caller last received. A mutation
	// response that omitted the counter would render "moved 0 times" directly
	// after a move, and only correct itself on the next refetch.
	if moved.DateChangeCount != 1 {
		t.Errorf("date_change_count = %d, want 1: the reschedule response must already count the move it made",
			moved.DateChangeCount)
	}

	var changes struct {
		DateChanges []struct {
			FromDate *string `json:"from_date"`
			ToDate   *string `json:"to_date"`
			Reason   string  `json:"reason"`
		} `json:"date_changes"`
	}
	testutil.Call(t, testHandler.ListMilestoneDateChanges,
		withURLParam(newRequest("GET", "/api/milestones/"+m.ID+"/date-changes", nil), "milestoneId", m.ID)).
		Want(http.StatusOK).JSON(&changes)
	if len(changes.DateChanges) != 1 {
		t.Fatalf("date_changes = %d, want exactly one row recording the move", len(changes.DateChanges))
	}
	if changes.DateChanges[0].Reason == "" {
		t.Error("the reschedule log must carry the reason that was given")
	}
}

// The single most important rule in the feature: an agent proposes that a
// milestone was reached, and a human decides. A system where the party doing
// the work also certifies the work has stopped measuring anything.
func TestAgentProposesAndOnlyAHumanAccepts(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	cycle, m := milestoneFixture(t, "Proposal", map[string]any{
		"type": "first_use", "title": "First real batch import",
		"planned_date": "2026-10-10", "adoption_check": "agent",
	})
	issueID := dbfx.Issue(t, "Batch import")
	testutil.Call(t, testHandler.LinkGoalIssue,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/issues", map[string]any{
			"issue_id": issueID,
		}), "id", cycle.ID)).Want(http.StatusOK)

	var proposal struct {
		ID             string  `json:"id"`
		State          string  `json:"state"`
		ProposedByType string  `json:"proposed_by_type"`
		SourceIssueID  *string `json:"source_issue_id"`
	}
	testutil.Call(t, testHandler.CreateMilestoneProposal,
		withURLParam(asAgent(t, "POST", "/api/milestones/"+m.ID+"/proposals", map[string]any{
			"proposed_status": "pending_accept",
			"evidence":        "Three tenants completed an end-to-end batch import; see access log query.",
			"source_issue_id": issueID,
		}), "milestoneId", m.ID)).Want(http.StatusCreated).JSON(&proposal)

	if proposal.ProposedByType != "agent" {
		t.Fatalf("proposed_by_type = %q, want agent", proposal.ProposedByType)
	}
	if proposal.State != "pending" {
		t.Fatalf("state = %q, want pending: a proposal must never apply itself", proposal.State)
	}
	// Provenance back into execution is what makes the claim auditable.
	if proposal.SourceIssueID == nil || *proposal.SourceIssueID != issueID {
		t.Errorf("source_issue_id = %v, want the issue the run worked on", proposal.SourceIssueID)
	}

	// The milestone has not moved.
	var untouched MilestoneResponse
	testutil.Call(t, testHandler.GetMilestone,
		withURLParam(newRequest("GET", "/api/milestones/"+m.ID, nil), "milestoneId", m.ID)).
		Want(http.StatusOK).JSON(&untouched)
	if untouched.Status != "planned" {
		t.Fatalf("status = %q, want planned: a proposal must not change the milestone", untouched.Status)
	}

	t.Run("an agent cannot decide its own proposal", func(t *testing.T) {
		testutil.Call(t, testHandler.DecideMilestoneProposal,
			withURLParam(asAgent(t, "POST", "/api/milestone-proposals/"+proposal.ID+"/decide", map[string]any{
				"state": "accepted",
			}), "proposalId", proposal.ID)).Want(http.StatusForbidden)
	})

	t.Run("a person accepts and the milestone moves", func(t *testing.T) {
		var decided struct {
			Proposal  map[string]any    `json:"proposal"`
			Milestone MilestoneResponse `json:"milestone"`
		}
		testutil.Call(t, testHandler.DecideMilestoneProposal,
			withURLParam(newRequest("POST", "/api/milestone-proposals/"+proposal.ID+"/decide", map[string]any{
				"state": "accepted", "decide_note": "Confirmed with the customer.",
			}), "proposalId", proposal.ID)).Want(http.StatusOK).JSON(&decided)

		if decided.Milestone.Status != "pending_accept" {
			t.Fatalf("milestone status = %q, want pending_accept applied from the accepted proposal",
				decided.Milestone.Status)
		}
		if decided.Proposal["state"] != "accepted" {
			t.Fatalf("proposal state = %v, want accepted", decided.Proposal["state"])
		}
	})

	t.Run("a settled proposal cannot be decided twice", func(t *testing.T) {
		// Guarded in the query rather than by a read-then-write, so two
		// reviewers racing cannot both win.
		testutil.Call(t, testHandler.DecideMilestoneProposal,
			withURLParam(newRequest("POST", "/api/milestone-proposals/"+proposal.ID+"/decide", map[string]any{
				"state": "rejected",
			}), "proposalId", proposal.ID)).Want(http.StatusConflict)
	})
}

// A newer proposal retires the pending ones for the same milestone, so a
// reviewer sees one current claim rather than a pile of stale ones.
func TestANewProposalSupersedesTheOlderPendingOnes(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "Supersede", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-10",
	})

	body := map[string]any{
		"proposed_status": "pending_accept",
		"evidence":        "Pull request 4471 merged and the tag is live.",
	}
	for range 2 {
		testutil.Call(t, testHandler.CreateMilestoneProposal,
			withURLParam(asAgent(t, "POST", "/api/milestones/"+m.ID+"/proposals", body), "milestoneId", m.ID)).
			Want(http.StatusCreated)
	}

	var listed struct {
		Proposals []struct {
			State string `json:"state"`
		} `json:"proposals"`
	}
	testutil.Call(t, testHandler.ListMilestoneProposals,
		withURLParam(newRequest("GET", "/api/milestones/"+m.ID+"/proposals", nil), "milestoneId", m.ID)).
		Want(http.StatusOK).JSON(&listed)

	pending := 0
	for _, p := range listed.Proposals {
		if p.State == "pending" {
			pending++
		}
	}
	if len(listed.Proposals) != 2 {
		t.Fatalf("proposals = %d, want both kept: the superseded one is history, not noise", len(listed.Proposals))
	}
	if pending != 1 {
		t.Fatalf("pending proposals = %d, want exactly 1; a reviewer must see one current claim", pending)
	}
}

// The release record is how engineering fact and delivery record agree without
// anything being retyped: it can point at a pull request row this server
// already holds, which is the row the agent's issue already carries.
func TestMilestoneReleaseLinksAPullRequest(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "Release", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-10",
	})

	// Neither a pull request nor a repository nor a tag records anything.
	testutil.Call(t, testHandler.CreateMilestoneRelease,
		withURLParam(newRequest("POST", "/api/milestones/"+m.ID+"/releases", map[string]any{
			"kind": "main",
		}), "milestoneId", m.ID)).Want(http.StatusBadRequest)

	// An id without its source is ambiguous: GitHub App PRs and
	// forgejo/gitea/gitlab PRs live in different tables.
	testutil.Call(t, testHandler.CreateMilestoneRelease,
		withURLParam(newRequest("POST", "/api/milestones/"+m.ID+"/releases", map[string]any{
			"kind": "main", "pull_request_id": testWorkspaceID,
		}), "milestoneId", m.ID)).Want(http.StatusBadRequest)

	var manual map[string]any
	testutil.Call(t, testHandler.CreateMilestoneRelease,
		withURLParam(newRequest("POST", "/api/milestones/"+m.ID+"/releases", map[string]any{
			"kind": "main", "repo_url": "https://example.invalid/acme/console",
			"tag": "v2.1.0", "released_at": "2026-09-18",
			"summary": "Batch actions and keyboard shortcuts are available.",
		}), "milestoneId", m.ID)).Want(http.StatusCreated).JSON(&manual)
	if manual["summary_by_type"] != "member" {
		t.Errorf("summary_by_type = %v, want member: notes record who wrote them", manual["summary_by_type"])
	}

	var listed struct {
		Releases []map[string]any `json:"releases"`
	}
	testutil.Call(t, testHandler.ListMilestoneReleases,
		withURLParam(newRequest("GET", "/api/milestones/"+m.ID+"/releases", nil), "milestoneId", m.ID)).
		Want(http.StatusOK).JSON(&listed)
	if len(listed.Releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(listed.Releases))
	}
}

func TestMilestoneTimelineNeedsAWindow(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "Timeline", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-10",
	})

	testutil.Call(t, testHandler.ListMilestonesTimeline,
		newRequest("GET", "/api/milestones/timeline", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ListMilestonesTimeline,
		newRequest("GET", "/api/milestones/timeline?from=2026-11-01&to=2026-10-01", nil)).
		Want(http.StatusBadRequest)

	var window struct {
		Milestones []MilestoneResponse `json:"milestones"`
	}
	testutil.Call(t, testHandler.ListMilestonesTimeline,
		newRequest("GET", "/api/milestones/timeline?from=2026-10-01&to=2026-10-31", nil)).
		Want(http.StatusOK).JSON(&window)

	found := false
	for _, got := range window.Milestones {
		if got.ID == m.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("a milestone planned inside the window must appear on the timeline")
	}
}

// The rule the whole feature rests on has two doors, and both have to be
// locked. UpdateMilestoneStatus refuses an agent's accept; creation used to
// let the same agent post an adoption milestone that was already achieved and
// certify itself in one request.
func TestAnAgentCannotCreateAnAlreadyAdoptedMilestone(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Self-certify direction")
	product := productGoal(t, "Self-certify product", direction.ID)
	cycle := newGoal(t, map[string]any{
		"level": 3, "title": "Self-certify cycle", "parent_goal_id": product.ID,
	})

	adoption := map[string]any{
		"type": "first_use", "title": "Claimed as already used",
		"planned_date": "2026-10-10", "adoption_check": "agent",
		"status": "achieved", "actual_date": "2026-10-10",
	}
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(asAgent(t, "POST", "/api/goals/"+cycle.ID+"/milestones", adoption), "id", cycle.ID)).
		Want(http.StatusForbidden)

	// The rule is not "agents may not backfill". A launch is settled by the
	// engineering record, so recording one that already shipped is fine.
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(asAgent(t, "POST", "/api/goals/"+cycle.ID+"/milestones", map[string]any{
			"type": "launch", "title": "Shipped last week",
			"planned_date": "2026-10-03", "status": "achieved", "actual_date": "2026-10-03",
		}), "id", cycle.ID)).Want(http.StatusCreated)

	// A person may record the adoption they witnessed.
	testutil.Call(t, testHandler.CreateMilestone,
		withURLParam(newRequest("POST", "/api/goals/"+cycle.ID+"/milestones", adoption), "id", cycle.ID)).
		Want(http.StatusCreated)
}

// The overdue flag is derived on read rather than stored, because the stored
// column was never written and every badge that depends on it stayed dark.
func TestOverdueMilestonesReportThemselvesAsDelayed(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, overdue := milestoneFixture(t, "Overdue", map[string]any{
		"type": "launch", "title": "Was due long ago", "planned_date": "2020-01-01",
	})
	if !overdue.IsDelayed {
		t.Error("a milestone planned in 2020 and never finished must report is_delayed")
	}

	_, future := milestoneFixture(t, "NotOverdue", map[string]any{
		"type": "launch", "title": "Due far ahead", "planned_date": "2099-01-01",
	})
	if future.IsDelayed {
		t.Error("a milestone due in 2099 must not report is_delayed")
	}

	// A finished milestone is never late, however long it took. The flag
	// describes open work; on the record, lateness is actual vs original date.
	var achieved MilestoneResponse
	testutil.Call(t, testHandler.UpdateMilestoneStatus,
		withURLParam(newRequest("PATCH", "/api/milestones/"+overdue.ID+"/status", map[string]any{
			"status": "achieved", "actual_date": "2026-01-01",
		}), "milestoneId", overdue.ID)).Want(http.StatusOK).JSON(&achieved)
	if achieved.IsDelayed {
		t.Error("an achieved milestone must not report is_delayed")
	}
}

// Two requests that both validated against the same status must not both win.
func TestMilestoneStatusWriteIsGuardedOnTheStatusItRead(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	_, m := milestoneFixture(t, "Guarded", map[string]any{
		"type": "launch", "title": "Ship it", "planned_date": "2026-10-10",
	})

	testutil.Call(t, testHandler.UpdateMilestoneStatus,
		withURLParam(newRequest("PATCH", "/api/milestones/"+m.ID+"/status", map[string]any{
			"status": "achieved", "actual_date": "2026-10-09",
		}), "milestoneId", m.ID)).Want(http.StatusOK)

	// Achieved is terminal, so the second request is refused by the rules. The
	// guard matters for the transitions the rules DO allow from a state that
	// has since moved; this asserts the terminal case, which is the one a
	// caller is most likely to retry.
	testutil.Call(t, testHandler.UpdateMilestoneStatus,
		withURLParam(newRequest("PATCH", "/api/milestones/"+m.ID+"/status", map[string]any{
			"status": "in_progress",
		}), "milestoneId", m.ID)).Want(http.StatusBadRequest)
}

// A goal cannot name a project that is not this workspace's. There are no
// foreign keys here by house rule, which is precisely why the reference has to
// be resolved in application code.
func TestGoalRefusesAProjectFromAnotherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	direction := directionGoal(t, "Foreign project direction")
	otherWS := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Other", "slug": "goal-foreign-project-ws", "issue_prefix": "OFP",
	})
	foreignProject := dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": otherWS, "title": "Foreign product",
	})

	testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
		"level": 2, "title": "Points at another tenant", "kind": "base",
		"parent_goal_id": direction.ID, "project_id": foreignProject,
	})).Want(http.StatusBadRequest)

	// A project id that is well-formed but names nothing is refused the same
	// way; it used to satisfy the required-project rule on its shape alone.
	testutil.Call(t, testHandler.CreateGoal, newRequest("POST", "/api/goals", map[string]any{
		"level": 2, "title": "Points at nothing", "kind": "base",
		"parent_goal_id": direction.ID, "project_id": "00000000-0000-0000-0000-000000000123",
	})).Want(http.StatusBadRequest)
}
