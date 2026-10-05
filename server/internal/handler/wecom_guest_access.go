package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
)

func (h *Handler) wecomGuestAccessScope(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return ws, pgtype.UUID{}, false
	}
	if _, ok = h.requireWorkspaceRole(w, r, uuidToString(ws), "workspace not found", "owner", "admin"); !ok {
		return ws, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	return ws, id, ok
}
func guestAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wecom.ErrInstallationNotFound):
		writeError(w, http.StatusNotFound, "wecom installation not found")
	case errors.Is(err, wecom.ErrGuestPolicyConflict):
		writeError(w, http.StatusConflict, "guest access changed; reload before saving")
	case errors.Is(err, wecom.ErrGuestPolicyValidation):
		writeError(w, http.StatusBadRequest, "invalid guest access configuration")
	default:
		writeError(w, http.StatusInternalServerError, "failed to load or save guest access")
	}
}
func (h *Handler) GetWecomGuestAccess(w http.ResponseWriter, r *http.Request) {
	ws, id, ok := h.wecomGuestAccessScope(w, r)
	if !ok {
		return
	}
	svc := wecom.GuestAccessService{Queries: h.Queries, Tx: h.TxStarter}
	policy, err := svc.Get(r.Context(), id, ws)
	if err != nil {
		guestAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}
func (h *Handler) UpdateWecomGuestAccess(w http.ResponseWriter, r *http.Request) {
	ws, id, ok := h.wecomGuestAccessScope(w, r)
	if !ok {
		return
	}
	svc := wecom.GuestAccessService{Queries: h.Queries, Tx: h.TxStarter}
	// Scope and policy load fail before accepting a writable form or body.
	if _, err := svc.Get(r.Context(), id, ws); err != nil {
		guestAccessError(w, err)
		return
	}
	user, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	var body struct {
		Enabled             *bool    `json:"enabled"`
		AllowedGroupIDs     []string `json:"allowed_group_ids"`
		AllowDirectMessages *bool    `json:"allow_direct_messages"`
		Version             string   `json:"version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 48*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || dec.Decode(new(any)) != io.EOF || body.Enabled == nil || body.AllowDirectMessages == nil || body.AllowedGroupIDs == nil {
		writeError(w, http.StatusBadRequest, "invalid guest access request")
		return
	}
	response, err := svc.Save(r.Context(), id, ws, user, wecom.GuestAccessUpdate{Enabled: *body.Enabled, AllowedGroupIDs: body.AllowedGroupIDs, AllowDirectMessages: *body.AllowDirectMessages, Version: body.Version})
	if err != nil {
		guestAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
