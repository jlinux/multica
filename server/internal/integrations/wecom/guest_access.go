package wecom

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

const wecomGuestRoutePrefix = "wecom-guest-v1:"

// A guest is a distinct external principal using an explicit administrator
// grant. Never infer a grant from the installation owner's existing access.
func resolveWecomGuest(ctx context.Context, q channelaccess.WecomActorQueries, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	platform, ok := inst.Platform.(Installation)
	if !ok || !inst.Active || strings.TrimSpace(msg.Source.SenderID) == "" {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	grant, err := channelaccess.LookupWecom(platform.BotID)
	if err != nil {
		return engine.ResolvedIdentity{}, err
	}
	if grant == nil || !grant.Allows(msg.Source.ChatID, string(msg.Source.ChatType)) {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	if grant.WorkspaceID != util.UUIDToString(inst.WorkspaceID) || grant.AgentID != util.UUIDToString(inst.AgentID) {
		return engine.ResolvedIdentity{}, fmt.Errorf("%w: grant does not match installation", channelaccess.ErrWecomAccessDenied)
	}
	guest := grant.Snapshot(msg.Source.SenderID, msg.Source.ChatID, string(msg.Source.ChatType))
	if err := channelaccess.ValidateWecomActor(ctx, q, guest); err != nil {
		return engine.ResolvedIdentity{}, err
	}
	var sponsor pgtype.UUID
	if err := sponsor.Scan(grant.SponsorUserID); err != nil {
		return engine.ResolvedIdentity{}, err
	}
	return engine.ResolvedIdentity{UserID: sponsor, WecomGuest: guest}, nil
}

func wecomGuestSessionRoute(source channel.Source, guest *channelaccess.WecomGuest) (string, []byte, error) {
	if guest == nil {
		return wecomSessionRoute(source)
	}
	if err := channelaccess.ValidateWecomSnapshot(guest); err != nil {
		return "", nil, err
	}
	if guest.SenderID != source.SenderID || guest.ChatID != source.ChatID || guest.ChatType != string(source.ChatType) {
		return "", nil, errors.New("WeCom guest route identity mismatch")
	}
	key, _, err := wecomSessionRoute(source)
	if err != nil {
		return "", nil, err
	}
	tuple, _ := json.Marshal([]string{key, source.SenderID, guest.GrantHash})
	config, err := json.Marshal(struct {
		ChatID   string                    `json:"chat_id"`
		SenderID string                    `json:"sender_id"`
		Guest    *channelaccess.WecomGuest `json:"wecom_guest"`
	}{source.ChatID, source.SenderID, guest})
	return fmt.Sprintf("%s%x", wecomGuestRoutePrefix, sha256.Sum256(tuple)), config, err
}
