package wecom

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const wecomGroupRoutePrefix = "wecom-group-v1:"

// Group bindings isolate each sender. The installation already scopes the DB
// key; the transport chat ID is stored separately for text and file delivery.
func wecomSessionRoute(source channel.Source) (string, []byte, error) {
	if strings.TrimSpace(source.ChatID) == "" {
		return "", nil, errors.New("wecom: missing chat ID")
	}
	if source.ChatType != channel.ChatTypeGroup {
		return source.ChatID, nil, nil
	}
	if strings.TrimSpace(source.SenderID) == "" {
		return "", nil, errors.New("wecom: missing group sender ID")
	}
	tuple, err := json.Marshal([2]string{source.ChatID, source.SenderID})
	if err != nil {
		return "", nil, err
	}
	config, err := json.Marshal(map[string]string{"chat_id": source.ChatID, "sender_id": source.SenderID})
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%s%x", wecomGroupRoutePrefix, sha256.Sum256(tuple)), config, nil
}

// Historical bindings used the transport ID as their key. New isolated
// bindings must carry a valid target instead of leaking a synthetic key to WeCom.
func wecomBindingChatID(binding db.ChannelChatSessionBinding) (string, error) {
	var config struct {
		ChatID string `json:"chat_id"`
	}
	if len(binding.Config) > 0 {
		if err := json.Unmarshal(binding.Config, &config); err != nil {
			return "", fmt.Errorf("wecom: decode binding route: %w", err)
		}
	}
	if strings.TrimSpace(config.ChatID) != "" {
		return config.ChatID, nil
	}
	if strings.HasPrefix(binding.ChannelChatID, wecomGroupRoutePrefix) || strings.HasPrefix(binding.ChannelChatID, wecomGuestRoutePrefix) || strings.TrimSpace(binding.ChannelChatID) == "" {
		return "", errors.New("wecom: isolated binding missing transport chat ID")
	}
	return binding.ChannelChatID, nil
}
