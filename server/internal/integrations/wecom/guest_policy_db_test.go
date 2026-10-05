package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	guuid "github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWecomGuestPolicyLifecycleAndSnapshotRevocation(t *testing.T) {
	pool := reclaimTestDB(t)
	base := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	user := base.User(t, "guest lifecycle", "guest-life-"+suffix+"@example.test")
	ws := base.Workspace(t, "guest lifecycle", "guest-life-"+suffix)
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "member")
	runtime := fx.Runtime(t, "lifecycle")
	agent := fx.Agent(t, "lifecycle", runtime)
	svc, _ := newReclaimSvc(t, pool)
	policySvc := GuestAccessService{Queries: db.New(pool), Tx: pool}
	ctx := context.Background()
	params := InstallationParams{WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(user), BotID: "life-" + suffix, Secret: "test-secret"}
	grant := channelaccess.WecomGrant{BotID: params.BotID, WorkspaceID: ws, AgentID: agent, SponsorUserID: user, AllowedGroupIDs: []string{"z", "a"}}
	env, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(env))
	inst, err := svc.Upsert(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", ws)
	initial, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := policySvc.Save(ctx, inst.ID, params.WorkspaceID, params.InstallerUserID, GuestAccessUpdate{Enabled: true, AllowedGroupIDs: []string{"a", "z"}, Version: initial.Version})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := grant.Snapshot("external", "a", "group")
	if err := channelaccess.ValidateWecomActor(ctx, db.New(pool), snapshot); err != nil {
		t.Fatal("first takeover changed fingerprint:", err)
	}
	params.Secret = "rotated-test-secret"
	if _, err := svc.Upsert(ctx, params); err != nil {
		t.Fatal(err)
	}
	rotated, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Version != saved.Version || !rotated.Enabled {
		t.Fatal("same bot rotation lost guest policy")
	}
	saved, err = policySvc.Save(ctx, inst.ID, params.WorkspaceID, params.InstallerUserID, GuestAccessUpdate{Enabled: false, AllowedGroupIDs: saved.AllowedGroupIDs, Version: rotated.Version})
	if err != nil {
		t.Fatal(err)
	}
	if channelaccess.ValidateWecomActor(ctx, db.New(pool), snapshot) == nil {
		t.Fatal("disable did not revoke snapshot")
	}
	_, err = policySvc.Save(ctx, inst.ID, params.WorkspaceID, params.InstallerUserID, GuestAccessUpdate{Enabled: true, AllowedGroupIDs: saved.AllowedGroupIDs, Version: saved.Version})
	if err != nil {
		t.Fatal(err)
	}
	if channelaccess.ValidateWecomActor(ctx, db.New(pool), snapshot) == nil {
		t.Fatal("re-enabling revived pre-revocation snapshot")
	}
	if err := svc.Revoke(ctx, inst.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upsert(ctx, params); err != nil {
		t.Fatal(err)
	}
	reinstalled, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if reinstalled.Enabled || reinstalled.Source != "database" {
		t.Fatal("reinstall revived access")
	}
	params.BotID = "replacement-" + suffix
	if _, err := svc.Upsert(ctx, params); err != nil {
		t.Fatal(err)
	}
	params.BotID = grant.BotID
	if _, err := svc.Upsert(ctx, params); err != nil {
		t.Fatal(err)
	}
	restored, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Enabled || restored.Source != "database" {
		t.Fatal("bot swap back revived ENV")
	}
}

func TestWecomGuestPolicyConcurrentSaveAndCredentialRotation(t *testing.T) {
	pool := reclaimTestDB(t)
	base := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	user := base.User(t, "guest concurrent", "guest-concurrent-"+suffix+"@example.test")
	ws := base.Workspace(t, "guest concurrent", "guest-concurrent-"+suffix)
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "member")
	runtime := fx.Runtime(t, "concurrent")
	agent := fx.Agent(t, "concurrent", runtime)
	svc, _ := newReclaimSvc(t, pool)
	pSvc := GuestAccessService{Queries: db.New(pool), Tx: pool}
	ctx := context.Background()
	p := InstallationParams{WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(user), BotID: "concurrent-" + suffix, Secret: "original"}
	inst, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", ws)
	initial, err := pSvc.Get(ctx, inst.ID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 3)
	start := make(chan struct{})
	for _, group := range []string{"a", "b"} {
		go func(group string) {
			<-start
			_, err := pSvc.Save(ctx, inst.ID, p.WorkspaceID, p.InstallerUserID, GuestAccessUpdate{Enabled: true, AllowedGroupIDs: []string{group}, Version: initial.Version})
			result <- err
		}(group)
	}
	go func() { <-start; p.Secret = "rotated"; _, err := svc.Upsert(ctx, p); result <- err }()
	close(start)
	ok, conflict := 0, 0
	for i := 0; i < 3; i++ {
		err := <-result
		if err == nil {
			ok++
		} else if errors.Is(err, ErrGuestPolicyConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 2 || conflict != 1 {
		t.Fatalf("success=%d conflicts=%d", ok, conflict)
	}
	loaded, err := pSvc.Get(ctx, inst.ID, p.WorkspaceID)
	if err != nil || !loaded.Enabled || len(loaded.AllowedGroupIDs) != 1 {
		t.Fatalf("guest policy lost: %+v %v", loaded, err)
	}
	row, err := svc.store.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: inst.ID, ChannelType: "wecom"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := installationFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.box.Open(decoded.SecretEncrypted)
	if err != nil || string(secret) != "rotated" {
		t.Fatalf("rotated credential lost: %v", err)
	}
}

// Use a revoked routing slot on another agent, so the reclaim creates a new
// installation instead of preserving a tombstone on the old row.
func TestWecomGuestPolicyReclaimedSlotDoesNotReviveEnvironment(t *testing.T) {
	pool := reclaimTestDB(t)
	base := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	user := base.User(t, "guest reclaim", "guest-reclaim-"+suffix+"@example.test")
	ws := base.Workspace(t, "guest reclaim", "guest-reclaim-"+suffix)
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "member")
	runtime := fx.Runtime(t, "reclaim")
	agentA := fx.Agent(t, "A", runtime)
	agentB := fx.Agent(t, "B", runtime)
	svc, _ := newReclaimSvc(t, pool)
	ctx := context.Background()
	p := InstallationParams{WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agentA), InstallerUserID: util.MustParseUUID(user), BotID: "reclaim-" + suffix, Secret: "test"}
	a, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", ws)
	if err := svc.Revoke(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	p.AgentID = util.MustParseUUID(agentB)
	grant := channelaccess.WecomGrant{BotID: p.BotID, WorkspaceID: ws, AgentID: agentB, SponsorUserID: user, AllowedGroupIDs: []string{"group"}}
	env, _ := json.Marshal([]channelaccess.WecomGrant{grant})
	t.Setenv(channelaccess.WecomEnv, string(env))
	b, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := (&GuestAccessService{Queries: db.New(pool), Tx: pool}).Get(ctx, b.ID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Source != "database" {
		t.Fatal("reclaimed slot revived environment grant")
	}
}

// Different bot slots on one agent still serialize: the second install must
// read the first installation and persist a swap tombstone, even if both began
// while that agent had no installation.
func TestWecomGuestPolicyDifferentBotInstallsShareAgentLock(t *testing.T) {
	pool := reclaimTestDB(t)
	base := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	user := base.User(t, "guest agent lock", "guest-agent-lock-"+suffix+"@example.test")
	ws := base.Workspace(t, "guest agent lock", "guest-agent-lock-"+suffix)
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "member")
	runtime := fx.Runtime(t, "agent lock")
	agent := fx.Agent(t, "agent lock", runtime)
	svc, _ := newReclaimSvc(t, pool)
	probe := &guestSlotProbe{entered: make(chan string, 2), release: make(chan struct{})}
	svc.probe = probe
	var once sync.Once
	release := func() { once.Do(func() { close(probe.release) }) }
	t.Cleanup(release)
	p := InstallationParams{WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(user), BotID: "lock-a-" + suffix, Secret: "test-a"}
	result := make(chan error, 2)
	go func() { _, err := svc.Upsert(context.Background(), p); result <- err }()
	if entered := <-probe.entered; entered != p.BotID {
		t.Fatal("wrong first bot")
	}
	second := p
	second.BotID = "lock-b-" + suffix
	second.Secret = "test-b"
	go func() { _, err := svc.Upsert(context.Background(), second); result <- err }()
	select {
	case <-probe.entered:
		t.Fatal("different bot install bypassed agent serialization")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for i := 0; i < 2; i++ {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", ws)
	row, err := db.New(pool).GetChannelInstallationByAppID(context.Background(), db.GetChannelInstallationByAppIDParams{ChannelType: "wecom", AppID: second.BotID})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := channelaccess.ResolveWecomInstallation(row)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Source != "database" || policy.Enabled {
		t.Fatal("concurrent different-bot install skipped tombstone")
	}
}

type guestSlotProbe struct {
	entered chan string
	release chan struct{}
}

func (p *guestSlotProbe) Probe(ctx context.Context, bot, secret string) error {
	p.entered <- bot
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
