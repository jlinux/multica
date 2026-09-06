package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	goalrules "github.com/multica-ai/multica/server/internal/goal"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type GoalResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Level        int16   `json:"level"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	ParentGoalID *string `json:"parent_goal_id"`
	OrphanReason string  `json:"orphan_reason"`
	PrevGoalID   *string `json:"prev_goal_id"`
	ProjectID    *string `json:"project_id"`
	Kind         *string `json:"kind"`
	Status       string  `json:"status"`
	OwnerType    *string `json:"owner_type"`
	OwnerID      *string `json:"owner_id"`
	Cycle        string  `json:"cycle"`
	DueDate      *string `json:"due_date"`
	IsRetro      bool    `json:"is_retro"`
	Position     float64 `json:"position"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	// ChildCount drives the "nothing is picking this up" warning on the
	// panorama. It is a count rather than the children themselves because the
	// board renders one tier at a time and never needs the rows here.
	ChildCount int64 `json:"child_count"`
	// Assignable is always false and is sent anyway. A client building an
	// assignee picker from a mixed list of work items should be told so by the
	// payload rather than by a convention it has to remember.
	Assignable bool `json:"assignable"`
}

func goalToResponse(g db.Goal) GoalResponse {
	return GoalResponse{
		ID:           uuidToString(g.ID),
		WorkspaceID:  uuidToString(g.WorkspaceID),
		Level:        g.Level,
		Title:        g.Title,
		Description:  g.Description,
		ParentGoalID: uuidToPtr(g.ParentGoalID),
		OrphanReason: g.OrphanReason,
		PrevGoalID:   uuidToPtr(g.PrevGoalID),
		ProjectID:    uuidToPtr(g.ProjectID),
		Kind:         textToPtr(g.Kind),
		Status:       g.Status,
		OwnerType:    textToPtr(g.OwnerType),
		OwnerID:      uuidToPtr(g.OwnerID),
		Cycle:        g.Cycle,
		DueDate:      dateToPtr(g.DueDate),
		IsRetro:      g.IsRetro,
		Position:     g.Position,
		CreatedAt:    timestampToString(g.CreatedAt),
		UpdatedAt:    timestampToString(g.UpdatedAt),
		Assignable:   goalrules.Assignable(),
	}
}

// writeGoalRuleError maps a rule violation to 400 and anything else to 500.
// Every message the rules package produces is written for a user to read, so
// it is passed through rather than replaced with a generic string.
func writeGoalRuleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, goalrules.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return true
	}
	writeError(w, http.StatusInternalServerError, "failed to validate goal")
	return true
}

type CreateGoalRequest struct {
	Level        int16   `json:"level"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	ParentGoalID *string `json:"parent_goal_id"`
	OrphanReason string  `json:"orphan_reason"`
	PrevGoalID   *string `json:"prev_goal_id"`
	ProjectID    *string `json:"project_id"`
	Kind         *string `json:"kind"`
	Status       string  `json:"status"`
	OwnerType    *string `json:"owner_type"`
	OwnerID      *string `json:"owner_id"`
	Cycle        string  `json:"cycle"`
	DueDate      *string `json:"due_date"`
	IsRetro      bool    `json:"is_retro"`
	Position     float64 `json:"position"`
}

func (h *Handler) ListGoals(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}

	params := db.ListGoalsParams{WorkspaceID: wsUUID}
	if v := r.URL.Query().Get("level"); v != "" {
		level, err := strconv.ParseInt(v, 10, 16)
		if err != nil || !goalrules.ValidLevel(int16(level)) {
			writeError(w, http.StatusBadRequest, "level must be 1, 2 or 3")
			return
		}
		params.Level = pgtype.Int2{Int16: int16(level), Valid: true}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		params.Status = pgtype.Text{String: v, Valid: true}
	}
	if v := r.URL.Query().Get("project_id"); v != "" {
		id, ok := parseUUIDOrBadRequest(w, v, "project_id")
		if !ok {
			return
		}
		params.ProjectID = id
	}
	if v := r.URL.Query().Get("owner_id"); v != "" {
		id, ok := parseUUIDOrBadRequest(w, v, "owner_id")
		if !ok {
			return
		}
		params.OwnerID = id
	}
	if v := r.URL.Query().Get("parent_goal_id"); v != "" {
		id, ok := parseUUIDOrBadRequest(w, v, "parent_goal_id")
		if !ok {
			return
		}
		params.ParentGoalID = id
	}
	if r.URL.Query().Get("orphan_only") == "true" {
		params.OrphanOnly = pgtype.Bool{Bool: true, Valid: true}
	}

	goals, err := h.Queries.ListGoals(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list goals")
		return
	}

	resp := make([]GoalResponse, len(goals))
	for i, g := range goals {
		resp[i] = goalToResponse(g)
	}
	h.fillGoalChildCounts(r, wsUUID, goals, resp)

	writeJSON(w, http.StatusOK, map[string]any{"goals": resp, "total": len(resp)})
}

// fillGoalChildCounts batches the alignment counts for a whole listing.
//
// A failure here is deliberately not fatal to the request: the counts drive a
// warning badge, and a panorama that renders without its badges is far better
// than one that 500s. The zero value reads as "no children", which is also
// what the badge shows when the count is genuinely zero, so the degraded mode
// is at worst a missing warning rather than a wrong one.
func (h *Handler) fillGoalChildCounts(r *http.Request, wsUUID pgtype.UUID, goals []db.Goal, resp []GoalResponse) {
	parentIDs := make([]pgtype.UUID, 0, len(goals))
	for _, g := range goals {
		// Only the upper tiers can be aligned to, so only they can be short of
		// children. Asking about cycle goals would be a guaranteed empty group.
		if goalrules.Level(g.Level) != goalrules.LevelCycle {
			parentIDs = append(parentIDs, g.ID)
		}
	}
	if len(parentIDs) == 0 {
		return
	}
	rows, err := h.Queries.CountGoalChildren(r.Context(), db.CountGoalChildrenParams{
		WorkspaceID: wsUUID,
		ParentIds:   parentIDs,
	})
	if err != nil {
		return
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[uuidToString(row.ParentGoalID)] = row.ChildCount
	}
	for i := range resp {
		resp[i].ChildCount = counts[resp[i].ID]
	}
}

func (h *Handler) GetGoal(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	resp := goalToResponse(g)

	if goalrules.Level(g.Level) != goalrules.LevelCycle {
		if rows, err := h.Queries.CountGoalChildren(r.Context(), db.CountGoalChildrenParams{
			WorkspaceID: wsUUID,
			ParentIds:   []pgtype.UUID{g.ID},
		}); err == nil && len(rows) > 0 {
			resp.ChildCount = rows[0].ChildCount
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// loadGoal resolves the {id} path param inside the caller's workspace. The
// workspace filter is part of the lookup rather than a later check, so a goal
// id from another workspace is indistinguishable from one that does not exist.
func (h *Handler) loadGoal(w http.ResponseWriter, r *http.Request) (db.Goal, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.Goal{}, wsUUID, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.Goal{}, wsUUID, false
	}
	g, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "goal not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load goal")
		}
		return db.Goal{}, wsUUID, false
	}
	return g, wsUUID, true
}

func (h *Handler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	var req CreateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// The rules package is pure and takes tiers, not ids, so the referenced
	// goals are resolved here — which is also where the workspace filter
	// belongs. A parent from another workspace reads as "not found" and can
	// never reach validation.
	in := goalrules.Input{
		Level:        goalrules.Level(req.Level),
		Title:        req.Title,
		Kind:         goalrules.Kind(strFromPtr(req.Kind)),
		OrphanReason: req.OrphanReason,
		HasProject:   req.ProjectID != nil && *req.ProjectID != "",
	}

	var parentID pgtype.UUID
	if req.ParentGoalID != nil && *req.ParentGoalID != "" {
		parent, ok := h.resolveGoalRef(w, r, wsUUID, *req.ParentGoalID, "parent_goal_id")
		if !ok {
			return
		}
		parentID = parent.ID
		in.HasParent = true
		in.ParentLevel = goalrules.Level(parent.Level)
	}

	var prevID pgtype.UUID
	if req.PrevGoalID != nil && *req.PrevGoalID != "" {
		prev, ok := h.resolveGoalRef(w, r, wsUUID, *req.PrevGoalID, "prev_goal_id")
		if !ok {
			return
		}
		prevID = prev.ID
		in.HasPrev = true
		in.PrevLevel = goalrules.Level(prev.Level)
	}

	var ownerType pgtype.Text
	var ownerID pgtype.UUID
	if req.OwnerType != nil && *req.OwnerType != "" {
		ownerType = pgtype.Text{String: *req.OwnerType, Valid: true}
		in.HasOwner = true
		in.OwnerType = goalrules.ActorType(*req.OwnerType)
	}
	if req.OwnerID != nil && *req.OwnerID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.OwnerID, "owner_id")
		if !ok {
			return
		}
		ownerID = id
	}
	if ownerType.Valid != ownerID.Valid {
		writeError(w, http.StatusBadRequest, "owner_type and owner_id must be given together")
		return
	}

	if writeGoalRuleError(w, goalrules.Validate(in)) {
		return
	}

	var projectID pgtype.UUID
	if in.HasProject {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		projectID = id
	}
	var dueDate pgtype.Date
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return
		}
		dueDate = d
	}
	status := req.Status
	if status == "" {
		status = string(goalrules.StatusNotStarted)
	}

	creatorType, creatorID := h.resolveActor(r, userID, workspaceID)
	creatorUUID, ok := parseUUIDOrBadRequest(w, creatorID, "creator_id")
	if !ok {
		return
	}

	var kind pgtype.Text
	if req.Kind != nil && *req.Kind != "" {
		kind = pgtype.Text{String: *req.Kind, Valid: true}
	}

	g, err := h.Queries.CreateGoal(r.Context(), db.CreateGoalParams{
		WorkspaceID:   wsUUID,
		Level:         req.Level,
		Title:         req.Title,
		Description:   req.Description,
		ParentGoalID:  parentID,
		OrphanReason:  req.OrphanReason,
		PrevGoalID:    prevID,
		ProjectID:     projectID,
		Kind:          kind,
		Status:        status,
		OwnerType:     ownerType,
		OwnerID:       ownerID,
		Cycle:         req.Cycle,
		DueDate:       dueDate,
		IsRetro:       req.IsRetro,
		Position:      req.Position,
		CreatedByType: creatorType,
		CreatedByID:   creatorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create goal")
		return
	}
	resp := goalToResponse(g)
	h.publish(protocol.EventGoalCreated, workspaceID, creatorType, creatorID,
		map[string]any{"goal": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// resolveGoalRef loads a goal named in a request body, scoped to the caller's
// workspace. A reference that does not resolve is a 400 naming the field
// rather than a 404, because the missing thing is an input the caller sent,
// not the resource they addressed.
func (h *Handler) resolveGoalRef(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw, field string) (db.Goal, bool) {
	id, ok := parseUUIDOrBadRequest(w, raw, field)
	if !ok {
		return db.Goal{}, false
	}
	g, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, field+" does not name a goal in this workspace")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load "+field)
		}
		return db.Goal{}, false
	}
	return g, true
}

func strFromPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type UpdateGoalRequest struct {
	Title        *string  `json:"title"`
	Description  *string  `json:"description"`
	ParentGoalID *string  `json:"parent_goal_id"`
	OrphanReason *string  `json:"orphan_reason"`
	PrevGoalID   *string  `json:"prev_goal_id"`
	ProjectID    *string  `json:"project_id"`
	Kind         *string  `json:"kind"`
	Status       *string  `json:"status"`
	OwnerType    *string  `json:"owner_type"`
	OwnerID      *string  `json:"owner_id"`
	Cycle        *string  `json:"cycle"`
	DueDate      *string  `json:"due_date"`
	Position     *float64 `json:"position"`
}

// UpdateGoal re-validates the WHOLE goal, not just the fields that arrived.
//
// Patching one field at a time is what lets a tier invariant break in two
// legal-looking steps: clear a parent here, change a kind there, and the goal
// ends up in a state no single request would have been allowed to create. The
// merged shape is validated instead, so every write leaves a goal that could
// have been created outright.
//
// level is absent from the request by design; a goal's tier is fixed.
func (h *Handler) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	current, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	var req UpdateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateGoalParams{ID: current.ID, WorkspaceID: wsUUID}
	in := goalrules.Input{
		Level:        goalrules.Level(current.Level),
		Title:        current.Title,
		Kind:         goalrules.Kind(current.Kind.String),
		OrphanReason: current.OrphanReason,
		HasProject:   current.ProjectID.Valid,
		HasParent:    current.ParentGoalID.Valid,
		HasPrev:      current.PrevGoalID.Valid,
		HasOwner:     current.OwnerType.Valid,
		OwnerType:    goalrules.ActorType(current.OwnerType.String),
	}

	// An unchanged parent still needs its tier for validation, so it is loaded
	// either way. This costs one read on an edit that does not touch alignment
	// and buys the guarantee above.
	if in.HasParent {
		parent, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{
			ID: current.ParentGoalID, WorkspaceID: wsUUID,
		})
		if err == nil {
			in.ParentLevel = goalrules.Level(parent.Level)
		} else if errors.Is(err, pgx.ErrNoRows) {
			// The parent was deleted out from under this goal. Treat it as
			// unaligned so the edit can proceed and the user can re-point it,
			// rather than making the goal permanently uneditable.
			in.HasParent = false
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load parent goal")
			return
		}
	}
	if in.HasPrev {
		prev, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{
			ID: current.PrevGoalID, WorkspaceID: wsUUID,
		})
		if err == nil {
			in.PrevLevel = goalrules.Level(prev.Level)
		} else if errors.Is(err, pgx.ErrNoRows) {
			in.HasPrev = false
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load previous goal")
			return
		}
	}

	if req.Title != nil {
		in.Title = *req.Title
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.OrphanReason != nil {
		in.OrphanReason = *req.OrphanReason
		params.OrphanReason = pgtype.Text{String: *req.OrphanReason, Valid: true}
	}
	if req.Kind != nil {
		in.Kind = goalrules.Kind(*req.Kind)
		params.Kind = pgtype.Text{String: *req.Kind, Valid: *req.Kind != ""}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Cycle != nil {
		params.Cycle = pgtype.Text{String: *req.Cycle, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}

	// An explicit empty string means "unalign this goal", which is a different
	// request from omitting the field. The query needs a separate flag because
	// a NULL parameter already means "leave it alone".
	if req.ParentGoalID != nil {
		if *req.ParentGoalID == "" {
			params.ClearParent = true
			in.HasParent = false
		} else {
			parent, ok := h.resolveGoalRef(w, r, wsUUID, *req.ParentGoalID, "parent_goal_id")
			if !ok {
				return
			}
			params.ParentGoalID = parent.ID
			in.HasParent = true
			in.ParentLevel = goalrules.Level(parent.Level)
		}
	}
	if req.PrevGoalID != nil {
		if *req.PrevGoalID == "" {
			params.ClearPrev = true
			in.HasPrev = false
		} else {
			prev, ok := h.resolveGoalRef(w, r, wsUUID, *req.PrevGoalID, "prev_goal_id")
			if !ok {
				return
			}
			if prev.ID == current.ID {
				writeError(w, http.StatusBadRequest, "a goal cannot continue itself")
				return
			}
			params.PrevGoalID = prev.ID
			in.HasPrev = true
			in.PrevLevel = goalrules.Level(prev.Level)
		}
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		params.ProjectID = id
		in.HasProject = true
	}
	if req.OwnerType != nil {
		in.OwnerType = goalrules.ActorType(*req.OwnerType)
		in.HasOwner = *req.OwnerType != ""
		params.OwnerType = pgtype.Text{String: *req.OwnerType, Valid: in.HasOwner}
	}
	if req.OwnerID != nil && *req.OwnerID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.OwnerID, "owner_id")
		if !ok {
			return
		}
		params.OwnerID = id
	}
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return
		}
		params.DueDate = d
	}

	if params.ParentGoalID.Valid && params.ParentGoalID == current.ID {
		writeError(w, http.StatusBadRequest, "a goal cannot align to itself")
		return
	}
	if writeGoalRuleError(w, goalrules.Validate(in)) {
		return
	}

	g, err := h.Queries.UpdateGoal(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "goal not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update goal")
		return
	}
	resp := goalToResponse(g)
	h.publishGoalChange(r, protocol.EventGoalUpdated, map[string]any{"goal": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteGoal soft-deletes a goal and settles everything that pointed at it, in
// one transaction.
//
// There are no foreign keys in this schema by house rule, so nothing cleans up
// on its own and the order here IS the contract. Children and continuations are
// detached rather than deleted: they are somebody's real work, and losing a
// parent should demote them to unaligned goals, not remove them. Milestones are
// soft-deleted with their goal because a milestone without one has nothing to
// be a milestone of.
//
// Releases and proposals are deliberately left in place. They are reachable
// only through a milestone, and they are the audit trail of what shipped and
// who agreed it had — the part worth keeping longest.
func (h *Handler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete goal")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DetachGoalChildren(r.Context(), db.DetachGoalChildrenParams{
		WorkspaceID: wsUUID, ParentGoalID: g.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach aligned goals")
		return
	}
	if err := qtx.DetachGoalContinuations(r.Context(), db.DetachGoalContinuationsParams{
		WorkspaceID: wsUUID, PrevGoalID: g.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach continuations")
		return
	}
	if err := qtx.SoftDeleteMilestonesForGoal(r.Context(), db.SoftDeleteMilestonesForGoalParams{
		GoalID: g.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove milestones")
		return
	}
	if err := qtx.DeleteGoalIssueLinksForGoal(r.Context(), db.DeleteGoalIssueLinksForGoalParams{
		GoalID: g.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink issues")
		return
	}
	if err := qtx.SoftDeleteGoal(r.Context(), db.SoftDeleteGoalParams{
		ID: g.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete goal")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete goal")
		return
	}
	// After the commit: a listener that refetched on an event the transaction
	// then rolled back would show the goal as gone and bring it back.
	h.publishGoalChange(r, protocol.EventGoalDeleted,
		map[string]any{"goal_id": uuidToString(g.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

type GoalIssueLinkRequest struct {
	IssueID string `json:"issue_id"`
}

// LinkGoalIssue attaches an issue to a goal: the seam between the plan and the
// work. The link is stored only on the goal side, which is what keeps goals
// out of every issue query in the product.
func (h *Handler) LinkGoalIssue(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	var req GoalIssueLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Resolved through the shared loader, never parsed here: issue_id may be a
	// human identifier ("MUL-826") as well as a UUID, and the loader is what
	// applies the workspace and visibility checks. The link is written with the
	// id it returns, per the handler UUID rules.
	issue, ok := h.loadIssueForUser(w, r, req.IssueID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor_id")
	if !ok {
		return
	}
	if err := h.Queries.LinkGoalIssue(r.Context(), db.LinkGoalIssueParams{
		GoalID:       g.ID,
		IssueID:      issue.ID,
		WorkspaceID:  wsUUID,
		LinkedByType: pgtype.Text{String: actorType, Valid: true},
		LinkedByID:   actorUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link issue")
		return
	}
	h.publishGoalChange(r, protocol.EventGoalIssuesChanged, map[string]any{
		"goal_id":  uuidToString(g.ID),
		"issue_id": uuidToString(issue.ID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) UnlinkGoalIssue(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	if err := h.Queries.UnlinkGoalIssue(r.Context(), db.UnlinkGoalIssueParams{
		GoalID: g.ID, IssueID: issueID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink issue")
		return
	}
	h.publishGoalChange(r, protocol.EventGoalIssuesChanged, map[string]any{
		"goal_id":  uuidToString(g.ID),
		"issue_id": uuidToString(issueID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GoalIssueResponse is one issue delivering a goal, in the shape the goal
// detail renders. It is a summary rather than the full issue: this list sits
// under a goal, and a client that needs everything about one of these rows
// follows it to the issue itself.
type GoalIssueResponse struct {
	ID           string  `json:"id"`
	Number       int32   `json:"number"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	Priority     string  `json:"priority"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	LinkedAt     string  `json:"linked_at"`
}

func (h *Handler) ListGoalIssues(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListGoalIssues(r.Context(), db.ListGoalIssuesParams{
		GoalID: g.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list linked issues")
		return
	}
	out := make([]GoalIssueResponse, len(rows))
	for i, row := range rows {
		out[i] = GoalIssueResponse{
			ID:           uuidToString(row.ID),
			Number:       row.Number,
			Title:        row.Title,
			Status:       row.Status,
			Priority:     row.Priority,
			AssigneeType: textToPtr(row.AssigneeType),
			AssigneeID:   uuidToPtr(row.AssigneeID),
			LinkedAt:     timestampToString(row.LinkedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out, "total": len(out)})
}

// ListGoalsForIssue answers "which goals does this issue serve?" on the issue
// detail. It reads the join table from the issue side; the issue row itself
// still holds nothing about goals.
func (h *Handler) ListGoalsForIssue(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	goals, err := h.Queries.ListGoalsForIssue(r.Context(), db.ListGoalsForIssueParams{
		IssueID: issue.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list goals for issue")
		return
	}
	resp := make([]GoalResponse, len(goals))
	for i, g := range goals {
		resp[i] = goalToResponse(g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": resp, "total": len(resp)})
}

// publishGoalChange fans a goal-layer write out to everyone in the workspace.
//
// The goal tier is shared: one person's plan is the next person's context, and
// a board that only updates for whoever made the change is a board two people
// cannot use at once. Resolving the actor here rather than at each call site
// keeps the attribution consistent — an agent's write is labelled as an
// agent's write wherever it came from.
func (h *Handler) publishGoalChange(r *http.Request, eventType string, payload map[string]any) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(eventType, workspaceID, actorType, actorID, payload)
}
