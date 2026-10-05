package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	inst := seedLegacyGuestInstallation(t, fx, svc, params)
	initial, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !initial.Enabled || initial.Source != "environment" {
		t.Fatal("existing legacy installation lost ENV access")
	}
	params.Secret = "legacy-rotated-test-secret"
	legacyRotated, err := svc.Upsert(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.box.Open(legacyRotated.SecretEncrypted)
	if err != nil || string(secret) != params.Secret {
		t.Fatalf("legacy credential rotation lost secret: %v", err)
	}
	beforeSave, err := policySvc.Get(ctx, inst.ID, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !beforeSave.Enabled || beforeSave.Source != "environment" || beforeSave.Version != initial.Version {
		t.Fatal("same bot legacy rotation lost ENV policy or changed version")
	}
	saved, err := policySvc.Save(ctx, inst.ID, params.WorkspaceID, params.InstallerUserID, GuestAccessUpdate{Enabled: true, AllowedGroupIDs: []string{"a", "z"}, Version: initial.Version})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || saved.Source != "database" {
		t.Fatal("equivalent first save did not take over legacy policy")
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

// Pre-upgrade installations have sealed credentials but no database guest
// policy. Seed that historical state directly rather than via today's Upsert.
func seedLegacyGuestInstallation(t *testing.T, fx *testutil.Fixture, svc *InstallationService, p InstallationParams) Installation {
	t.Helper()
	sealed, err := svc.box.Seal([]byte(p.Secret))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := encodeInstallConfig(Installation{BotID: p.BotID, SecretEncrypted: sealed})
	if err != nil {
		t.Fatal(err)
	}
	id := fx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": p.WorkspaceID, "agent_id": p.AgentID, "channel_type": "wecom",
		"config": cfg, "installer_user_id": p.InstallerUserID, "status": "active",
	})
	inst, err := svc.GetInWorkspace(context.Background(), util.MustParseUUID(id), p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestWecomGuestPolicyFreshInstallDefaultsDisabledDespiteEnvironment(t *testing.T) {
	pool := reclaimTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	fx.UserID = fx.User(t, "fresh sponsor", "fresh-"+suffix+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "fresh install", "fresh-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "member")
	agent := fx.Agent(t, "fresh agent", fx.Runtime(t, "fresh runtime"))
	svc, _ := newReclaimSvc(t, pool)
	p := InstallationParams{WorkspaceID: util.MustParseUUID(fx.WorkspaceID), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(fx.UserID), BotID: "fresh-" + suffix, Secret: "fresh-test-secret"}
	grant := channelaccess.WecomGrant{BotID: p.BotID, WorkspaceID: fx.WorkspaceID, AgentID: agent, SponsorUserID: fx.UserID, AllowedGroupIDs: []string{"group"}}
	env, err := json.Marshal([]channelaccess.WecomGrant{grant})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(channelaccess.WecomEnv, string(env))
	ctx := context.Background()
	inst, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", fx.WorkspaceID)
	policy, err := (&GuestAccessService{Queries: db.New(pool), Tx: pool}).Get(ctx, inst.ID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Source != "database" {
		t.Fatalf("fresh install revived ENV grant: enabled=%v source=%s", policy.Enabled, policy.Source)
	}
	if err := channelaccess.ValidateWecomActor(ctx, db.New(pool), grant.Snapshot("external", "group", "group")); !errors.Is(err, channelaccess.ErrWecomAccessDenied) {
		t.Fatalf("fresh install accepted ENV snapshot: %v", err)
	}
}

func TestWecomGuestPolicyDeletedReclaimedWorkspaceDoesNotReviveEnvironment(t *testing.T) {
	pool := reclaimTestDB(t)
	base := testutil.New(pool, "", "")
	suffix := guuid.NewString()
	user := base.User(t, "deleted reclaim sponsor", "deleted-reclaim-"+suffix+"@example.test")
	wsX := base.Workspace(t, "original workspace", "original-"+suffix)
	wsY := base.Workspace(t, "temporary workspace", "temporary-"+suffix)
	fxX, fxY := testutil.New(pool, wsX, user), testutil.New(pool, wsY, user)
	fxX.Member(t, wsX, user, "member")
	fxY.Member(t, wsY, user, "member")
	agentX := fxX.Agent(t, "original agent", fxX.Runtime(t, "original runtime"))
	agentY := fxY.Agent(t, "temporary agent", fxY.Runtime(t, "temporary runtime"))
	svc, _ := newReclaimSvc(t, pool)
	q := db.New(pool)
	policySvc := GuestAccessService{Queries: q, Tx: pool}
	ctx := context.Background()
	p := InstallationParams{WorkspaceID: util.MustParseUUID(wsX), AgentID: util.MustParseUUID(agentX), InstallerUserID: util.MustParseUUID(user), BotID: "deleted-reclaim-" + suffix, Secret: "test-secret"}
	grant := channelaccess.WecomGrant{BotID: p.BotID, WorkspaceID: wsX, AgentID: agentX, SponsorUserID: user, AllowedGroupIDs: []string{"group"}}
	env, err := json.Marshal([]channelaccess.WecomGrant{grant})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(channelaccess.WecomEnv, string(env))
	legacy := seedLegacyGuestInstallation(t, fxX, svc, p)
	snapshot := grant.Snapshot("external", "group", "group")
	if err := channelaccess.ValidateWecomActor(ctx, q, snapshot); err != nil {
		t.Fatalf("legacy ENV grant unavailable before revoke: %v", err)
	}
	if err := svc.Revoke(ctx, legacy.ID); err != nil {
		t.Fatal(err)
	}
	moved := p
	moved.WorkspaceID, moved.AgentID = util.MustParseUUID(wsY), util.MustParseUUID(agentY)
	reclaimed, err := svc.Upsert(ctx, moved)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.ID == legacy.ID {
		t.Fatal("move did not reclaim original installation")
	}
	movedPolicy, err := policySvc.Get(ctx, reclaimed.ID, moved.WorkspaceID)
	if err != nil || movedPolicy.Enabled || movedPolicy.Source != "database" {
		t.Fatalf("move lost disabled policy: %+v %v", movedPolicy, err)
	}
	if err := q.DeleteWorkspace(ctx, moved.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{ChannelType: "wecom", AppID: p.BotID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("DeleteWorkspace did not remove last installation: %v", err)
	}
	reinstalled, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fxX.Cleanup(t, "DELETE FROM channel_installation WHERE workspace_id=$1", wsX)
	policy, err := policySvc.Get(ctx, reinstalled.ID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Source != "database" {
		t.Fatalf("reinstall after deleted tombstone revived ENV: enabled=%v source=%s", policy.Enabled, policy.Source)
	}
	if err := channelaccess.ValidateWecomActor(ctx, q, snapshot); !errors.Is(err, channelaccess.ErrWecomAccessDenied) {
		t.Fatalf("deleted tombstone revived pre-revocation snapshot: %v", err)
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
