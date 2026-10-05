package channelaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type actorFake struct {
	agent     db.Agent
	member    db.Member
	err       error
	memberErr error
}

func (f actorFake) GetChannelInstallationByAppID(context.Context, db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{WorkspaceID: actorUUID("11111111-1111-1111-1111-111111111111"), AgentID: actorUUID("22222222-2222-2222-2222-222222222222"), ChannelType: "wecom", Status: "active", Config: []byte(`{"bot_id":"bot"}`)}, nil
}
func (f actorFake) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) { return f.agent, f.err }
func (f actorFake) GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return f.member, f.memberErr
}
func actorUUID(s string) pgtype.UUID { var u pgtype.UUID; _ = u.Scan(s); return u }
func TestValidateWecomActor(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	g, _ := LookupWecom("bot")
	s := g.Snapshot("sender", "group", "group")
	f := actorFake{agent: db.Agent{ID: actorUUID(g.AgentID), WorkspaceID: actorUUID(g.WorkspaceID), OwnerID: actorUUID(g.SponsorUserID)}, member: db.Member{Role: "member"}}
	for _, role := range []string{"member", "admin", "owner"} {
		t.Run(role, func(t *testing.T) {
			actor := f
			actor.member.Role = role
			if err := ValidateWecomActor(context.Background(), actor, s); err != nil {
				t.Fatalf("configured agent owner rejected: %v", err)
			}
		})
	}
	wrongOwner := f.agent
	wrongOwner.OwnerID = actorUUID(g.WorkspaceID)
	wrongWorkspace := f.agent
	wrongWorkspace.WorkspaceID = actorUUID(g.AgentID)
	archived := f.agent
	archived.ArchivedAt.Valid = true
	for _, tc := range []struct {
		name  string
		actor actorFake
	}{
		{"removed member", actorFake{agent: f.agent, memberErr: pgx.ErrNoRows}},
		{"different owner", actorFake{agent: wrongOwner, member: db.Member{Role: "admin"}}},
		{"different workspace", actorFake{agent: wrongWorkspace, member: f.member}},
		{"archived agent", actorFake{agent: archived, member: f.member}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateWecomActor(context.Background(), tc.actor, s); !errors.Is(err, ErrWecomAccessDenied) {
				t.Fatalf("expected access denial: %v", err)
			}
		})
	}
	failure := errors.New("database unavailable")
	if err := ValidateWecomActor(context.Background(), actorFake{agent: f.agent, memberErr: failure}, s); !errors.Is(err, failure) || errors.Is(err, ErrWecomAccessDenied) {
		t.Fatalf("membership lookup failure must remain retryable: %v", err)
	}
	t.Setenv(WecomEnv, "")
	if err := ValidateWecomActor(context.Background(), f, s); !errors.Is(err, ErrWecomAccessDenied) {
		t.Fatalf("member cannot grant access without operator configuration: %v", err)
	}
}

func TestWecomActorErrorsDistinguishRevocationFromDatabaseFailure(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	g, _ := LookupWecom("bot")
	snapshot := g.Snapshot("sender", "group", "group")
	for _, tc := range []struct {
		err    error
		denied bool
	}{
		{pgx.ErrNoRows, true}, {context.DeadlineExceeded, false},
	} {
		err := ValidateWecomActor(context.Background(), actorFake{err: tc.err}, snapshot)
		if errors.Is(err, ErrWecomAccessDenied) != tc.denied {
			t.Fatalf("wrong error classification for %v: %v", tc.err, err)
		}
		if !tc.denied && !errors.Is(err, tc.err) {
			t.Fatalf("lost database failure: %v", err)
		}
	}
}
