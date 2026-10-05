package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func guestAccessFixture(t *testing.T) (string, string, string) {
	t.Helper()
	suffix := uuid.NewString()
	sponsor := dbfx.User(t, "ordinary sponsor", "wecom-sponsor-"+suffix+"@example.test")
	dbfx.Member(t, testWorkspaceID, sponsor, "member")
	runtime := dbfx.Runtime(t, "wecom-ui-runtime")
	agent := dbfx.Agent(t, "wecom-ui-agent", runtime, testutil.Cols{"owner_id": sponsor})
	bot := "wecom-ui-" + suffix
	raw, _ := json.Marshal(map[string]any{"bot_id": bot, "app_id": bot, "secret_encrypted": "Y3JlZGVudGlhbC1zZW50aW5lbA=="})
	id := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "installer_user_id": testUserID, "channel_type": "wecom", "config": raw})
	grant := channelaccess.WecomGrant{BotID: bot, WorkspaceID: testWorkspaceID, AgentID: agent, SponsorUserID: sponsor, AllowedGroupIDs: []string{"second", "first"}}
	env, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(env))
	return id, agent, sponsor
}
func guestAccessRequest(method, ws, id, user string, body any) *http.Request {
	req := testutil.JSONRequest(method, "/api/workspaces/"+ws+"/wecom/installations/"+id+"/guest-access", body)
	testutil.WithHeaders(req, "X-User-ID", user)
	return testutil.WithURLParams(req, "id", ws, "installationId", id)
}
func TestWecomGuestAccessEnvironmentSaveDisableAndConflict(t *testing.T) {
	id, _, sponsor := guestAccessFixture(t)
	var get map[string]any
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&get)
	if get["source"] != "environment" || get["sponsor_user_id"] != sponsor {
		t.Fatalf("wrong initial policy: %+v", get)
	}
	body := map[string]any{"enabled": true, "allowed_group_ids": []string{"first", "second"}, "allow_direct_messages": false, "version": get["version"]}
	var saved map[string]any
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, body)).Want(200).JSON(&saved)
	if saved["source"] != "database" || saved["allowed_group_ids"].([]any)[0] != "second" {
		t.Fatal("equivalent save did not preserve legacy order")
	}
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, body)).Want(409)
	body["version"] = saved["version"]
	body["enabled"] = false
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, body)).Want(200).JSON(&saved)
	if saved["enabled"] != false || saved["source"] != "database" {
		t.Fatal("disable not persisted")
	}
	var config map[string]any
	dbfx.QueryRow(t, "SELECT config FROM channel_installation WHERE id=$1", id).Scan(&config)
	if config["secret_encrypted"] != "Y3JlZGVudGlhbC1zZW50aW5lbA==" {
		t.Fatal("credentials changed")
	}
}
func TestWecomGuestAccessRoleScopeMalformedAndValidation(t *testing.T) {
	id, _, sponsor := guestAccessFixture(t)
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, sponsor, nil)).Want(403)
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, sponsor, map[string]any{})).Want(403)
	other := dbfx.Workspace(t, "other", "other-"+uuid.NewString())
	dbfx.Member(t, other, testUserID, "admin")
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", other, id, testUserID, nil)).Want(404)
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", other, id, testUserID, map[string]any{})).Want(404)
	var policy map[string]any
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&policy)
	for _, groups := range [][]string{{"*"}, {" "}, {"duplicate", "duplicate"}} {
		testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": groups, "allow_direct_messages": false, "version": policy["version"]})).Want(400)
	}
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": []string{"first"}, "allow_direct_messages": false, "version": policy["version"], "sponsor_user_id": testUserID})).Want(400)
	dbfx.Exec(t, "UPDATE channel_installation SET config=jsonb_set(config,'{guest_access}','null'::jsonb) WHERE id=$1", id)
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(500)
}

func TestWecomGuestAccessPrivateOnlyLegacyTakeover(t *testing.T) {
	id, agent, sponsor := guestAccessFixture(t)
	var bot string
	dbfx.QueryRow(t, "SELECT config->>'bot_id' FROM channel_installation WHERE id=$1", id).Scan(&bot)
	grant := channelaccess.WecomGrant{BotID: bot, WorkspaceID: testWorkspaceID, AgentID: agent, SponsorUserID: sponsor, AllowDirectMessages: true}
	raw, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(raw))
	snapshot := grant.Snapshot("guest", "guest", "p2p")
	var initial map[string]any
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&initial)
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": []string{}, "allow_direct_messages": true, "version": initial["version"]})).Want(200)
	if err := channelaccess.ValidateWecomActor(context.Background(), testHandler.Queries, snapshot); err != nil {
		t.Fatal("private-only takeover invalidated snapshot:", err)
	}
}
func TestWecomGuestAccessEnabledNeedsChatScope(t *testing.T) {
	id, _, _ := guestAccessFixture(t)
	var initial map[string]any
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&initial)
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": []string{}, "allow_direct_messages": false, "version": initial["version"]})).Want(400)
}

func TestWecomGuestAccessGroupsAndMemberSummary(t *testing.T) {
	id, agent, sponsor := guestAccessFixture(t)
	for _, binding := range []struct{ key, chatType, config string }{
		{"wecom-group-v1:sender-a", "group", `{"chat_id":"real-group","sender_id":"a"}`},
		{"wecom-guest-v1:sender-b", "group", `{"chat_id":"real-group","sender_id":"b"}`},
		{"private-user", "p2p", `{"chat_id":"private-user"}`},
		{"wecom-group-v1:missing-real", "group", `{}`},
	} {
		session := dbfx.ChatSession(t, agent)
		dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{"chat_session_id": session, "installation_id": id, "channel_type": "wecom", "channel_chat_id": binding.key, "chat_type": binding.chatType, "config": []byte(binding.config)})
	}
	var policy wecom.GuestAccessResponse
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&policy)
	if len(policy.Groups) != 3 {
		t.Fatalf("wrong candidate set: %+v", policy.Groups)
	}
	for _, group := range policy.Groups {
		if group.Name != nil || strings.HasPrefix(group.ChatID, "wecom-") || group.ChatID == "private-user" {
			t.Fatalf("non-transport candidate leaked: %+v", group)
		}
	}
	h := *testHandler
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	h.WecomStore = wecom.NewStore(h.Queries)
	h.WecomCredentials = &wecom.SecretboxCredentialsResolver{Box: box}
	h.ChannelRouter = &engine.Router{}
	router := chi.NewRouter()
	router.With(middleware.RequireWorkspaceMemberFromURL(h.Queries, "id")).Get("/api/workspaces/{id}/wecom/installations", h.ListWecomInstallations)
	var list map[string]any
	req := testutil.WithHeaders(testutil.JSONRequest("GET", "/api/workspaces/"+testWorkspaceID+"/wecom/installations", nil), "X-User-ID", sponsor)
	testutil.Call(t, router.ServeHTTP, req).Want(200).JSON(&list)
	found := false
	for _, value := range list["installations"].([]any) {
		row := value.(map[string]any)
		if row["id"] != id {
			continue
		}
		found = true
		summary := row["guest_access"].(map[string]any)
		if summary["status"] != "enabled" || summary["allowed_group_count"] != float64(2) || len(summary) != 3 {
			t.Fatalf("wrong member summary: %+v", summary)
		}
		if _, ok := row["secret_encrypted"]; ok {
			t.Fatal("credential leaked")
		}
		if _, ok := row["allowed_group_ids"]; ok {
			t.Fatal("full guest details leaked")
		}
	}
	if !found {
		t.Fatal("installation missing")
	}
	router = chi.NewRouter()
	router.With(middleware.RequireWorkspaceRoleFromURL(h.Queries, "id", "owner", "admin")).Get("/api/workspaces/{id}/wecom/installations/{installationId}/guest-access", h.GetWecomGuestAccess)
	testutil.Call(t, router.ServeHTTP, testutil.WithHeaders(testutil.JSONRequest("GET", "/api/workspaces/"+testWorkspaceID+"/wecom/installations/"+id+"/guest-access", nil), "X-User-ID", sponsor)).Want(403)
}

func TestWecomGuestAccessOwnerChangeRequiresExplicitAdminSave(t *testing.T) {
	id, agent, _ := guestAccessFixture(t)
	ctx := context.Background()
	svc := wecom.GuestAccessService{Queries: testHandler.Queries, Tx: testHandler.TxStarter}
	initial, err := svc.Get(ctx, parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Save(ctx, parseUUID(id), parseUUID(testWorkspaceID), parseUUID(testUserID), wecom.GuestAccessUpdate{Enabled: true, AllowedGroupIDs: initial.AllowedGroupIDs, Version: initial.Version})
	if err != nil {
		t.Fatal(err)
	}
	newOwner := dbfx.User(t, "new owner", "wecom-new-owner-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, newOwner, "member")
	dbfx.Exec(t, "UPDATE agent SET owner_id=$1 WHERE id=$2", newOwner, agent)
	row, err := testHandler.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: parseUUID(id), ChannelType: "wecom"})
	if err != nil {
		t.Fatal(err)
	}
	inst := wecom.Installation{ID: row.ID, WorkspaceID: row.WorkspaceID, AgentID: row.AgentID}
	if summary := svc.Summary(ctx, inst); summary.Status != "unavailable" {
		t.Fatalf("new owner silently inherited: %+v", summary)
	}
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": saved.AllowedGroupIDs, "allow_direct_messages": false, "version": saved.Version})).Want(409)
	var updated wecom.GuestAccessResponse
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&updated)
	if updated.SponsorUserID != newOwner {
		t.Fatal("GET did not show current owner")
	}
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": saved.AllowedGroupIDs, "allow_direct_messages": false, "version": updated.Version})).Want(200)
	if summary := svc.Summary(ctx, inst); summary.Status != "enabled" {
		t.Fatalf("explicit admin save failed to authorize new owner: %+v", summary)
	}
}

func TestWecomGuestAccessRequestBounds(t *testing.T) {
	id, _, _ := guestAccessFixture(t)
	var initial wecom.GuestAccessResponse
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&initial)
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("g-%d", i)
	}
	for _, groups := range [][]string{tooMany, {strings.Repeat("g", 257)}, {"leading "}, {"g?"}} {
		testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": groups, "allow_direct_messages": false, "version": initial.Version})).Want(400)
	}
	for _, body := range []string{`{}`, `{"enabled":true,"allowed_group_ids":[],"version":"x"}`, `{"enabled":false,"allowed_group_ids":[],"allow_direct_messages":false,"version":"x"} {}`, `{"enabled":false,"allowed_group_ids":[],"allow_direct_messages":false,"version":"` + strings.Repeat("x", 50000) + `"}`} {
		testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, body)).Want(400)
	}
}

func TestWecomGuestAccessEnvironmentVersionCannotOverwriteChangedGrant(t *testing.T) {
	id, agent, sponsor := guestAccessFixture(t)
	var initial wecom.GuestAccessResponse
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&initial)
	var bot string
	dbfx.QueryRow(t, "SELECT config->>'bot_id' FROM channel_installation WHERE id=$1", id).Scan(&bot)
	changed := channelaccess.WecomGrant{BotID: bot, WorkspaceID: testWorkspaceID, AgentID: agent, SponsorUserID: sponsor, AllowedGroupIDs: []string{"changed"}}
	raw, _ := json.Marshal([]channelaccess.WecomGrant{changed})
	t.Setenv(channelaccess.WecomEnv, string(raw))
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, id, testUserID, map[string]any{"enabled": true, "allowed_group_ids": initial.AllowedGroupIDs, "allow_direct_messages": false, "version": initial.Version})).Want(409)
	var policy wecom.GuestAccessResponse
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, id, testUserID, nil)).Want(200).JSON(&policy)
	if policy.Source != "environment" || len(policy.AllowedGroupIDs) != 1 || policy.AllowedGroupIDs[0] != "changed" {
		t.Fatal("stale form took over new environment policy")
	}
	wrongChannel := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "installer_user_id": testUserID, "channel_type": "slack", "config": []byte(`{}`)})
	testutil.Call(t, testHandler.GetWecomGuestAccess, guestAccessRequest("GET", testWorkspaceID, wrongChannel, testUserID, nil)).Want(404)
	testutil.Call(t, testHandler.UpdateWecomGuestAccess, guestAccessRequest("PUT", testWorkspaceID, wrongChannel, testUserID, map[string]any{})).Want(404)
}
