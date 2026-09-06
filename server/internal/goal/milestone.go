package goal

import (
	"strings"
	"time"
)

// MilestoneType is the three-stage arc a delivery goes through. The order of
// the constants is the order they are always displayed and reached in.
type MilestoneType string

const (
	// MilestoneLaunch: the thing is available to the people who will use it.
	MilestoneLaunch MilestoneType = "launch"
	// MilestoneFirstUse: someone ran a real end-to-end pass with it. This is
	// the stage every other tool omits, and the reason the arc exists.
	MilestoneFirstUse MilestoneType = "first_use"
	// MilestoneNthUse: it is still being used, N passes in.
	MilestoneNthUse MilestoneType = "nth_use"
)

// MilestoneStatus mirrors the milestone.status CHECK in migration 453.
// IsDelayed is an overlay flag, not a member of this set: a milestone can be
// in progress and late at the same time, and collapsing the two would lose
// whichever fact was written second.
type MilestoneStatus string

const (
	MilestonePlanned       MilestoneStatus = "planned"
	MilestoneInProgress    MilestoneStatus = "in_progress"
	MilestonePendingAccept MilestoneStatus = "pending_accept"
	MilestoneAchieved      MilestoneStatus = "achieved"
	MilestoneCancelled     MilestoneStatus = "cancelled"
)

// AdoptionCheck is how "it was really used" gets decided for this milestone,
// fixed when it is created.
//
// Leaving this to a verbal understanding is what makes an adoption rate
// meaningless: two owners in one workspace can mean things ten times apart by
// "used once", and the number stops being comparable across the very products
// it is supposed to compare. Naming the method up front is cheap; discovering
// at review time that nobody agreed is not.
type AdoptionCheck string

const (
	// AdoptionVerifier: a named person confirms, once.
	AdoptionVerifier AdoptionCheck = "verifier"
	// AdoptionAnalytics: an event threshold in the product's own telemetry.
	AdoptionAnalytics AdoptionCheck = "analytics"
	// AdoptionAgent: an agent inspects logs, tickets or data and reports what
	// it found, with evidence, for a human to accept.
	AdoptionAgent AdoptionCheck = "agent"
)

// ValidMilestoneType reports whether t names a stage of the arc.
func ValidMilestoneType(t MilestoneType) bool {
	switch t {
	case MilestoneLaunch, MilestoneFirstUse, MilestoneNthUse:
		return true
	}
	return false
}

// ValidAdoptionCheck reports whether c names a decision method.
func ValidAdoptionCheck(c AdoptionCheck) bool {
	switch c {
	case AdoptionVerifier, AdoptionAnalytics, AdoptionAgent:
		return true
	}
	return false
}

// milestoneTransitions is the state machine, written out rather than derived
// so that reading it answers "what can happen next" directly.
//
// pending_accept is a real state and not a flag because it is where the
// milestone changes hands: the owner has done everything they can and the
// decision now belongs to someone else. A system without it silently lets the
// person who built the thing also declare it adopted.
var milestoneTransitions = map[MilestoneStatus][]MilestoneStatus{
	MilestonePlanned:       {MilestoneInProgress, MilestonePendingAccept, MilestoneCancelled},
	MilestoneInProgress:    {MilestonePendingAccept, MilestoneCancelled},
	MilestonePendingAccept: {MilestoneAchieved, MilestoneInProgress, MilestoneCancelled},
	// Terminal. Reopening is deliberately not a transition: a milestone that
	// was achieved and then was not is a new fact about a later period, and
	// belongs in a new milestone rather than in a rewritten old one.
	MilestoneAchieved:  {},
	MilestoneCancelled: {},
}

// CanTransition reports whether a milestone may move from -> to, ignoring what
// kind of milestone it is. Prefer CanTransitionFor, which knows.
//
// A no-op move is allowed so that an idempotent retry of the same write does
// not have to be special-cased by every caller.
func CanTransition(from, to MilestoneStatus) bool {
	if from == to {
		return true
	}
	for _, allowed := range milestoneTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// CanTransitionFor reports whether a milestone of this type may move from -> to.
//
// The difference from CanTransition is one edge: a launch may be marked reached
// directly, without first being handed to anyone.
//
// pending_accept exists because the owner has done all they can and the
// decision now belongs to someone else. For an adoption milestone that someone
// is real — a verifier, an event threshold, an agent's evidence — and skipping
// the handoff would let the person who built the thing also declare it used.
// For a launch there is nobody else: it is settled by the engineering record,
// and routing it through a handoff state invents a reviewer who does not
// exist. Requiring it produced a "Mark reached" button that could only ever
// fail, which is how this edge was found.
func CanTransitionFor(milestoneType MilestoneType, from, to MilestoneStatus) bool {
	if to == MilestoneAchieved && !RequiresAcceptance(milestoneType) {
		switch from {
		case MilestonePlanned, MilestoneInProgress, MilestonePendingAccept:
			return true
		}
	}
	return CanTransition(from, to)
}

// RequiresAcceptance reports whether reaching this stage needs someone other
// than the owner to say so.
//
// Launch is self-evident from the engineering record — a merged pull request,
// a tag — so it does not. Adoption is a claim about the world outside the
// repository, and nothing in the repository can settle it.
func RequiresAcceptance(t MilestoneType) bool {
	return t == MilestoneFirstUse || t == MilestoneNthUse
}

// MilestoneInput is one milestone as a caller proposes it.
type MilestoneInput struct {
	Type           MilestoneType
	N              int
	Title          string
	PlannedDate    time.Time
	HasPlannedDate bool
	Adoption       AdoptionCheck
	HasVerifier    bool
}

// MinNthUse is the smallest N an nth_use milestone may carry. N=1 is what the
// first_use stage already means, so a second milestone claiming it would put
// two rows in front of a verifier for the same event.
const MinNthUse = 2

// ValidateMilestone applies the rules the database cannot express, and returns
// the first violation with the message the user will read.
func ValidateMilestone(in MilestoneInput) error {
	if !ValidMilestoneType(in.Type) {
		return invalidf("type must be launch, first_use or nth_use")
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return invalidf("title is required")
	}
	if len([]rune(title)) > MaxTitleLen {
		return invalidf("title must be at most %d characters", MaxTitleLen)
	}

	if in.Type == MilestoneNthUse {
		if in.N < MinNthUse {
			return invalidf("N must be at least %d; the first pass is the first_use milestone", MinNthUse)
		}
	} else if in.N != 0 {
		return invalidf("N applies to nth_use milestones only")
	}

	// A milestone without a date is a wish. The date is what lets the system
	// come back and ask, which is the only reason any of this is tracked.
	if !in.HasPlannedDate {
		return invalidf("a planned date is required")
	}

	if RequiresAcceptance(in.Type) {
		if !ValidAdoptionCheck(in.Adoption) {
			return invalidf("say how adoption will be decided: verifier, analytics or agent")
		}
		if in.Adoption == AdoptionVerifier && !in.HasVerifier {
			return invalidf("a verifier-checked milestone needs someone named to confirm it")
		}
	} else if in.Adoption != "" {
		return invalidf("an adoption check applies to first_use and nth_use milestones only")
	}

	return nil
}

// IsDelayed reports whether an unfinished milestone has passed its planned
// date, given the day the caller considers today.
//
// The clock is a parameter rather than a call to time.Now so the sweep that
// writes this flag and the tests that pin its edges see the same function.
// Comparison is by calendar day: planned dates carry no time of day, and a
// milestone due today is not late until today is over.
func IsDelayed(status MilestoneStatus, planned, today time.Time) bool {
	switch status {
	case MilestoneAchieved, MilestoneCancelled:
		return false
	}
	return dayOf(planned).Before(dayOf(today))
}

// OnTime reports whether an achieved milestone landed by the date it was FIRST
// given, not the date it was last moved to.
//
// Measuring against the current planned date would make every rescheduled
// milestone on time by construction, and the statistic would flatter exactly
// the plans that slipped most. Rescheduling stays free and un-blamed — it just
// does not rewrite history.
func OnTime(actual, originalPlanned time.Time) bool {
	return !dayOf(actual).After(dayOf(originalPlanned))
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// CanPropose reports whether an actor may propose moving a milestone to the
// given status.
//
// Only the two forward stages are proposable. Cancelling, reopening or pulling
// a milestone backwards are human edits with their own audit trail: they are
// judgements about whether the work should continue, and an agent has no
// standing to make them.
func CanPropose(from, to MilestoneStatus) bool {
	if to != MilestonePendingAccept && to != MilestoneAchieved {
		return false
	}
	return from != to && CanTransition(from, to)
}

// CanDecide reports whether an actor of this type may settle a pending
// proposal into the given state.
//
// Acceptance is reserved for a human — a member, or an external verifier
// confirming through a signed link. An agent may not accept, including its own
// proposal, and the restriction is structural rather than advisory: a
// milestone is a claim that something is genuinely done or genuinely used, and
// a system where the party doing the work also certifies the work has stopped
// measuring anything.
//
// Superseding is the exception, and is not a judgement: it is what happens to
// an older proposal when a newer one arrives for the same milestone, so the
// system itself records it.
func CanDecide(state ProposalState, decider ActorType) bool {
	switch state {
	case ProposalAccepted:
		return decider == ActorMember || decider == ActorExternal
	case ProposalRejected:
		return decider == ActorMember || decider == ActorExternal
	case ProposalSuperseded:
		return true
	}
	return false
}

// ProposalState is the life of one agent proposal.
type ProposalState string

const (
	ProposalPending    ProposalState = "pending"
	ProposalAccepted   ProposalState = "accepted"
	ProposalRejected   ProposalState = "rejected"
	ProposalSuperseded ProposalState = "superseded"
)

// ActorExternal is a verifier with no workspace seat, confirming through a
// signed single-use link. It appears as a decider and never as an author,
// which is why it lives here rather than beside ActorMember and ActorAgent.
const ActorExternal ActorType = "external"

// ProposalInput is one proposal as an agent submits it.
type ProposalInput struct {
	From     MilestoneStatus
	To       MilestoneStatus
	Evidence string
	HasDate  bool
}

// MinEvidenceLen is the shortest evidence this package will accept. A proposal
// is an assertion about the world that a human is being asked to trust; "done"
// is not one. The bound is low because the structured refs alongside the prose
// carry most of the weight — it exists to refuse the empty case, not to demand
// an essay.
const MinEvidenceLen = 12

// ValidateProposal applies the rules for an agent-submitted proposal.
func ValidateProposal(in ProposalInput) error {
	if !CanPropose(in.From, in.To) {
		return invalidf("a milestone at %s cannot be proposed as %s", in.From, in.To)
	}
	if len([]rune(strings.TrimSpace(in.Evidence))) < MinEvidenceLen {
		return invalidf("a proposal must say what it is based on")
	}
	if in.To == MilestoneAchieved && !in.HasDate {
		return invalidf("proposing a milestone as achieved requires the date it happened")
	}
	return nil
}
