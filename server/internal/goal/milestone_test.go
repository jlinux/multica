package goal

import (
	"strings"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// The full transition matrix, asserted pair by pair. A state machine written
// as a map is easy to extend by accident, and the reachable set is a product
// rule: achieved and cancelled are terminal on purpose.
func TestCanTransition(t *testing.T) {
	all := []MilestoneStatus{
		MilestonePlanned, MilestoneInProgress, MilestonePendingAccept,
		MilestoneAchieved, MilestoneCancelled,
	}
	allowed := map[[2]MilestoneStatus]bool{
		{MilestonePlanned, MilestoneInProgress}:       true,
		{MilestonePlanned, MilestonePendingAccept}:    true,
		{MilestonePlanned, MilestoneCancelled}:        true,
		{MilestoneInProgress, MilestonePendingAccept}: true,
		{MilestoneInProgress, MilestoneCancelled}:     true,
		{MilestonePendingAccept, MilestoneAchieved}:   true,
		{MilestonePendingAccept, MilestoneInProgress}: true,
		{MilestonePendingAccept, MilestoneCancelled}:  true,
	}
	for _, from := range all {
		for _, to := range all {
			want := from == to || allowed[[2]MilestoneStatus{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s -> %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	// Stated separately because the reason is a product decision, not a gap in
	// the table: a milestone that was achieved and then was not is a new fact
	// about a later period and belongs in a new milestone.
	if CanTransition(MilestoneAchieved, MilestoneInProgress) {
		t.Error("achieved must be terminal; reopening rewrites history instead of recording it")
	}
}

func TestRequiresAcceptance(t *testing.T) {
	if RequiresAcceptance(MilestoneLaunch) {
		t.Error("launch is settled by the engineering record (a merged PR, a tag) and needs no acceptance")
	}
	for _, ty := range []MilestoneType{MilestoneFirstUse, MilestoneNthUse} {
		if !RequiresAcceptance(ty) {
			t.Errorf("%s is a claim about the world outside the repository; nothing in the repository can settle it", ty)
		}
	}
}

func TestValidateMilestone(t *testing.T) {
	base := func() MilestoneInput {
		return MilestoneInput{
			Type: MilestoneFirstUse, Title: "First real batch import",
			PlannedDate: day("2026-10-10"), HasPlannedDate: true,
			Adoption: AdoptionVerifier, HasVerifier: true,
		}
	}
	tests := []struct {
		name    string
		mutate  func(*MilestoneInput)
		wantErr string
	}{
		{name: "verifier-checked first use", mutate: func(*MilestoneInput) {}},
		{
			name:   "launch needs no adoption check",
			mutate: func(in *MilestoneInput) { in.Type = MilestoneLaunch; in.Adoption = ""; in.HasVerifier = false },
		},
		{
			name:    "adoption check is required for first use",
			mutate:  func(in *MilestoneInput) { in.Adoption = "" },
			wantErr: "verifier, analytics or agent",
		},
		{
			name:    "adoption check is meaningless on launch",
			mutate:  func(in *MilestoneInput) { in.Type = MilestoneLaunch },
			wantErr: "first_use and nth_use milestones only",
		},
		{
			name:    "verifier method needs a verifier",
			mutate:  func(in *MilestoneInput) { in.HasVerifier = false },
			wantErr: "someone named to confirm",
		},
		{
			name:   "an agent may be the adoption check",
			mutate: func(in *MilestoneInput) { in.Adoption = AdoptionAgent; in.HasVerifier = false },
		},
		{
			name:   "nth use with a valid N",
			mutate: func(in *MilestoneInput) { in.Type = MilestoneNthUse; in.N = 10 },
		},
		{
			name:    "N=1 duplicates the first-use stage",
			mutate:  func(in *MilestoneInput) { in.Type = MilestoneNthUse; in.N = 1 },
			wantErr: "at least 2",
		},
		{
			name:    "N outside nth use",
			mutate:  func(in *MilestoneInput) { in.N = 3 },
			wantErr: "nth_use milestones only",
		},
		{
			name:    "a milestone without a date is a wish",
			mutate:  func(in *MilestoneInput) { in.HasPlannedDate = false },
			wantErr: "planned date is required",
		},
		{name: "blank title", mutate: func(in *MilestoneInput) { in.Title = " " }, wantErr: "title is required"},
		{name: "unknown type", mutate: func(in *MilestoneInput) { in.Type = "shipped" }, wantErr: "launch, first_use or nth_use"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			err := ValidateMilestone(in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateMilestone(%+v) = %v, want nil", in, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateMilestone(%+v) = %v, want error containing %q", in, err, tc.wantErr)
			}
		})
	}
}

func TestIsDelayed(t *testing.T) {
	planned := day("2026-10-10")
	tests := []struct {
		name   string
		status MilestoneStatus
		today  string
		want   bool
	}{
		// Due today is not late until today is over. Planned dates carry no
		// time of day, so the comparison has to be by calendar day.
		{"due today", MilestonePlanned, "2026-10-10", false},
		{"day before", MilestonePlanned, "2026-10-09", false},
		{"day after", MilestonePlanned, "2026-10-11", true},
		{"in progress and overdue", MilestoneInProgress, "2026-10-20", true},
		{"waiting on a verifier and overdue", MilestonePendingAccept, "2026-10-20", true},
		// Terminal states are never late; the flag describes open work.
		{"achieved late is not delayed", MilestoneAchieved, "2026-12-01", false},
		{"cancelled is not delayed", MilestoneCancelled, "2026-12-01", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDelayed(tc.status, planned, day(tc.today)); got != tc.want {
				t.Errorf("IsDelayed(%s, planned=%s, today=%s) = %v, want %v",
					tc.status, planned.Format("2006-01-02"), tc.today, got, tc.want)
			}
		})
	}
}

// On-time is measured against the FIRST date ever planned. Measuring against
// the current one would make every rescheduled milestone on time by
// construction, flattering exactly the plans that slipped most.
func TestOnTimeUsesTheOriginalDate(t *testing.T) {
	original := day("2026-10-03")
	rescheduled := day("2026-10-10")
	actual := day("2026-10-09")

	if OnTime(actual, original) {
		t.Error("landing after the first planned date must not count as on time")
	}
	if !OnTime(actual, rescheduled) {
		t.Fatal("guard: the actual date is inside the rescheduled window, which is what makes this test meaningful")
	}
	if !OnTime(day("2026-10-03"), original) {
		t.Error("landing exactly on the planned day is on time")
	}
}

func TestCanPropose(t *testing.T) {
	ok := []struct{ from, to MilestoneStatus }{
		{MilestonePlanned, MilestonePendingAccept},
		{MilestoneInProgress, MilestonePendingAccept},
		{MilestonePendingAccept, MilestoneAchieved},
	}
	for _, tc := range ok {
		if !CanPropose(tc.from, tc.to) {
			t.Errorf("CanPropose(%s -> %s) = false, want true", tc.from, tc.to)
		}
	}
	// Cancelling and pulling a milestone backwards are judgements about
	// whether the work should continue. An agent has no standing to make them.
	bad := []struct{ from, to MilestoneStatus }{
		{MilestonePlanned, MilestoneCancelled},
		{MilestonePendingAccept, MilestoneCancelled},
		{MilestonePendingAccept, MilestoneInProgress},
		{MilestoneAchieved, MilestoneAchieved},
		{MilestonePlanned, MilestoneAchieved},
	}
	for _, tc := range bad {
		if CanPropose(tc.from, tc.to) {
			t.Errorf("CanPropose(%s -> %s) = true, want false", tc.from, tc.to)
		}
	}
}

// The single most important rule in this package: an agent may propose that a
// milestone was reached, and may never accept its own proposal.
func TestAgentCannotAcceptItsOwnProposal(t *testing.T) {
	if CanDecide(ProposalAccepted, ActorAgent) {
		t.Fatal("an agent must not accept a proposal: the party doing the work cannot also certify the work")
	}
	if CanDecide(ProposalRejected, ActorAgent) {
		t.Error("rejection is a human judgement too")
	}
	for _, actor := range []ActorType{ActorMember, ActorExternal} {
		if !CanDecide(ProposalAccepted, actor) {
			t.Errorf("%s must be able to accept a proposal", actor)
		}
	}
	// Superseding is not a judgement — it is what happens to an older proposal
	// when a newer one arrives, so the system records it itself.
	if !CanDecide(ProposalSuperseded, ActorAgent) {
		t.Error("superseding is bookkeeping, not a decision, and must not require a human")
	}
}

func TestValidateProposal(t *testing.T) {
	tests := []struct {
		name    string
		in      ProposalInput
		wantErr string
	}{
		{
			name: "evidence-backed achievement",
			in: ProposalInput{
				From: MilestonePendingAccept, To: MilestoneAchieved,
				Evidence: "Three tenants completed an end-to-end batch import; see access log query.",
				HasDate:  true,
			},
		},
		{
			name:    "no evidence",
			in:      ProposalInput{From: MilestonePlanned, To: MilestonePendingAccept, Evidence: "done"},
			wantErr: "what it is based on",
		},
		{
			name: "achieved without a date",
			in: ProposalInput{
				From: MilestonePendingAccept, To: MilestoneAchieved,
				Evidence: "Three tenants completed an end-to-end batch import.",
			},
			wantErr: "date it happened",
		},
		{
			name: "agents cannot propose a cancellation",
			in: ProposalInput{
				From: MilestonePlanned, To: MilestoneCancelled,
				Evidence: "The customer went quiet for six weeks.",
			},
			wantErr: "cannot be proposed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProposal(tc.in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateProposal(%+v) = %v, want nil", tc.in, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateProposal(%+v) = %v, want error containing %q", tc.in, err, tc.wantErr)
			}
		})
	}
}

// A launch is settled by the engineering record, so it may be marked reached
// without first being handed to a reviewer who does not exist. An adoption
// milestone may not: skipping the handoff is exactly how the person who built
// the thing ends up declaring it used.
func TestOnlyALaunchMayBeReachedWithoutAHandoff(t *testing.T) {
	if !CanTransitionFor(MilestoneLaunch, MilestonePlanned, MilestoneAchieved) {
		t.Error("a launch that just shipped must be markable as reached in one step")
	}
	if !CanTransitionFor(MilestoneLaunch, MilestoneInProgress, MilestoneAchieved) {
		t.Error("a launch in progress must be markable as reached")
	}
	for _, ty := range []MilestoneType{MilestoneFirstUse, MilestoneNthUse} {
		if CanTransitionFor(ty, MilestonePlanned, MilestoneAchieved) {
			t.Errorf("%s must pass through pending_accept; the handoff is the point", ty)
		}
		if !CanTransitionFor(ty, MilestonePendingAccept, MilestoneAchieved) {
			t.Errorf("%s must still be reachable once it has been handed over", ty)
		}
	}
	// The extra edge is additive: nothing the generic machine refused for
	// another reason becomes legal because the type is a launch.
	if CanTransitionFor(MilestoneLaunch, MilestoneAchieved, MilestoneInProgress) {
		t.Error("achieved is terminal for every type")
	}
	if CanTransitionFor(MilestoneLaunch, MilestoneCancelled, MilestoneAchieved) {
		t.Error("a cancelled milestone is not reachable")
	}
}
