-- What actually shipped for a milestone, in engineering terms.
--
-- A milestone is reached by one or more releases, not one: a main release
-- followed by two hotfixes is the normal shape, and collapsing them into a
-- single tag field loses exactly the history a release review needs.
--
-- Two paths coexist on purpose, and this is not a compatibility shim — both
-- are permanent product surfaces:
--
--   manual     repo_url / ref / tag / released_at typed in. Costs nothing to
--              adopt, works for a team with no VCS connection configured, and
--              is the only option for a release that happened somewhere this
--              workspace does not integrate with.
--   linked     pull_request_id + pull_request_source point at a row this
--              server already has. Multica stores pull requests in two tables
--              — github_pull_request (GitHub App) and vcs_pull_request
--              (forgejo/gitea/gitlab) — which is why the source discriminator
--              exists rather than a single id.
--
-- The linked path is what closes the loop with agent work: an agent finishes
-- an issue, the issue already carries its pull request through
-- issue_pull_request / issue_vcs_pull_request, and that same PR row is what
-- gets attached here when the milestone is proposed (migration 456). Nothing
-- has to be retyped for the engineering fact and the delivery record to agree.
--
-- No foreign keys by house rule; a deleted PR row leaves the manual columns as
-- the surviving record, which is why they are kept even on a linked release.
CREATE TABLE milestone_release (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    milestone_id UUID NOT NULL,

    kind TEXT NOT NULL CHECK (kind IN ('main', 'hotfix', 'pending')),

    repo_url TEXT NOT NULL DEFAULT '',
    ref TEXT NOT NULL DEFAULT '',
    tag TEXT NOT NULL DEFAULT '',
    released_at DATE,

    pull_request_id UUID,
    pull_request_source TEXT CHECK (pull_request_source IN ('github', 'vcs')),

    -- Release notes written for the people who will use the thing, not for the
    -- people who wrote it. Normally drafted by an agent from the commit and PR
    -- range and then edited by a human, which is why authorship is recorded
    -- polymorphically instead of assumed to be a member.
    summary TEXT NOT NULL DEFAULT '',
    summary_by_type TEXT CHECK (summary_by_type IN ('member', 'agent')),
    summary_by_id UUID,
    summary_at TIMESTAMPTZ,

    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT milestone_release_pr_pair
        CHECK ((pull_request_id IS NULL) = (pull_request_source IS NULL)),
    -- A release row that names neither a pull request nor a repository records
    -- nothing; refuse it rather than let the list fill with blanks.
    CONSTRAINT milestone_release_identifies_something
        CHECK (pull_request_id IS NOT NULL OR repo_url <> '' OR tag <> ''),
    CONSTRAINT milestone_release_summary_pair
        CHECK ((summary_at IS NULL) = (summary_by_id IS NULL))
);

COMMENT ON TABLE milestone_release IS
    'Releases delivering a milestone: main plus any hotfixes. Either typed in manually or linked to an existing github_pull_request / vcs_pull_request row, which is how agent-produced PRs reach the delivery record without retyping.';
COMMENT ON COLUMN milestone_release.pull_request_source IS
    'github | vcs — which table pull_request_id points into. Multica stores GitHub App PRs and forgejo/gitea/gitlab PRs separately, so the id alone is ambiguous.';
COMMENT ON COLUMN milestone_release.summary IS
    'Release notes in the language of the people who will use the feature. Usually drafted by an agent from the PR range, then edited by a human before publishing.';
