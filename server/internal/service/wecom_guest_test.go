package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWecomGuestTaskAuthorization(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	base := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	user := base.User(t, "guest sponsor", "guest-"+suffix+"@example.test")
	ws := base.Workspace(t, "guest workspace", "guest-"+suffix)
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "admin")
	runtime := fx.Runtime(t, "guest-runtime")
	agent := fx.Agent(t, "guest-agent", runtime)
	grant := channelaccess.WecomGrant{BotID: "bot-" + suffix, WorkspaceID: ws, AgentID: agent, SponsorUserID: user, AllowedGroupIDs: []string{"group"}}
	raw, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(raw))
	guest := grant.Snapshot("external-user", "group", "group")
	config, _ := json.Marshal(map[string]any{"wecom_guest": guest, "chat_id": "group", "sender_id": "external-user"})
	installConfig, _ := json.Marshal(map[string]string{"bot_id": grant.BotID})
	installation := fx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": ws, "agent_id": agent, "channel_type": "wecom", "config": installConfig, "status": "active", "installer_user_id": user})
	session := fx.ChatSession(t, agent)
	fx.Insert(t, "channel_chat_session_binding", testutil.Cols{"chat_session_id": session, "installation_id": installation, "channel_type": "wecom", "channel_chat_id": "wecom-guest-v1:test", "chat_type": "group", "config": config})
	fx.InsertNoID(t, "channel_chat_context_generation", testutil.Cols{"chat_session_id": session, "revision": 1}, "chat_session_id=$1", session)
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	chat, err := svc.Queries.GetChatSession(ctx, util.MustParseUUID(session))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := svc.Queries.GetChannelChatSessionBindingBySessionAny(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.EnqueueChannelChatTask(ctx, chat, util.MustParseUUID(user), false, 1, binding.ID, binding.RouteRevision)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", task.ID)
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE task_id=$1", task.ID)
	if task.OriginatorSource.String != channelaccess.WecomGuestSource || string(task.RuntimeMcpOverlay) != "{}" || string(task.RuntimeConnectedApps) != "[]" {
		t.Fatalf("unsafe queued task: %+v", task)
	}
	if err := svc.ValidateWecomGuestTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.PrepareChatTaskEnqueue(channelaccess.WithWecomGuest(ctx, guest), util.MustParseUUID(agent), util.MustParseUUID(user))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.attrSource.String != channelaccess.WecomGuestSource || string(prepared.runtimeOverlay.Overlay) != "{}" || string(prepared.runtimeOverlay.ConnectedApps) != "[]" {
		t.Fatalf("unsafe guest preparation: %+v", prepared)
	}
	chat, err = svc.Queries.GetChatSession(ctx, util.MustParseUUID(session))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnqueueChatTask(ctx, chat, util.MustParseUUID(user), false); err == nil {
		t.Fatal("first-party enqueue entered guest session")
	}
	agentRow, err := svc.Queries.GetAgent(ctx, chat.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendDirectChatMessage(ctx, chat, agentRow, util.MustParseUUID(user), "must not persist", nil, "user", util.MustParseUUID(user)); err == nil {
		t.Fatal("direct send entered guest session")
	}
	if fx.Count(t, "SELECT count(*) FROM chat_message WHERE chat_session_id=$1", session) != 0 {
		t.Fatal("rejected first-party message persisted")
	}
	corrupt := task
	corrupt.RuntimeMcpOverlay = []byte(`{"mcpServers":{"personal":{}}}`)
	if svc.ValidateWecomGuestTask(ctx, corrupt) == nil {
		t.Fatal("personal overlay allowed")
	}
	corrupt = task
	corrupt.OriginatorSource.String = "direct_human"
	if svc.ValidateWecomGuestTask(ctx, corrupt) == nil {
		t.Fatal("missing guest source allowed")
	}
	corrupt = task
	corrupt.ChatSessionID.Valid = false
	if svc.ValidateWecomGuestTask(ctx, corrupt) == nil {
		t.Fatal("missing guest metadata allowed")
	}
	fx.Exec(t, "UPDATE agent_task_queue SET status='failed',failure_reason='agent_error.provider_network' WHERE id=$1", task.ID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	newTask, err := svc.EnqueuePreparedChannelChatTaskInTx(ctx, tx, chat, util.MustParseUUID(user), true, 1, prepared)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if newTask.OriginatorSource.String != channelaccess.WecomGuestSource || string(newTask.RuntimeMcpOverlay) != "{}" {
		_ = tx.Rollback(ctx)
		t.Fatal("prepared /new lost guest identity")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	parent, err := svc.Queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := svc.MaybeRetryFailedTask(ctx, parent)
	if err != nil || retry == nil {
		t.Fatalf("retry: %v, %v", retry, err)
	}
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", retry.ID)
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE task_id=$1", retry.ID)
	if retry.InitiatorUserID.Valid {
		t.Fatal("fixture should exercise retry with no initiator")
	}
	if err := svc.ValidateWecomGuestTask(ctx, *retry); err != nil {
		t.Fatalf("valid retry rejected: %v", err)
	}
	fx.Exec(t, "UPDATE channel_installation SET status='revoked' WHERE id=$1", installation)
	if svc.ValidateWecomGuestTask(ctx, task) == nil {
		t.Fatal("revoked installation allowed")
	}
	fx.Exec(t, "UPDATE channel_installation SET status='active' WHERE id=$1", installation)
	fx.Exec(t, "UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2", ws, user)
	if svc.ValidateWecomGuestTask(ctx, task) == nil {
		t.Fatal("demoted sponsor allowed")
	}
	fx.Exec(t, "UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2", ws, user)
	t.Setenv(channelaccess.WecomEnv, "")
	if svc.ValidateWecomGuestTask(ctx, task) == nil {
		t.Fatal("revoked grant allowed")
	}
}
