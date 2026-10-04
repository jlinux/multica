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
	agent  db.Agent
	member db.Member
	err    error
}

func (f actorFake) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) { return f.agent, f.err }
func (f actorFake) GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return f.member, f.err
}
func actorUUID(s string) pgtype.UUID { var u pgtype.UUID; _ = u.Scan(s); return u }
func TestValidateWecomActor(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	g, _ := LookupWecom("bot")
	s := g.Snapshot("sender", "group", "group")
	f := actorFake{agent: db.Agent{ID: actorUUID(g.AgentID), WorkspaceID: actorUUID(g.WorkspaceID), OwnerID: actorUUID(g.SponsorUserID)}, member: db.Member{Role: "admin"}}
	if err := ValidateWecomActor(context.Background(), f, s); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []actorFake{
		{agent: f.agent, member: db.Member{Role: "member"}},
		{agent: db.Agent{}, member: f.member},
		{err: errors.New("database unavailable")},
	} {
		if ValidateWecomActor(context.Background(), bad, s) == nil {
			t.Fatal("invalid grant actor accepted")
		}
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
