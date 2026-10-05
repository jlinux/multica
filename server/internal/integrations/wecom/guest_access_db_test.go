package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	googleuuid "github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGuestAccessRealResolverAndIsolatedGroupDB(t *testing.T) {
	pool := mediaBindTestDB(t)
	ctx := context.Background()
	fx := dbfx.New(pool, "", "")
	suffix := googleuuid.NewString()
	fx.UserID = fx.User(t, "sponsor", "guest-sponsor-"+suffix+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "guest access", "guest-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "member")
	runtime := fx.Runtime(t, "guest runtime")
	agent := fx.Agent(t, "guest agent", runtime)
	bot := "bot-" + suffix
	config, _ := json.Marshal(map[string]string{"app_id": bot, "bot_id": bot, "app_secret_encrypted": ""})
	installation := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agent, "channel_type": "wecom", "config": config, "installer_user_id": fx.UserID, "status": "active"})
	grant := channelaccess.WecomGrant{BotID: bot, WorkspaceID: fx.WorkspaceID, AgentID: agent, SponsorUserID: fx.UserID, AllowedGroupIDs: []string{"group"}}
	policy, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(policy))
	q := db.New(pool)
	store := NewStore(q)
	session := engine.NewChatSession(q, pool, TypeWecom, engine.SessionTitles{})
	router := engine.NewRouter(bindTestIssues{}, &bindTestTasks{}, q, engine.RouterConfig{Logger: testLogger()})
	router.Register(TypeWecom, NewResolverSet(store, session, nil, nil))
	t.Cleanup(func() {
		router.Drain(ctx)
		// Only this fixture's rows are removed; no shared database truncation.
		for _, sql := range []string{
			`DELETE FROM channel_chat_context_generation WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`,
			`DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
			`DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`,
			`DELETE FROM chat_session WHERE workspace_id=$1`,
			`DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
			`DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
		} {
			if _, err := pool.Exec(ctx, sql, fx.WorkspaceID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	message := func(sender, text string) channel.InboundMessage {
		raw, _ := json.Marshal(InboundMessage{BotID: bot})
		return channel.InboundMessage{MessageID: googleuuid.NewString(), EventID: googleuuid.NewString(), Text: text, AddressedToBot: true, SkipAgentRun: true, Source: channel.Source{ChannelType: TypeWecom, ChatID: "group", ChatType: channel.ChatTypeGroup, SenderID: sender}, Raw: raw}
	}
	inst, err := (&installationResolver{store: store}).ResolveInstallation(ctx, message("alice", "question"))
	if err != nil {
		t.Fatal(err)
	}
	getRoute := func(sender string) db.ChannelChatSessionBinding {
		t.Helper()
		msg := message(sender, "unused")
		identity, err := (&identityResolver{store: store}).ResolveSender(ctx, inst, msg)
		if err != nil {
			t.Fatal(err)
		}
		if identity.WecomGuest == nil || util.UUIDToString(identity.UserID) != fx.UserID {
			t.Fatal("guest identity missing")
		}
		key, _, err := wecomGuestSessionRoute(msg.Source, identity.WecomGuest)
		if err != nil {
			t.Fatal(err)
		}
		row, err := q.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{InstallationID: inst.ID, ChannelChatID: key})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	send := func(sender, text string) {
		t.Helper()
		if err := router.Handle(ctx, message(sender, text)); err != nil {
			t.Fatalf("%s %s: %v", sender, text, err)
		}
	}
	inboundErrors := make(chan error, 2)
	for _, sender := range []string{"alice", "bob"} {
		go func(sender string) { inboundErrors <- router.Handle(ctx, message(sender, sender+" private question")) }(sender)
	}
	for range 2 {
		if err := <-inboundErrors; err != nil {
			t.Fatal(err)
		}
	}
	alice, bob := getRoute("alice"), getRoute("bob")
	if alice.ChatSessionID == bob.ChatSessionID {
		t.Fatal("two users share session")
	}
	for _, pair := range []struct {
		row  db.ChannelChatSessionBinding
		want string
	}{{alice, "alice private question"}, {bob, "bob private question"}} {
		var content string
		if err := pool.QueryRow(ctx, `SELECT string_agg(content,'|') FROM chat_message WHERE chat_session_id=$1`, pair.row.ChatSessionID).Scan(&content); err != nil {
			t.Fatal(err)
		}
		if content != pair.want {
			t.Fatalf("mixed history: %s", content)
		}
	}
	send("alice", "/clear")
	cleared, bobAfterClear := getRoute("alice"), getRoute("bob")
	if cleared.ChatSessionID != alice.ChatSessionID || cleared.ContextRevision <= alice.ContextRevision {
		t.Fatal("clear did not reset alice context")
	}
	if bobAfterClear.ChatSessionID != bob.ChatSessionID || bobAfterClear.ContextRevision != bob.ContextRevision {
		t.Fatal("alice clear changed bob context")
	}
	send("alice", "/new")
	next, bobAfterNew := getRoute("alice"), getRoute("bob")
	if next.ChatSessionID == alice.ChatSessionID || bobAfterNew.ChatSessionID != bob.ChatSessionID {
		t.Fatal("new rotated wrong user")
	}
	stored, err := channelaccess.GuestFromConfig(next.Config)
	if err != nil || stored == nil || stored.SenderID != "alice" {
		t.Fatalf("guest snapshot lost: %+v %v", stored, err)
	}

	// A bound member keeps their real identity despite the enabled guest policy.
	member := fx.User(t, "member", "guest-member-"+suffix+"@example.test")
	memberRow := fx.Member(t, fx.WorkspaceID, member, "member")
	fx.Insert(t, "channel_user_binding", dbfx.Cols{"workspace_id": fx.WorkspaceID, "multica_user_id": member, "installation_id": installation, "channel_type": "wecom", "channel_user_id": "bound"})
	id, err := (&identityResolver{store: store}).ResolveSender(ctx, inst, message("bound", "question"))
	if err != nil || id.WecomGuest != nil || util.UUIDToString(id.UserID) != member {
		t.Fatalf("member downgraded: %+v %v", id, err)
	}
	fx.Exec(t, `DELETE FROM member WHERE id=$1`, memberRow)
	if _, err := (&identityResolver{store: store}).ResolveSender(ctx, inst, message("bound", "question")); !errors.Is(err, engine.ErrSenderNotMember) {
		t.Fatalf("removed member downgraded to guest: %v", err)
	}
	// No member identities were provisioned for either external sender.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_user_binding WHERE installation_id=$1 AND channel_user_id IN ('alice','bob')`, installation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("guest created a member binding")
	}

	// Revoked guest access must not tear down the shared bot connection.
	fx.Exec(t, "DELETE FROM member WHERE workspace_id=$1 AND user_id=$2", fx.WorkspaceID, fx.UserID)
	deniedMessage := message("alice", "question after sponsor removal")
	if err := router.Handle(ctx, deniedMessage); err != nil {
		t.Fatalf("denial failed connector: %v", err)
	}
	if _, err := (&deduper{store: store}).Claim(ctx, inst.ID, deniedMessage.MessageID); !errors.Is(err, engine.ErrDuplicate) {
		t.Fatalf("denied callback not finalized: %v", err)
	}
	var deniedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_audit WHERE installation_id=$1 AND drop_reason='guest_access_denied'`, installation).Scan(&deniedCount); err != nil || deniedCount != 1 {
		t.Fatalf("denial audit: %d %v", deniedCount, err)
	}
}
