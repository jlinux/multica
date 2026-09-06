-- An agent proposing that a milestone has been reached, and a human deciding.
--
-- This table is the whole reason the goal layer can survive contact with a real
-- team. Every product-management tool that came before it fails the same way:
-- the person who pays the data-entry cost is not the person who gets the
-- benefit, so within a quarter the data is stale and the dashboards are
-- theatre. The one thing Multica has that those tools do not is a fleet of
-- agents already doing the work and already holding the evidence — so the
-- entry cost moves to them.
--
-- What an agent may do is bounded on purpose. It PROPOSES, with evidence; a
-- human ACCEPTS. There is no configuration that lets a proposal apply itself,
-- and that is not timidity: a milestone is a claim that something is genuinely done or
-- truly used, and a system that lets the party doing the work also certify the
-- work is one that stops meaning anything.
--
-- source_task_id / source_issue_id are what make the claim auditable. They say
-- which agent run, against which issue, produced this proposal — so a reviewer
-- can open the execution log, the diff and the tokens spent behind a one-line
-- assertion that a feature is live.
--
-- No foreign keys by house rule. A proposal whose task or issue was deleted
-- keeps its evidence text, which is the part a reviewer actually reads.
CREATE TABLE milestone_proposal (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    milestone_id UUID NOT NULL,

    -- Only forward moves are proposable. Nothing here can cancel a milestone or
    -- pull it backwards; those are human edits with their own audit.
    proposed_status TEXT NOT NULL CHECK (proposed_status IN ('pending_accept', 'achieved')),
    proposed_actual_date DATE,

    -- Why the agent believes this. Prose a reviewer reads, plus structured
    -- pointers (PR ids, log queries, ticket ids, analytics event counts) it can
    -- click. A proposal with no evidence is a guess and is refused upstream.
    evidence TEXT NOT NULL DEFAULT '',
    evidence_refs JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(evidence_refs) = 'array'),

    proposed_by_type TEXT NOT NULL CHECK (proposed_by_type IN ('member', 'agent')),
    proposed_by_id UUID NOT NULL,

    -- The provenance chain back into execution: which run, on which issue.
    source_task_id UUID,
    source_issue_id UUID,

    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'accepted', 'rejected', 'superseded')),
    decided_by_type TEXT CHECK (decided_by_type IN ('member', 'agent', 'external')),
    decided_by_id UUID,
    decided_at TIMESTAMPTZ,
    decide_note TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT milestone_proposal_decision_pair
        CHECK ((state = 'pending') = (decided_at IS NULL)),
    CONSTRAINT milestone_proposal_decider_pair
        CHECK ((decided_at IS NULL) = (decided_by_id IS NULL)),
    CONSTRAINT milestone_proposal_achieved_has_date
        CHECK (proposed_status <> 'achieved' OR proposed_actual_date IS NOT NULL)
);

COMMENT ON TABLE milestone_proposal IS
    'An agent proposes a milestone has been reached and attaches evidence; a human accepts or rejects. Proposals never apply themselves — the party doing the work does not certify the work. This is what moves the data-entry cost of the goal layer off the humans.';
COMMENT ON COLUMN milestone_proposal.source_task_id IS
    'The agent run that produced this proposal. Makes the claim auditable: a reviewer can open the execution log, diff and token cost behind the assertion. No FK; evidence text survives a deleted task.';
COMMENT ON COLUMN milestone_proposal.evidence_refs IS
    'Structured pointers backing the proposal: pull request ids, ticket ids, log queries, analytics event counts. Rendered as links next to the prose in evidence.';
