package channelaccess

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type WecomActorQueries interface {
	GetAgent(context.Context, pgtype.UUID) (db.Agent, error)
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

func ValidateWecomActor(ctx context.Context, q WecomActorQueries, s *WecomGuest) error {
	if err := ValidateWecomSnapshot(s); err != nil {
		return err
	}
	var agentID, workspaceID, sponsorID pgtype.UUID
	if agentID.Scan(s.AgentID) != nil || workspaceID.Scan(s.WorkspaceID) != nil || sponsorID.Scan(s.SponsorUserID) != nil {
		return denied("invalid WeCom guest actor")
	}
	agent, err := q.GetAgent(ctx, agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied("WeCom guest actor no longer exists")
	}
	if err != nil {
		return err
	}
	if agent.WorkspaceID != workspaceID || agent.OwnerID != sponsorID || agent.ArchivedAt.Valid {
		return denied("WeCom guest sponsor must own the active agent")
	}
	// The server operator grants access through WecomEnv. The sponsor is the
	// execution identity, not the administrator configuring that grant; retain
	// ownership and workspace membership without elevating their workspace role.
	_, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: sponsorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return denied("WeCom guest actor no longer exists")
	}
	if err != nil {
		return err
	}
	return nil
}

type wecomGuestContextKey struct{}

// WithWecomGuest is only used after server-side identity resolution.
func WithWecomGuest(ctx context.Context, s *WecomGuest) context.Context {
	return context.WithValue(ctx, wecomGuestContextKey{}, s)
}
func WecomGuestFromContext(ctx context.Context) *WecomGuest {
	s, _ := ctx.Value(wecomGuestContextKey{}).(*WecomGuest)
	return s
}
