package channelaccess

import (
	"encoding/json"
	"errors"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"testing"
)

func policyInstallation(t *testing.T, policy any) db.ChannelInstallation {
	t.Helper()
	cfg := map[string]any{"bot_id": "bot", "app_id": "bot", "secret_encrypted": "untouched"}
	if policy != nil {
		cfg["guest_access"] = policy
	}
	raw, _ := json.Marshal(cfg)
	return db.ChannelInstallation{ID: actorUUID("44444444-4444-4444-4444-444444444444"), WorkspaceID: actorUUID("11111111-1111-1111-1111-111111111111"), AgentID: actorUUID("22222222-2222-2222-2222-222222222222"), ChannelType: "wecom", Status: "active", Config: raw}
}
func TestEffectiveWecomDatabaseDisableOverridesEnvironment(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	row := policyInstallation(t, map[string]any{"enabled": false, "allowed_group_ids": []string{}, "allow_direct_messages": false, "sponsor_user_id": "33333333-3333-3333-3333-333333333333", "version": "saved", "updated_by": nil, "updated_at": nil})
	p, err := ResolveWecomInstallation(row)
	if err != nil || p.Source != "database" || p.Enabled || p.Grant != nil {
		t.Fatalf("disabled persisted policy fell back: %+v %v", p, err)
	}
}
func TestEffectiveWecomMalformedDatabaseNeverFallsBack(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	for _, malformed := range []any{"broken", map[string]any{}, map[string]any{"enabled": true}, map[string]any{"enabled": false, "allowed_group_ids": []string{}, "allow_direct_messages": false, "sponsor_user_id": "", "version": "", "updated_by": nil, "updated_at": nil}} {
		_, err := ResolveWecomInstallation(policyInstallation(t, malformed))
		if !errors.Is(err, ErrWecomAccessDenied) {
			t.Fatalf("malformed database policy accepted: %v", err)
		}
	}
	nullGeneration := map[string]any{"enabled": true, "allowed_group_ids": []string{"group"}, "allow_direct_messages": false, "sponsor_user_id": "33333333-3333-3333-3333-333333333333", "version": "saved", "generation": nil}
	if _, err := ResolveWecomInstallation(policyInstallation(t, nullGeneration)); !errors.Is(err, ErrWecomAccessDenied) {
		t.Fatal("null persisted generation accepted")
	}
	row := policyInstallation(t, nil)
	row.Config = []byte(`{"bot_id":"bot","guest_access":null}`)
	if _, err := ResolveWecomInstallation(row); !errors.Is(err, ErrWecomAccessDenied) {
		t.Fatalf("null policy accepted: %v", err)
	}
}
func TestEffectiveWecomEnvironmentTakeoverPreservesSnapshot(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	p, err := ResolveWecomInstallation(policyInstallation(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	old := p.Grant.Snapshot("sender", "group", "group")
	policy := WecomPolicy{Enabled: true, AllowedGroupIDs: p.Grant.AllowedGroupIDs, SponsorUserID: p.Grant.SponsorUserID, Version: "saved"}
	saved, err := ResolveWecomInstallation(policyInstallation(t, policy))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateWecomGrantSnapshot(saved.Grant, old); err != nil {
		t.Fatal("equivalent takeover invalidated snapshot:", err)
	}
}
