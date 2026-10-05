package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrWecomGuestAccess distinguishes revoked or invalid access from infrastructure failures.
var ErrWecomGuestAccess = channelaccess.ErrWecomAccessDenied

func wecomGuestBinding(ctx context.Context, q *db.Queries, binding db.ChannelChatSessionBinding) (*channelaccess.WecomGuest, error) {
	if binding.ChannelType != "wecom" {
		return nil, nil
	}
	guest, err := channelaccess.GuestFromConfig(binding.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed guest metadata", ErrWecomGuestAccess)
	}
	if guest == nil {
		if strings.HasPrefix(binding.ChannelChatID, "wecom-guest-v1:") {
			return nil, ErrWecomGuestAccess
		}
		return nil, nil
	}
	if err := channelaccess.ValidateWecomActor(ctx, q, guest); err != nil {
		return nil, err
	}
	installation, err := q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: binding.InstallationID, ChannelType: "wecom"})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWecomGuestAccess
		}
		return nil, err
	}
	var config struct {
		BotID string `json:"bot_id"`
	}
	if json.Unmarshal(installation.Config, &config) != nil || config.BotID != guest.BotID || installation.Status != "active" || util.UUIDToString(installation.WorkspaceID) != guest.WorkspaceID || util.UUIDToString(installation.AgentID) != guest.AgentID || binding.ChatType != guest.ChatType {
		return nil, ErrWecomGuestAccess
	}
	var route struct {
		ChatID   string `json:"chat_id"`
		SenderID string `json:"sender_id"`
	}
	if json.Unmarshal(binding.Config, &route) != nil || route.ChatID != guest.ChatID || route.SenderID != guest.SenderID {
		return nil, ErrWecomGuestAccess
	}
	return guest, nil
}

func guestPrepared(ctx context.Context, q *db.Queries, guest *channelaccess.WecomGuest, agentID, initiatorID pgtype.UUID) (PreparedChatTaskEnqueue, error) {
	if util.UUIDToString(agentID) != guest.AgentID || util.UUIDToString(initiatorID) != guest.SponsorUserID {
		return PreparedChatTaskEnqueue{}, ErrWecomGuestAccess
	}
	if err := channelaccess.ValidateWecomActor(ctx, q, guest); err != nil {
		return PreparedChatTaskEnqueue{}, err
	}
	return PreparedChatTaskEnqueue{accountableUser: initiatorID, attrSource: pgtype.Text{String: channelaccess.WecomGuestSource, Valid: true}, attrEvidenceKind: pgtype.Text{String: "chat", Valid: true}, runtimeOverlay: emptyGuestOverlay()}, nil
}

func emptyGuestOverlay() runtimeMCPOverlayData {
	return runtimeMCPOverlayData{Overlay: json.RawMessage(`{}`), ConnectedApps: json.RawMessage(`[]`)}
}

// ValidateWecomGuestTask rechecks delegated access at dispatch, including retries.
// A guest marker without durable channel identity must never become a human task.
func (s *TaskService) ValidateWecomGuestTask(ctx context.Context, task db.AgentTaskQueue) error {
	return validateWecomGuestTask(ctx, s.Queries, task)
}

// A revoked guest may not create new retry attempts. Infrastructure failures
// remain errors so the caller can retry admission after recovery.
func validateWecomGuestRetry(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	err := validateWecomGuestTask(ctx, q, task)
	if errors.Is(err, ErrWecomGuestAccess) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func validateWecomGuestTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) error {
	marked := task.OriginatorSource.String == channelaccess.WecomGuestSource
	if !task.ChatSessionID.Valid {
		if marked {
			return ErrWecomGuestAccess
		}
		return nil
	}
	binding, err := q.GetChannelChatSessionBindingBySessionAny(ctx, task.ChatSessionID)
	if errors.Is(err, pgx.ErrNoRows) && !marked {
		return nil
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWecomGuestAccess
		}
		return err
	}
	guest, err := wecomGuestBinding(ctx, q, binding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWecomGuestAccess
		}
		return err
	}
	if guest == nil {
		if marked {
			return ErrWecomGuestAccess
		}
		return nil
	}
	if !marked || util.UUIDToString(task.AgentID) != guest.AgentID || (util.UUIDToString(task.InitiatorUserID) != guest.SponsorUserID && !(task.RetryOfTaskID.Valid && !task.InitiatorUserID.Valid)) || util.UUIDToString(task.OriginatorUserID) != guest.SponsorUserID || util.UUIDToString(task.AccountableUserID) != guest.SponsorUserID {
		return ErrWecomGuestAccess
	}

	delivery, err := q.GetChannelTaskDelivery(ctx, task.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWecomGuestAccess
		}
		return err
	}
	snapshot, err := channelaccess.GuestFromConfig(delivery.Config)
	if err != nil {
		return ErrWecomGuestAccess
	}
	if snapshot == nil || *snapshot != *guest || delivery.InstallationID != binding.InstallationID || delivery.BindingID != binding.ID || delivery.ChannelType != "wecom" {
		return ErrWecomGuestAccess
	}
	var overlay map[string]json.RawMessage
	var apps []json.RawMessage
	if len(task.RuntimeMcpOverlay) > 0 && (json.Unmarshal(task.RuntimeMcpOverlay, &overlay) != nil || len(overlay) > 0) {
		return ErrWecomGuestAccess
	}
	if len(task.RuntimeConnectedApps) > 0 && (json.Unmarshal(task.RuntimeConnectedApps, &apps) != nil || len(apps) > 0) {
		return ErrWecomGuestAccess
	}
	session, err := q.GetChatSession(ctx, task.ChatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWecomGuestAccess
		}
		return err
	}
	if util.UUIDToString(session.WorkspaceID) != guest.WorkspaceID || session.AgentID != task.AgentID {
		return ErrWecomGuestAccess
	}
	return nil
}

func (s *TaskService) guestEnqueueContext(ctx context.Context, session db.ChatSession, requireDelivery bool) (context.Context, error) {
	binding, err := s.Queries.GetChannelChatSessionBindingBySessionAny(ctx, session.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nil
	}
	if err != nil {
		return ctx, err
	}
	guest, err := wecomGuestBinding(ctx, s.Queries, binding)
	if err != nil {
		return ctx, err
	}
	if guest == nil {
		return ctx, nil
	}
	if !requireDelivery || util.UUIDToString(session.WorkspaceID) != guest.WorkspaceID {
		return ctx, ErrWecomGuestAccess
	}
	return channelaccess.WithWecomGuest(ctx, guest), nil
}

// Retry must not refresh the sponsor's personal connected-app credentials.
func (s *TaskService) taskRetryOverlay(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) runtimeMCPOverlayData {
	if task.OriginatorSource.String == channelaccess.WecomGuestSource {
		return emptyGuestOverlay()
	}
	if task.ChatSessionID.Valid {
		binding, err := s.Queries.GetChannelChatSessionBindingBySessionAny(ctx, task.ChatSessionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return emptyGuestOverlay()
		}
		if err == nil && binding.ChannelType == "wecom" {
			guest, err := channelaccess.GuestFromConfig(binding.Config)
			if err != nil || guest != nil || strings.HasPrefix(binding.ChannelChatID, "wecom-guest-v1:") {
				return emptyGuestOverlay()
			}
		}
	}
	return s.buildRuntimeMCPOverlay(ctx, task.OriginatorUserID, agent)
}

func validateGuestEnqueueTx(ctx context.Context, q *db.Queries, binding db.ChannelChatSessionBinding, session db.ChatSession, initiator pgtype.UUID, delivery bool, prepared PreparedChatTaskEnqueue) (PreparedChatTaskEnqueue, error) {
	guest, err := wecomGuestBinding(ctx, q, binding)
	if err != nil {
		return prepared, err
	}
	if guest == nil {
		if prepared.attrSource.String == channelaccess.WecomGuestSource {
			return prepared, ErrWecomGuestAccess
		}
		return prepared, nil
	}
	if !delivery || util.UUIDToString(session.WorkspaceID) != guest.WorkspaceID {
		return prepared, ErrWecomGuestAccess
	}
	result, err := guestPrepared(ctx, q, guest, session.AgentID, initiator)
	if err != nil {
		return prepared, fmt.Errorf("guest enqueue: %w", err)
	}
	return result, nil
}
