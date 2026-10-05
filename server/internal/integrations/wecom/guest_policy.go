package wecom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrGuestPolicyConflict = errors.New("WeCom guest policy changed; reload before saving")
var ErrGuestPolicyValidation = errors.New("invalid WeCom guest policy request")

type GuestAccessUpdate struct {
	Enabled             bool     `json:"enabled"`
	AllowedGroupIDs     []string `json:"allowed_group_ids"`
	AllowDirectMessages bool     `json:"allow_direct_messages"`
	Version             string   `json:"version"`
}
type GuestAccessGroup struct {
	ChatID string  `json:"chat_id"`
	Name   *string `json:"name"`
}
type GuestAccessResponse struct {
	Enabled             bool               `json:"enabled"`
	AllowedGroupIDs     []string           `json:"allowed_group_ids"`
	AllowDirectMessages bool               `json:"allow_direct_messages"`
	Version             string             `json:"version"`
	Source              string             `json:"source"`
	SponsorUserID       string             `json:"sponsor_user_id"`
	UpdatedBy           *string            `json:"updated_by"`
	UpdatedAt           *string            `json:"updated_at"`
	Groups              []GuestAccessGroup `json:"groups"`
}
type GuestAccessSummary struct {
	Status              string `json:"status"`
	AllowedGroupCount   int    `json:"allowed_group_count"`
	AllowDirectMessages bool   `json:"allow_direct_messages"`
}
type GuestAccessService struct {
	Queries *db.Queries
	Tx      engine.TxStarter
}

func loadGuestPolicy(ctx context.Context, q *db.Queries, id, ws pgtype.UUID) (db.ChannelInstallation, channelaccess.EffectiveWecomPolicy, db.Agent, error) {
	row, err := q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{ID: id, WorkspaceID: ws, ChannelType: "wecom"})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, channelaccess.EffectiveWecomPolicy{}, db.Agent{}, ErrInstallationNotFound
	}
	if err != nil {
		return row, channelaccess.EffectiveWecomPolicy{}, db.Agent{}, err
	}
	p, err := channelaccess.ResolveWecomInstallation(row)
	if err != nil {
		return row, p, db.Agent{}, err
	}
	agent, err := q.GetAgent(ctx, row.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, p, agent, ErrInstallationNotFound
	}
	if err != nil {
		return row, p, agent, err
	}
	if row.Status != "active" || agent.ArchivedAt.Valid || agent.WorkspaceID != ws || !agent.OwnerID.Valid {
		return row, p, agent, ErrInstallationNotFound
	}
	if _, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: ws, UserID: agent.OwnerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return row, p, agent, ErrInstallationNotFound
		}
		return row, p, agent, err
	}
	// An owner change invalidates an open form as well as existing delegation.
	hash := sha256.Sum256([]byte(p.Version + ":" + agent.OwnerID.String()))
	p.Version = hex.EncodeToString(hash[:])
	return row, p, agent, nil
}

func guestPolicyResponse(p channelaccess.EffectiveWecomPolicy, owner pgtype.UUID, groups []GuestAccessGroup) GuestAccessResponse {
	ids := p.AllowedGroupIDs
	if ids == nil {
		ids = []string{}
	}
	return GuestAccessResponse{Enabled: p.Enabled, AllowedGroupIDs: ids, AllowDirectMessages: p.AllowDirectMessages, Version: p.Version, Source: p.Source, SponsorUserID: owner.String(), UpdatedBy: p.UpdatedBy, UpdatedAt: p.UpdatedAt, Groups: groups}
}
func guestPolicyGroups(ctx context.Context, q *db.Queries, row db.ChannelInstallation, allowed []string) ([]GuestAccessGroup, error) {
	bindings, err := q.ListWecomGuestGroupBindings(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		id, err := wecomBindingChatID(binding)
		if err != nil {
			continue
		}
		if channelaccess.ValidateWecomGroups([]string{id}) == nil {
			seen[id] = true
		}
	}
	for _, id := range allowed {
		seen[id] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	groups := make([]GuestAccessGroup, 0, len(ids))
	for _, id := range ids {
		groups = append(groups, GuestAccessGroup{ChatID: id})
	}
	return groups, nil
}
func (s *GuestAccessService) Get(ctx context.Context, id, ws pgtype.UUID) (GuestAccessResponse, error) {
	row, p, agent, err := loadGuestPolicy(ctx, s.Queries, id, ws)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	groups, err := guestPolicyGroups(ctx, s.Queries, row, p.AllowedGroupIDs)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	return guestPolicyResponse(p, agent.OwnerID, groups), nil
}
func sameGuestGroups(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range a {
		seen[id] = true
	}
	for _, id := range b {
		if !seen[id] {
			return false
		}
	}
	return true
}
func (s *GuestAccessService) Save(ctx context.Context, id, ws, updater pgtype.UUID, in GuestAccessUpdate) (GuestAccessResponse, error) {
	if (in.Enabled && !in.AllowDirectMessages && len(in.AllowedGroupIDs) == 0) || in.Version == "" || len(in.Version) > 256 || in.AllowedGroupIDs == nil || channelaccess.ValidateWecomGroups(in.AllowedGroupIDs) != nil {
		return GuestAccessResponse{}, ErrGuestPolicyValidation
	}
	if s.Tx == nil {
		return GuestAccessResponse{}, errors.New("missing transaction starter")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.Queries.WithTx(tx)
	// Read the routing key, lock both lifecycle slots, then reread. A bot swap
	// between read and locks cannot make us overwrite the replacement's policy.
	initial, err := q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{ID: id, WorkspaceID: ws, ChannelType: "wecom"})
	if errors.Is(err, pgx.ErrNoRows) {
		return GuestAccessResponse{}, ErrInstallationNotFound
	}
	if err != nil {
		return GuestAccessResponse{}, err
	}
	var config struct {
		BotID string `json:"bot_id"`
	}
	if json.Unmarshal(initial.Config, &config) != nil || config.BotID == "" {
		return GuestAccessResponse{}, channelaccess.ErrWecomAccessDenied
	}
	if err = lockInstallationSlots(ctx, q, ws, initial.AgentID, config.BotID); err != nil {
		return GuestAccessResponse{}, err
	}
	row, p, agent, err := loadGuestPolicy(ctx, q, id, ws)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	if p.Version != in.Version {
		return GuestAccessResponse{}, ErrGuestPolicyConflict
	}
	groups := in.AllowedGroupIDs
	unchanged := p.Enabled == in.Enabled && p.AllowDirectMessages == in.AllowDirectMessages && sameGuestGroups(p.AllowedGroupIDs, groups) && p.SponsorUserID == agent.OwnerID.String()
	generation := p.Generation
	if unchanged {
		groups = p.AllowedGroupIDs
	} else {
		generation = guuid.NewString()
	}
	// Keep the fingerprint of historical DM-only grants whose group field
	// was null, while persisting a strict array for the management API.
	legacyNilGroups := unchanged && ((p.Source == "environment" && p.AllowedGroupIDs == nil) || p.LegacyNilGroups)
	if groups == nil {
		groups = []string{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	by := updater.String()
	policy := channelaccess.WecomPolicy{Enabled: in.Enabled, AllowedGroupIDs: groups, AllowDirectMessages: in.AllowDirectMessages, SponsorUserID: agent.OwnerID.String(), Version: guuid.NewString(), UpdatedBy: &by, UpdatedAt: &now, Generation: generation, LegacyNilGroups: legacyNilGroups}
	raw, err := json.Marshal(policy)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	row, err = q.UpdateWecomGuestPolicy(ctx, db.UpdateWecomGuestPolicyParams{ID: id, WorkspaceID: ws, GuestAccess: raw})
	if errors.Is(err, pgx.ErrNoRows) {
		return GuestAccessResponse{}, ErrGuestPolicyConflict
	}
	if err != nil {
		return GuestAccessResponse{}, err
	}
	_, p, agent, err = loadGuestPolicy(ctx, q, id, ws)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	candidates, err := guestPolicyGroups(ctx, q, row, p.AllowedGroupIDs)
	if err != nil {
		return GuestAccessResponse{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GuestAccessResponse{}, err
	}
	return guestPolicyResponse(p, agent.OwnerID, candidates), nil
}
func (s *GuestAccessService) Summary(ctx context.Context, inst Installation) GuestAccessSummary {
	unavailable := GuestAccessSummary{Status: "unavailable"}
	_, p, agent, err := loadGuestPolicy(ctx, s.Queries, inst.ID, inst.WorkspaceID)
	if err != nil {
		return unavailable
	}
	if !p.Enabled {
		return GuestAccessSummary{Status: "disabled"}
	}
	if p.Grant == nil {
		return unavailable
	}
	if agent.OwnerID.String() != p.SponsorUserID {
		return unavailable
	}
	return GuestAccessSummary{Status: "enabled", AllowedGroupCount: len(p.AllowedGroupIDs), AllowDirectMessages: p.AllowDirectMessages}
}
func lockInstallationSlots(ctx context.Context, q *db.Queries, ws, agent pgtype.UUID, bot string) error {
	if err := q.LockChannelInstallationAppIDSlot(ctx, db.LockChannelInstallationAppIDSlotParams{ChannelType: "wecom", AppID: bot}); err != nil {
		return err
	}
	return q.LockChannelInstallationAgentSlot(ctx, db.LockChannelInstallationAgentSlotParams{ChannelType: "wecom", WorkspaceID: ws, AgentID: agent})
}
