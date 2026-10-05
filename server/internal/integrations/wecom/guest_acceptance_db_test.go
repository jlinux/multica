package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	googleuuid "github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Drives the actual callback worker and router against PostgreSQL. Only the
// WeCom transport and agent execution are simulated; no real bot or CLI runs.
func TestGuestAcceptancePersistentConnectionDB(t *testing.T) {
	pool := mediaBindTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fx := dbfx.New(pool, "", "")
	suffix := googleuuid.NewString()
	fx.UserID = fx.User(t, "acceptance sponsor", "acceptance-"+suffix+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "acceptance", "acceptance-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "member")
	runtime := fx.Runtime(t, "acceptance runtime")
	agent := fx.Agent(t, "acceptance agent", runtime)
	bot := "acceptance-bot-" + suffix
	cfg, _ := json.Marshal(map[string]string{"app_id": bot, "bot_id": bot, "app_secret_encrypted": ""})
	installation := fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agent, "channel_type": "wecom", "config": cfg, "installer_user_id": fx.UserID, "status": "active"})
	boundUser := fx.User(t, "bound member", "bound-"+suffix+"@example.test")
	fx.Member(t, fx.WorkspaceID, boundUser, "member")
	fx.Insert(t, "channel_user_binding", dbfx.Cols{"workspace_id": fx.WorkspaceID, "multica_user_id": boundUser, "installation_id": installation, "channel_type": "wecom", "channel_user_id": "bound"})
	grant := channelaccess.WecomGrant{BotID: bot, WorkspaceID: fx.WorkspaceID, AgentID: agent, SponsorUserID: fx.UserID, AllowedGroupIDs: []string{"allowed-group"}}
	raw, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(raw))
	q := db.New(pool)
	router := engine.NewRouter(bindTestIssues{}, &bindTestTasks{}, q, engine.RouterConfig{Logger: testLogger()})
	router.Register(TypeWecom, NewResolverSet(NewStore(q), engine.NewChatSession(q, pool, TypeWecom, engine.SessionTitles{}), nil, nil))
	t.Cleanup(func() {
		router.Drain(context.Background())
		for _, sql := range []string{
			`DELETE FROM channel_chat_context_generation WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`,
			`DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
			`DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id=$1)`,
			`DELETE FROM chat_session WHERE workspace_id=$1`,
			`DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
			`DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id=$1)`,
		} {
			if _, err := pool.Exec(context.Background(), sql, fx.WorkspaceID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	turns := []struct{ sender, chat, kind, text string }{
		{"alice", "allowed-group", "group", "A-731"},
		{"bob", "allowed-group", "group", "B-926"},
		{"alice", "blocked-group", "group", "not allowed group"},
		{"alice", "private-chat", "single", "private not allowed"},
		{"alice", "allowed-group", "group", "/issue do not create"},
		{"alice", "allowed-group", "group", "after removal"},
		{"bound", "allowed-group", "group", "member still works"},
	}
	frames := make([][]byte, 0, len(turns))
	for _, turn := range turns {
		mc := aibotMsgCallback{MsgID: googleuuid.NewString(), ChatID: turn.chat, ChatType: turn.kind, MsgType: "text"}
		mc.From.UserID = turn.sender
		mc.Text.Content = turn.text
		body, err := json.Marshal(mc)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := json.Marshal(frameEnvelope{Cmd: cmdMsgCallback, Headers: frameHeaders{ReqID: mc.MsgID}, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	conn := &floodConn{frames: frames, delivered: make(chan struct{}), unblock: make(chan struct{})}
	handled := make(chan string, len(turns))
	c := &wecomChannel{installationID: util.MustParseUUID(installation), botID: bot, secret: "local-test-only", dialer: scriptedDialer{conn: conn}, wsURL: "wss://example.test/local-acceptance", senders: newSendersRegistry(), handler: func(ctx context.Context, msg channel.InboundMessage) error {
		if msg.Text == "after removal" {
			if _, err := pool.Exec(ctx, "DELETE FROM member WHERE workspace_id=$1 AND user_id=$2", fx.WorkspaceID, fx.UserID); err != nil {
				return fmt.Errorf("remove test sponsor: %w", err)
			}
		}
		msg.SkipAgentRun = true
		if err := router.Handle(ctx, msg); err != nil {
			return err
		}
		handled <- msg.Text
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- c.Connect(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("connector did not stop")
		}
	}()
	for _, turn := range turns {
		select {
		case text := <-handled:
			if text != turn.text {
				t.Fatalf("unexpected callback order: %q", text)
			}
		case err := <-done:
			done <- err
			t.Fatalf("connection stopped before %q: %v", turn.text, err)
		case <-ctx.Done():
			t.Fatalf("callback not handled: %q", turn.text)
		}
	}
	select {
	case <-conn.unblock:
		t.Fatal("authorization denial closed shared connection")
	default:
	}
	rows, err := pool.Query(ctx, `SELECT b.config->>'sender_id', string_agg(m.content,'|' ORDER BY m.created_at) FROM channel_chat_session_binding b JOIN chat_session s ON s.id=b.chat_session_id JOIN chat_message m ON m.chat_session_id=s.id WHERE b.installation_id=$1 GROUP BY b.id`, installation)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	histories := map[string]string{}
	for rows.Next() {
		var sender, content string
		if err := rows.Scan(&sender, &content); err != nil {
			t.Fatal(err)
		}
		histories[sender] = content
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(histories) != 3 || histories["alice"] != "A-731" || histories["bob"] != "B-926" || histories["bound"] != "member still works" {
		t.Fatalf("isolated histories: %+v", histories)
	}
	if got := fx.Count(t, `SELECT count(*) FROM channel_inbound_audit WHERE installation_id=$1 AND drop_reason='guest_access_denied'`, installation); got != 1 {
		t.Fatalf("denial audits: %d", got)
	}
	if got := fx.Count(t, `SELECT count(*) FROM channel_inbound_audit WHERE installation_id=$1 AND drop_reason='unbound_user'`, installation); got != 2 {
		t.Fatalf("unbound audits: %d", got)
	}
	if got := fx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id=$1`, fx.WorkspaceID); got != 0 {
		t.Fatalf("guest command created %d issues", got)
	}
	if got := fx.Count(t, `SELECT count(*) FROM channel_inbound_message_dedup WHERE installation_id=$1`, installation); got != len(turns) {
		t.Fatalf("dedup callbacks: %d", got)
	}
	t.Log("PASS: raw callbacks, isolated A/B histories, group/private access denied, guest issue blocked, removed sponsor denied, bound member continues on same connection")
}
