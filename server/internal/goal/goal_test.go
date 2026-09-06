package goal

import (
	"errors"
	"strings"
	"testing"
)

// The alignment matrix is the whole readability contract of the panorama, so
// it is asserted exhaustively rather than by example: every ordered pair of
// tiers, including the invalid ones, has a stated answer here.
func TestCanAlign(t *testing.T) {
	levels := []Level{LevelDirection, LevelProduct, LevelCycle}
	want := map[[2]Level]bool{
		{LevelProduct, LevelDirection}: true,
		{LevelCycle, LevelProduct}:     true,
	}
	for _, child := range levels {
		for _, parent := range levels {
			got := CanAlign(child, parent)
			if got != want[[2]Level{child, parent}] {
				t.Errorf("CanAlign(%s -> %s) = %v, want %v", child, parent, got, !got)
			}
		}
	}
	// The refusal that matters most: a cycle goal may not hang straight off a
	// direction. Callers are expected to add the intermediate product goal.
	if CanAlign(LevelCycle, LevelDirection) {
		t.Error("a cycle goal must not align directly to a direction; the intermediate product goal is what keeps the map readable")
	}
}

func TestCanContinue(t *testing.T) {
	if !CanContinue(LevelCycle, LevelCycle) {
		t.Error("a cycle goal must be able to continue another cycle goal (2.0 -> 2.1)")
	}
	for _, tc := range [][2]Level{
		{LevelProduct, LevelProduct},
		{LevelDirection, LevelDirection},
		{LevelCycle, LevelProduct},
		{LevelProduct, LevelCycle},
	} {
		if CanContinue(tc[0], tc[1]) {
			t.Errorf("CanContinue(%s, %s) = true, want false: continuation exists only between cycle goals", tc[0], tc[1])
		}
	}
}

func TestValidate(t *testing.T) {
	base := func() Input {
		return Input{Level: LevelCycle, Title: "Console 2.1", HasParent: true, ParentLevel: LevelProduct}
	}

	tests := []struct {
		name    string
		mutate  func(*Input)
		wantErr string // substring; empty means the input must be accepted
	}{
		{name: "aligned cycle goal", mutate: func(*Input) {}},
		{
			name:   "product goal needs a kind",
			mutate: func(in *Input) { *in = Input{Level: LevelProduct, Title: "Console rebuild", HasParent: true, ParentLevel: LevelDirection} },
			// Defaulting this instead of demanding it would quietly collect
			// every un-thought-about goal into one bucket.
			wantErr: "base (sustain) or brk (explore)",
		},
		{
			name: "product goal with a kind and a project",
			mutate: func(in *Input) {
				*in = Input{
					Level: LevelProduct, Title: "Console rebuild", Kind: KindBase,
					HasParent: true, ParentLevel: LevelDirection, HasProject: true,
				}
			},
		},
		{
			name: "product goal without a project",
			mutate: func(in *Input) {
				*in = Input{
					Level: LevelProduct, Title: "Console rebuild", Kind: KindBase,
					HasParent: true, ParentLevel: LevelDirection,
				}
			},
			wantErr: "must name the project",
		},
		{
			name:    "kind outside level 2",
			mutate:  func(in *Input) { in.Kind = KindBreakthru },
			wantErr: "product goals only",
		},
		{
			name:    "direction cannot align",
			mutate:  func(in *Input) { *in = Input{Level: LevelDirection, Title: "Five minute integration", HasParent: true, ParentLevel: LevelDirection} },
			wantErr: "nothing above it",
		},
		{
			name:    "tier skip is refused",
			mutate:  func(in *Input) { in.ParentLevel = LevelDirection },
			wantErr: "may only align to a product goal",
		},
		{
			name:    "unaligned goal must say why",
			mutate:  func(in *Input) { in.HasParent = false },
			wantErr: "why it stands alone",
		},
		{
			name: "unaligned goal with a reason is fine",
			mutate: func(in *Input) {
				in.HasParent = false
				in.OrphanReason = "One-off request from the annual event; no lasting product line."
			},
		},
		{
			name: "a direction needs no reason to stand alone",
			mutate: func(in *Input) {
				*in = Input{Level: LevelDirection, Title: "Five minute integration"}
			},
		},
		{
			name:    "orphan reason is bounded",
			mutate:  func(in *Input) { in.HasParent = false; in.OrphanReason = strings.Repeat("x", MaxOrphanReasonLen+1) },
			wantErr: "at most 500 characters",
		},
		{name: "blank title", mutate: func(in *Input) { in.Title = "   " }, wantErr: "title is required"},
		{name: "long title", mutate: func(in *Input) { in.Title = strings.Repeat("x", MaxTitleLen+1) }, wantErr: "at most 200 characters"},
		{name: "unknown level", mutate: func(in *Input) { in.Level = 4 }, wantErr: "level must be"},
		{
			name:    "continuation is cycle-only",
			mutate:  func(in *Input) { in.HasPrev = true; in.PrevLevel = LevelProduct },
			wantErr: "only a cycle goal may continue",
		},
		{
			name:   "an agent may own a goal",
			mutate: func(in *Input) { in.HasOwner = true; in.OwnerType = ActorAgent },
		},
		{
			name:    "owner must be a known actor",
			mutate:  func(in *Input) { in.HasOwner = true; in.OwnerType = "squad" },
			wantErr: "member or an agent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			err := Validate(in)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%+v) = %v, want nil", in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want error containing %q", in, tc.wantErr)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("error does not wrap ErrInvalid, so the handler cannot map it to 400: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// A goal is a planning unit and never work an agent can be handed. The
// structural guarantee is that goals live in their own table, but this asserts
// the second lock so a future union-based assignable list cannot quietly
// reintroduce the leak.
func TestGoalIsNeverAssignable(t *testing.T) {
	if Assignable() {
		t.Fatal("a goal must never be assignable: an agent handed a three-year direction would spend real tokens on it")
	}
}
