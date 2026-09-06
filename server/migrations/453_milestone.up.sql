-- A goal's life does not end at launch.
--
-- Three milestone types, in order: launch (the thing is available),
-- first_use (someone ran a real end-to-end pass with it), nth_use (it is
-- still being used, N times in). Shipping is the FIRST of the three, not the
-- last. A goal that reaches launch and never reaches first_use is the case
-- this whole table exists to make visible; every tool that stops at "merged"
-- renders that case as a finished green bar.
--
-- No foreign keys by house rule. goal_id, verifier_id and accepted_by_id are
-- re-validated in application code, and deleting a goal deletes its milestones
-- explicitly, inside the same transaction (internal/goal documents the order).
CREATE TABLE milestone (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    goal_id UUID NOT NULL,

    type TEXT NOT NULL CHECK (type IN ('launch', 'first_use', 'nth_use')),
    -- Which N this nth_use verifies. A goal may carry several (N=3, N=10) to
    -- test adoption at increasing depth. Starts at 2 because N=1 is first_use.
    n INTEGER CHECK (n IS NULL OR n >= 2),

    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    -- What this delivery is worth, in the language of the people who will use
    -- it. Required at creation for a reason: a milestone nobody can state the
    -- value of is a date, not a milestone.
    value_statement TEXT NOT NULL DEFAULT '',

    -- The FIRST date ever planned, written once and never rewritten. On-time
    -- attainment is measured against this, so rescheduling cannot quietly
    -- launder a slip into a hit. It is a team-and-period statistic only; see
    -- the reporting rules in internal/goal.
    original_planned_date DATE NOT NULL,
    planned_date DATE NOT NULL,
    actual_date DATE,

    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'in_progress', 'pending_accept', 'achieved', 'cancelled')),
    -- Overlay flag, not a status: a milestone can be in_progress AND late.
    is_delayed BOOLEAN NOT NULL DEFAULT false,

    -- How "it was really used" gets decided, chosen when the milestone is
    -- created and never left to a verbal understanding:
    --   verifier  a named person confirms once
    --   analytics an event threshold in the product's own telemetry
    --   agent     an agent checks logs/tickets/data and reports evidence
    -- Two people in one workspace can otherwise mean things ten times apart by
    -- "used once", and the adoption rate stops being comparable.
    adoption_check TEXT CHECK (adoption_check IN ('verifier', 'analytics', 'agent')),
    adoption_config JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(adoption_config) = 'object'),

    -- 'external' is a verifier with no workspace seat — the customer, the
    -- teacher, the ops lead who will never have an account. verifier_label
    -- carries their display name in that case; confirmation arrives through a
    -- signed single-use link rather than a login.
    verifier_type TEXT CHECK (verifier_type IN ('member', 'agent', 'external')),
    verifier_id UUID,
    verifier_label TEXT NOT NULL DEFAULT '',

    accepted_by_type TEXT CHECK (accepted_by_type IN ('member', 'agent', 'external')),
    accepted_by_id UUID,
    accept_note TEXT NOT NULL DEFAULT '',
    accepted_at TIMESTAMPTZ,

    is_retro BOOLEAN NOT NULL DEFAULT false,

    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,

    CONSTRAINT milestone_n_matches_type CHECK ((type = 'nth_use') = (n IS NOT NULL)),
    -- A launch date is self-evident; an adoption claim is not. The two
    -- adoption types must say up front how they will be decided.
    CONSTRAINT milestone_adoption_check_required
        CHECK (type = 'launch' OR adoption_check IS NOT NULL),
    CONSTRAINT milestone_achieved_has_date
        CHECK (status <> 'achieved' OR actual_date IS NOT NULL),
    CONSTRAINT milestone_accepted_pair
        CHECK ((accepted_at IS NULL) = (accepted_by_id IS NULL))
);

COMMENT ON TABLE milestone IS
    'Launch -> first real use -> continued use. Launch is the first of three, not the finish line; a goal stuck at launch is the signal this table exists to surface.';
COMMENT ON COLUMN milestone.original_planned_date IS
    'First date ever planned, never rewritten. On-time attainment is measured against this so a reschedule cannot turn a slip into a hit. Aggregate reporting only, never per person.';
COMMENT ON COLUMN milestone.adoption_check IS
    'verifier | analytics | agent — how "really used" is decided, fixed at creation. Required for first_use and nth_use so adoption stays comparable across owners.';
COMMENT ON COLUMN milestone.verifier_type IS
    'member | agent | external. external is a verifier with no seat, confirming through a signed single-use link; verifier_label holds their name.';
