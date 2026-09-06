// Package goal holds the tier and alignment rules of the goal layer.
//
// Everything here is pure: no database handle, no context, no clock beyond
// what a caller passes in. The rules are the part of this feature that is
// easiest to get subtly wrong and hardest to notice — a goal aligned one tier
// too far up still renders, it just quietly makes the panorama unreadable —
// so they are separated from storage in order to be exhaustively unit-tested
// without a Postgres.
//
// The database enforces the invariants that need no lookup (see migration
// 451). This package enforces the ones that do, and owns every message a user
// reads when a rule refuses their edit.
package goal

import (
	"errors"
	"fmt"
	"strings"
)

// Level is the goal tier. It is fixed when a goal is created: parents and
// children were chosen under the tier's rules, so changing it would silently
// invalidate both sides. Archiving and recreating is the supported path.
type Level int16

const (
	// LevelDirection is where the organisation is going, on a one-to-three
	// year horizon. Few, stable, and never executable.
	LevelDirection Level = 1
	// LevelProduct is what a product bets on over six to twelve months. It is
	// the tier that binds to a project.
	LevelProduct Level = 2
	// LevelCycle is what a team commits to deliver in two weeks to three
	// months. Milestones hang here, and so does the seam to issues.
	LevelCycle Level = 3
)

// Kind splits product goals into maintaining what exists and exploring what
// does not. It is deliberately a forced binary on level 2 only: the entire
// point is to make a portfolio that is all exploration, or all maintenance,
// visible as a shape rather than a feeling.
type Kind string

const (
	KindBase      Kind = "base"
	KindBreakthru Kind = "brk"
)

// Status values mirror the goal.status CHECK in migration 451.
type Status string

const (
	StatusNotStarted Status = "not_started"
	StatusInProgress Status = "in_progress"
	StatusAtRisk     Status = "at_risk"
	StatusDone       Status = "done"
	StatusArchived   Status = "archived"
)

// ActorType is the polymorphic actor discriminator used across this repository
// for assignees, leads and authors.
type ActorType string

const (
	ActorMember ActorType = "member"
	ActorAgent  ActorType = "agent"
)

// ErrInvalid is the sentinel every rule violation here wraps, so a handler can
// map the whole package to 400 with one errors.Is.
var ErrInvalid = errors.New("invalid goal")

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ValidLevel reports whether v names a tier this package knows.
func ValidLevel(v int16) bool {
	switch Level(v) {
	case LevelDirection, LevelProduct, LevelCycle:
		return true
	}
	return false
}

// String renders a tier the way the product names it, for error messages.
func (l Level) String() string {
	switch l {
	case LevelDirection:
		return "direction"
	case LevelProduct:
		return "product goal"
	case LevelCycle:
		return "cycle goal"
	}
	return fmt.Sprintf("level(%d)", int16(l))
}

// CanAlign reports whether a goal at child may align to a goal at parent.
//
// A goal may only align exactly one tier up. Skipping a tier is refused rather
// than tolerated: the map's readability is the feature, and a cycle goal
// hanging directly off a three-year direction is precisely the shape that
// makes a goal tree stop being worth opening. A team that genuinely needs the
// link adds the intermediate product goal, which is the conversation the
// refusal is trying to force.
func CanAlign(child, parent Level) bool {
	return child == parent+1 && ValidLevel(int16(child)) && ValidLevel(int16(parent))
}

// CanContinue reports whether prev may be the predecessor of a goal at level.
//
// Continuation expresses version lineage (2.0 -> 2.1) and exists only between
// cycle goals. It is independent of alignment: 2.1 may continue 2.0 while
// aligning to a different product goal, or to none.
func CanContinue(level, prev Level) bool {
	return level == LevelCycle && prev == LevelCycle
}

// Input is one goal as a caller proposes it, before storage.
//
// ParentLevel and PrevLevel are the tiers of the goals the caller named, which
// the caller has already loaded and workspace-checked. They are passed in
// rather than looked up here so this package stays free of a database, and so
// a handler cannot accidentally validate against a goal from another
// workspace: resolving the row is the caller's job, and it is the step where
// the workspace filter belongs.
type Input struct {
	Level        Level
	Title        string
	Kind         Kind
	HasParent    bool
	ParentLevel  Level
	OrphanReason string
	HasPrev      bool
	PrevLevel    Level
	HasProject   bool
	OwnerType    ActorType
	HasOwner     bool
}

// MaxTitleLen mirrors the char_length CHECK on goal.title.
const MaxTitleLen = 200

// MaxOrphanReasonLen bounds the one-line justification an unaligned goal
// carries. It is short on purpose: the field exists to make someone say why in
// a sentence at review time, not to hold a design document.
const MaxOrphanReasonLen = 500

// Validate applies every tier rule that the database cannot, and returns the
// first violation with the message the user will read.
//
// Order matters. Level is checked first because every later rule is expressed
// in terms of it, and an unknown tier would otherwise produce a confusing
// downstream complaint about kind or alignment.
func Validate(in Input) error {
	if !ValidLevel(int16(in.Level)) {
		return invalidf("level must be 1 (direction), 2 (product goal) or 3 (cycle goal)")
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return invalidf("title is required")
	}
	if len([]rune(title)) > MaxTitleLen {
		return invalidf("title must be at most %d characters", MaxTitleLen)
	}

	// Kind is the level-2 portfolio split and is meaningless elsewhere.
	// Requiring it, rather than defaulting it, is what keeps the base-versus-
	// breakthrough mix honest: a default would collect every un-thought-about
	// goal into one bucket and the shape would stop meaning anything.
	switch in.Level {
	case LevelProduct:
		if in.Kind != KindBase && in.Kind != KindBreakthru {
			return invalidf("a product goal must be either base (sustain) or brk (explore)")
		}
		// A product goal that names no product cannot be rolled up, cannot
		// appear on the timeline (which groups by product before it lays out
		// any dates), and cannot inherit anything. There is deliberately no
		// separate product entity: project is the product.
		if !in.HasProject {
			return invalidf("a product goal must name the project it belongs to")
		}
	default:
		if in.Kind != "" {
			return invalidf("kind applies to product goals only")
		}
	}

	if in.HasParent {
		if in.Level == LevelDirection {
			return invalidf("a direction has nothing above it to align to")
		}
		if !CanAlign(in.Level, in.ParentLevel) {
			return invalidf(
				"a %s may only align to a %s, not to a %s",
				in.Level, in.Level-1, in.ParentLevel,
			)
		}
	} else if in.Level != LevelDirection {
		// An unaligned goal is a legitimate, displayed category — one-off work
		// that should not be forced under a strategic parent just to have one.
		// The single sentence is the price of that legitimacy: it is what makes
		// the quarterly "adopt it, keep it, or archive it" pass possible.
		if strings.TrimSpace(in.OrphanReason) == "" {
			return invalidf("an unaligned goal must say in one line why it stands alone")
		}
		if len([]rune(in.OrphanReason)) > MaxOrphanReasonLen {
			return invalidf("the reason must be at most %d characters", MaxOrphanReasonLen)
		}
	}

	if in.HasPrev && !CanContinue(in.Level, in.PrevLevel) {
		return invalidf("only a cycle goal may continue another cycle goal")
	}

	if in.HasOwner && in.OwnerType != ActorMember && in.OwnerType != ActorAgent {
		return invalidf("owner must be a member or an agent")
	}

	return nil
}

// Assignable reports whether a goal may ever be handed to an agent as work.
//
// It is always false, at every tier, and it is a function rather than a
// comment so that the intent is greppable and testable rather than a habit
// that erodes. A goal is a planning unit: it has no executable body, no
// runtime, no diff to produce. The thing an agent works on is an issue linked
// through goal_issue, and an agent that owns a goal owns the record-keeping,
// not the delivery.
//
// The structural guarantee is the separate table (migration 451); this
// function is the second lock, for any future code path tempted to build an
// assignable list from a union.
func Assignable() bool { return false }
