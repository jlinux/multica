package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type guestActors struct {
	agent  db.Agent
	member db.Member
}

func (q guestActors) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) { return q.agent, nil }
func (q guestActors) GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return q.member, nil
}
func guestFixture(t *testing.T) (guestActors, engine.ResolvedInstallation, channel.InboundMessage, *channelaccess.WecomGuest) {
	t.Helper()
	var ws, agent, sponsor pgtype.UUID
	_ = ws.Scan("11111111-1111-1111-1111-111111111111")
	_ = agent.Scan("22222222-2222-2222-2222-222222222222")
	_ = sponsor.Scan("33333333-3333-3333-3333-333333333333")
	grant := channelaccess.WecomGrant{BotID: "bot", WorkspaceID: "11111111-1111-1111-1111-111111111111", AgentID: "22222222-2222-2222-2222-222222222222", SponsorUserID: "33333333-3333-3333-3333-333333333333", AllowedGroupIDs: []string{"group"}}
	raw, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(raw))
	return guestActors{agent: db.Agent{ID: agent, WorkspaceID: ws, OwnerID: sponsor}, member: db.Member{Role: "member"}}, engine.ResolvedInstallation{WorkspaceID: ws, AgentID: agent, Active: true, Platform: Installation{BotID: "bot"}}, channel.InboundMessage{Source: channel.Source{ChatID: "group", ChatType: channel.ChatTypeGroup, SenderID: "visitor"}}, grant.Snapshot("visitor", "group", "group")
}
func TestResolveWecomGuestScope(t *testing.T) {
	q, inst, msg, _ := guestFixture(t)
	id, err := resolveWecomGuest(context.Background(), q, inst, msg)
	if err != nil || id.UserID != q.agent.OwnerID || id.WecomGuest == nil || id.WecomGuest.SenderID != "visitor" {
		t.Fatalf("identity=%+v err=%v", id, err)
	}
	msg.Source.ChatID = "elsewhere"
	if _, err = resolveWecomGuest(context.Background(), q, inst, msg); !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("unauthorized group: %v", err)
	}
	msg.Source.ChatID = "group"
	inst.AgentID = inst.WorkspaceID
	if _, err = resolveWecomGuest(context.Background(), q, inst, msg); !errors.Is(err, channelaccess.ErrWecomAccessDenied) {
		t.Fatalf("installation mismatch must be an access denial: %v", err)
	}
}
func TestGuestRouteSeparatesIdentityAndGrant(t *testing.T) {
	_, _, msg, guest := guestFixture(t)
	memberKey, _, _ := wecomSessionRoute(msg.Source)
	key, config, err := wecomGuestSessionRoute(msg.Source, guest)
	if err != nil || key == memberKey {
		t.Fatalf("guest route %q %v", key, err)
	}
	stored, err := channelaccess.GuestFromConfig(config)
	if err != nil || stored == nil || *stored != *guest {
		t.Fatal("lost external identity")
	}
	target, err := wecomBindingChatID(db.ChannelChatSessionBinding{ChannelChatID: key, Config: config})
	if err != nil || target != "group" {
		t.Fatalf("transport target %q %v", target, err)
	}
	guest.GrantHash = "changed"
	other, _, err := wecomGuestSessionRoute(msg.Source, guest)
	if err == nil && other == key {
		t.Fatal("changed grant shares context")
	}
	if _, err := wecomBindingChatID(db.ChannelChatSessionBinding{ChannelChatID: key}); err == nil {
		t.Fatal("synthetic key sent as group ID")
	}
}
