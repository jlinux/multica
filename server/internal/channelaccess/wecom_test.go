package channelaccess

import (
	"encoding/json"
	"testing"
)

const grantJSON = `[{"bot_id":"bot","workspace_id":"11111111-1111-1111-1111-111111111111","agent_id":"22222222-2222-2222-2222-222222222222","sponsor_user_id":"33333333-3333-3333-3333-333333333333","allowed_group_ids":["group"],"allow_direct_messages":false}]`

func TestWecomGrantBoundaries(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	g, err := LookupWecom("bot")
	if err != nil || g == nil {
		t.Fatalf("lookup: %v", err)
	}
	if !g.Allows("group", "group") || g.Allows("other", "group") || g.Allows("sender", "p2p") || g.Allows("group", "unknown") {
		t.Fatal("chat boundary not enforced")
	}
	snap := g.Snapshot("sender", "group", "group")
	if err := ValidateWecomSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	t.Setenv(WecomEnv, "[]")
	if err := ValidateWecomSnapshot(snap); err == nil {
		t.Fatal("revoked grant accepted")
	}
}
func TestWecomInvalidPoliciesFailClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `[{}]`, `[{"bot_id":"bot","unknown":true}]`, grantJSON + ` trailing`} {
		t.Setenv(WecomEnv, raw)
		if _, err := LookupWecom("bot"); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	t.Setenv(WecomEnv, "")
	if g, err := LookupWecom("bot"); g != nil || err != nil {
		t.Fatal("default must be disabled")
	}
}
func TestWecomSnapshotTampering(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	g, _ := LookupWecom("bot")
	for _, change := range []func(*WecomGuest){
		func(s *WecomGuest) { s.SenderID = "" }, func(s *WecomGuest) { s.ChatID = "other" }, func(s *WecomGuest) { s.SponsorUserID = "other" }, func(s *WecomGuest) { s.AgentID = "other" }, func(s *WecomGuest) { s.GrantHash = "old" },
	} {
		s := g.Snapshot("sender", "group", "group")
		change(s)
		if ValidateWecomSnapshot(s) == nil {
			t.Fatal("tampered snapshot accepted")
		}
	}
}

func TestWecomPolicyExactScopeAndExplicitPrivateAccess(t *testing.T) {
	t.Setenv(WecomEnv, grantJSON)
	if g, err := LookupWecom("different-bot"); err != nil || g != nil {
		t.Fatal("unknown bot received grant")
	}
	g, _ := LookupWecom("bot")
	g.AllowDirectMessages = true
	raw, _ := json.Marshal([]WecomGrant{*g})
	t.Setenv(WecomEnv, string(raw))
	if err := ValidateWecomSnapshot(g.Snapshot("visitor", "visitor", "p2p")); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal([]WecomGrant{*g, *g})
	t.Setenv(WecomEnv, string(raw))
	if _, err := LookupWecom("bot"); err == nil {
		t.Fatal("duplicate bot grants accepted")
	}
	g.AllowedGroupIDs = []string{"*"}
	raw, _ = json.Marshal([]WecomGrant{*g})
	t.Setenv(WecomEnv, string(raw))
	if _, err := LookupWecom("bot"); err == nil {
		t.Fatal("wildcard group accepted")
	}
}
