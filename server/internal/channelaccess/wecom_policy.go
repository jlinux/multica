package channelaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WecomPolicy is the independently persisted delegation, never a credential.
type WecomPolicy struct {
	Enabled             bool     `json:"enabled"`
	AllowedGroupIDs     []string `json:"allowed_group_ids"`
	AllowDirectMessages bool     `json:"allow_direct_messages"`
	SponsorUserID       string   `json:"sponsor_user_id"`
	Version             string   `json:"version"`
	UpdatedBy           *string  `json:"updated_by"`
	UpdatedAt           *string  `json:"updated_at"`
	Generation          string   `json:"generation,omitempty"`
	LegacyNilGroups     bool     `json:"legacy_nil_groups,omitempty"`
}

type EffectiveWecomPolicy struct {
	WecomPolicy
	Source string
	Grant  *WecomGrant
}

func ValidateWecomGroups(groups []string) error {
	if len(groups) > 100 {
		return denied("at most 100 explicit groups are allowed")
	}
	seen := map[string]bool{}
	for _, id := range groups {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id || len(id) > 256 || strings.ContainsAny(id, "*?") || seen[id] {
			return denied("group IDs must be unique, explicit and at most 256 bytes")
		}
		seen[id] = true
	}
	return nil
}

func canonicalUUID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u != uuid.Nil && u.String() == id
}

// ResolveWecomInstallation is the single policy precedence boundary. Presence
// of a database key (even null or invalid) consumes the environment fallback.
func ResolveWecomInstallation(row db.ChannelInstallation) (EffectiveWecomPolicy, error) {
	p := EffectiveWecomPolicy{WecomPolicy: WecomPolicy{AllowedGroupIDs: []string{}}, Source: "none"}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(row.Config, &cfg) != nil || cfg == nil {
		return p, denied("malformed installation configuration")
	}
	var bot string
	if json.Unmarshal(cfg["bot_id"], &bot) != nil || strings.TrimSpace(bot) == "" {
		return p, denied("missing installation bot")
	}
	if raw, exists := cfg["guest_access"]; exists {
		p.Source = "database"
		var required map[string]json.RawMessage
		if json.Unmarshal(raw, &required) != nil || required == nil {
			return p, denied("malformed database guest policy")
		}
		for _, key := range []string{"enabled", "allowed_group_ids", "allow_direct_messages", "sponsor_user_id", "version"} {
			if value, ok := required[key]; !ok || string(value) == "null" {
				return p, denied("incomplete database guest policy")
			}
		}
		for _, key := range []string{"generation", "legacy_nil_groups"} {
			if value, exists := required[key]; exists && string(value) == "null" {
				return p, denied("malformed database guest generation")
			}
		}
		if json.Unmarshal(raw, &p.WecomPolicy) != nil || p.Version == "" || ValidateWecomGroups(p.AllowedGroupIDs) != nil || (p.Enabled && !canonicalUUID(p.SponsorUserID)) || (p.SponsorUserID != "" && !canonicalUUID(p.SponsorUserID)) {
			return p, denied("invalid database guest policy")
		}
		if p.Generation != "" && !canonicalUUID(p.Generation) {
			return p, denied("invalid database guest generation")
		}
		if p.Enabled && !p.AllowDirectMessages && len(p.AllowedGroupIDs) == 0 {
			return p, denied("database guest policy permits no chats")
		}
		if p.LegacyNilGroups && len(p.AllowedGroupIDs) != 0 {
			return p, denied("invalid legacy guest group metadata")
		}
		if p.UpdatedBy != nil && !canonicalUUID(*p.UpdatedBy) {
			return p, denied("invalid database guest updater")
		}
		if p.UpdatedAt != nil {
			if _, err := time.Parse(time.RFC3339Nano, *p.UpdatedAt); err != nil {
				return p, denied("invalid database guest update time")
			}
		}
		if p.Enabled {
			p.Grant = &WecomGrant{BotID: bot, WorkspaceID: row.WorkspaceID.String(), AgentID: row.AgentID.String(), SponsorUserID: p.SponsorUserID, AllowedGroupIDs: p.AllowedGroupIDs, AllowDirectMessages: p.AllowDirectMessages, PolicyRevision: p.Generation}
			if p.LegacyNilGroups {
				p.Grant.AllowedGroupIDs = nil
			}
		}
	} else {
		g, err := LookupWecom(bot)
		if err != nil {
			return p, err
		}
		if g != nil && g.WorkspaceID == row.WorkspaceID.String() && g.AgentID == row.AgentID.String() {
			p.Source = "environment"
			p.Grant = g
			p.Enabled = true
			p.AllowedGroupIDs = g.AllowedGroupIDs
			p.AllowDirectMessages = g.AllowDirectMessages
			p.SponsorUserID = g.SponsorUserID
		}
	}
	// Token includes effective ENV state and scope, but no credential material.
	token, _ := json.Marshal([]any{row.ID.String(), row.WorkspaceID.String(), row.AgentID.String(), row.Status, bot, p.Source, p.WecomPolicy})
	digest := sha256.Sum256(token)
	p.Version = hex.EncodeToString(digest[:])
	if row.ChannelType != "wecom" || row.Status != "active" {
		p.Grant = nil
	}
	return p, nil
}

func ValidateWecomGrantSnapshot(g *WecomGrant, s *WecomGuest) error {
	if s == nil || strings.TrimSpace(s.SenderID) == "" {
		return denied("WeCom guest identity missing")
	}
	if g == nil || g.BotID != s.BotID || g.WorkspaceID != s.WorkspaceID || g.AgentID != s.AgentID || g.SponsorUserID != s.SponsorUserID || g.fingerprint() != s.GrantHash || !g.Allows(s.ChatID, s.ChatType) {
		return denied("WeCom guest grant is unavailable or changed")
	}
	return nil
}

// DisabledWecomPolicy is a durable lifecycle tombstone; reinstall must not
// accidentally revive an environment grant belonging to the previous bot.
func DisabledWecomPolicy() json.RawMessage {
	raw, err := json.Marshal(WecomPolicy{AllowedGroupIDs: []string{}, Version: uuid.NewString(), Generation: uuid.NewString()})
	if err != nil {
		panic(fmt.Sprintf("encode guest policy: %v", err))
	}
	return raw
}
