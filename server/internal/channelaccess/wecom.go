// Package channelaccess defines explicit operator grants for external channels.
package channelaccess

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
)

var ErrWecomAccessDenied = errors.New("WeCom guest access denied")

func denied(reason string) error { return fmt.Errorf("%w: %s", ErrWecomAccessDenied, reason) }

const WecomEnv = "MULTICA_WECOM_GUEST_ACCESS"
const WecomGuestSource = "wecom_guest_grant"

// WecomGrant is an operator's explicit delegation to one configured agent.
// It does not sandbox that agent's filesystem, shell or configured tools.
type WecomGrant struct {
	BotID               string   `json:"bot_id"`
	WorkspaceID         string   `json:"workspace_id"`
	AgentID             string   `json:"agent_id"`
	SponsorUserID       string   `json:"sponsor_user_id"`
	AllowedGroupIDs     []string `json:"allowed_group_ids"`
	AllowDirectMessages bool     `json:"allow_direct_messages"`
	PolicyRevision      string   `json:"policy_revision,omitempty"`
}

// WecomGuest is persisted on the isolated session and task delivery snapshot.
// SenderID identifies the actual caller; SponsorUserID is the delegating human.
type WecomGuest struct {
	BotID         string `json:"bot_id"`
	WorkspaceID   string `json:"workspace_id"`
	AgentID       string `json:"agent_id"`
	SponsorUserID string `json:"sponsor_user_id"`
	SenderID      string `json:"sender_id"`
	ChatID        string `json:"chat_id"`
	ChatType      string `json:"chat_type"`
	GrantHash     string `json:"grant_hash"`
}

func LookupWecom(botID string) (*WecomGrant, error) {
	raw := strings.TrimSpace(os.Getenv(WecomEnv))
	if raw == "" {
		return nil, nil
	}
	var grants []WecomGrant
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&grants); err != nil {
		return nil, denied("invalid WeCom guest policy")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, denied("invalid WeCom guest policy suffix")
	}
	seen := map[string]bool{}
	var match *WecomGrant
	for _, g := range grants {
		if strings.TrimSpace(g.BotID) == "" || seen[g.BotID] {
			return nil, denied("invalid or duplicate WeCom guest bot")
		}
		seen[g.BotID] = true
		for _, id := range []string{g.WorkspaceID, g.AgentID, g.SponsorUserID} {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed == uuid.Nil || parsed.String() != id {
				return nil, denied("invalid WeCom guest policy UUID")
			}
		}
		if !g.AllowDirectMessages && len(g.AllowedGroupIDs) == 0 {
			return nil, denied("WeCom guest policy permits no chats")
		}
		for _, group := range g.AllowedGroupIDs {
			if strings.TrimSpace(group) == "" || group == "*" {
				return nil, denied("WeCom guest groups must be explicit")
			}
		}
		if g.BotID == botID {
			copy := g
			match = &copy
		}
	}
	return match, nil
}

func (g WecomGrant) Allows(chatID, chatType string) bool {
	if strings.TrimSpace(chatID) == "" {
		return false
	}
	switch chatType {
	case "p2p":
		return g.AllowDirectMessages
	case "group":
		for _, id := range g.AllowedGroupIDs {
			if id == chatID {
				return true
			}
		}
	}
	return false
}

func (g WecomGrant) fingerprint() string {
	raw, _ := json.Marshal(g)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (g WecomGrant) Snapshot(senderID, chatID, chatType string) *WecomGuest {
	return &WecomGuest{BotID: g.BotID, WorkspaceID: g.WorkspaceID, AgentID: g.AgentID, SponsorUserID: g.SponsorUserID, SenderID: senderID, ChatID: chatID, ChatType: chatType, GrantHash: g.fingerprint()}
}

// GuestFromConfig never converts malformed metadata into a member session.
func GuestFromConfig(config []byte) (*WecomGuest, error) {
	if len(config) == 0 {
		return nil, nil
	}
	var c struct {
		Guest *WecomGuest `json:"wecom_guest"`
	}
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("%w: malformed channel access metadata", ErrWecomAccessDenied)
	}
	return c.Guest, nil
}
