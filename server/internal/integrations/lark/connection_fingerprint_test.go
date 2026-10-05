package lark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRowFingerprintWecomIgnoresGuestAccess(t *testing.T) {
	const credentials = `"bot_id":"bot","app_id":"app","secret_encrypted":"sealed","bot_display_name":"Support"`
	base := rowFingerprint(db.ChannelInstallation{ChannelType: "wecom", Config: []byte(`{` + credentials + `}`)})
	for _, tc := range []struct{ name, policy string }{
		{"first save", `{"enabled":true,"allowed_group_ids":["group"],"version":"one"}`},
		{"scope update", `{"enabled":true,"allowed_group_ids":["other"],"allow_direct_messages":true,"version":"two"}`},
		{"disabled", `{"enabled":false,"version":"three"}`},
		{"malformed policy", `"invalid-policy"`},
		{"null policy", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{ "guest_access":` + tc.policy + `,` + credentials + ` }`)
			original := bytes.Clone(raw)
			if got := rowFingerprint(db.ChannelInstallation{ChannelType: "wecom", Config: raw}); got != base {
				t.Error("policy-only edit changed the connection fingerprint")
			}
			if !bytes.Equal(raw, original) {
				t.Error("fingerprinting mutated the factory config")
			}
		})
	}
}

func TestRowFingerprintWecomPreservesConnectionChanges(t *testing.T) {
	baseConfig := map[string]any{"bot_id": "bot", "app_id": "app", "secret_encrypted": "sealed", "app_secret_encrypted": "sealed-app", "bot_display_name": "Support", "transport": map[string]any{"timeout": 9007199254740992}}
	raw, err := json.Marshal(baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	base := rowFingerprint(db.ChannelInstallation{ChannelType: "wecom", Config: raw})
	for _, field := range []string{"bot_id", "app_id", "secret_encrypted", "app_secret_encrypted", "bot_display_name", "new_option"} {
		t.Run(field, func(t *testing.T) {
			var changed map[string]json.RawMessage
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			changed[field] = json.RawMessage(`"changed"`)
			config, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if rowFingerprint(db.ChannelInstallation{ChannelType: "wecom", Config: config}) == base {
				t.Error("connection field change did not rotate the fingerprint")
			}
		})
	}
	if rowFingerprint(db.ChannelInstallation{ChannelType: "wecom", Config: bytes.Replace(raw, []byte("9007199254740992"), []byte("9007199254740993"), 1)}) == base {
		t.Error("canonicalization rounded away a numeric config change")
	}
}

func TestRowFingerprintPreservesRawHashOutsideWecomObjects(t *testing.T) {
	for _, typ := range []string{"feishu", "slack", "unknown", "wecom"} {
		for _, config := range []string{`{"guest_access":true}`, `{"guest_access":false}`, ` {"guest_access":true} `, `{"bot_id":`, `{"app_id":`, `{"bot_id":"bot"} trailing`, `null`, `[]`, `"config"`, ``} {
			if typ == "wecom" && json.Valid([]byte(config)) && bytes.HasPrefix(bytes.TrimSpace([]byte(config)), []byte("{")) {
				continue
			}
			t.Run(typ+"/"+config, func(t *testing.T) {
				hash := sha256.Sum256([]byte(typ + "\x00" + config))
				if got := rowFingerprint(db.ChannelInstallation{ChannelType: typ, Config: []byte(config)}); got != hex.EncodeToString(hash[:]) {
					t.Error("existing raw config hash changed")
				}
			})
		}
	}
}

func TestChannelInstallationStoreWecomGuestSaveKeepsFingerprintDB(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for the connection fingerprint integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fx := dbfx.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Fingerprint tester", "fingerprint-"+suffix+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "Fingerprint workspace", "fingerprint-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	agent := fx.Agent(t, "Fingerprint agent", "")
	bot := "fingerprint-" + suffix
	config, err := json.Marshal(map[string]string{"bot_id": bot, "app_id": bot, "secret_encrypted": "sealed"})
	if err != nil {
		t.Fatal(err)
	}
	id := util.MustParseUUID(fx.Insert(t, "channel_installation", dbfx.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agent, "channel_type": "wecom", "config": config, "installer_user_id": fx.UserID, "status": "active"}))
	grant, err := json.Marshal([]channelaccess.WecomGrant{{BotID: bot, WorkspaceID: fx.WorkspaceID, AgentID: agent, SponsorUserID: fx.UserID, AllowedGroupIDs: []string{"group"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(channelaccess.WecomEnv, string(grant))
	q := db.New(pool)
	store := NewChannelInstallationStore(q)
	installation := func() engine.Installation {
		t.Helper()
		rows, err := store.ListActiveInstallations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ID == id {
				return row
			}
		}
		t.Fatal("active installation missing")
		return engine.Installation{}
	}
	before := installation()
	svc := wecom.GuestAccessService{Queries: q, Tx: pool}
	policy, err := svc.Get(ctx, id, util.MustParseUUID(fx.WorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Source != "environment" || !policy.Enabled {
		t.Fatal("legacy ENV policy was not loaded")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		groups  []string
	}{{"equivalent ENV takeover", true, []string{"group"}}, {"scope edit", true, []string{"other"}}, {"disable", false, []string{"other"}}} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err = svc.Save(ctx, id, util.MustParseUUID(fx.WorkspaceID), util.MustParseUUID(fx.UserID), wecom.GuestAccessUpdate{Enabled: tc.enabled, AllowedGroupIDs: tc.groups, Version: policy.Version})
			if err != nil {
				t.Fatal(err)
			}
			after := installation()
			if after.Fingerprint != before.Fingerprint {
				t.Error("GuestAccessService.Save rotated the supervisor connection fingerprint")
			}
			if bytes.Equal(after.Config, before.Config) || !bytes.Contains(after.Config, []byte(`"guest_access"`)) {
				t.Error("store did not forward the updated raw config")
			}
		})
	}
}
