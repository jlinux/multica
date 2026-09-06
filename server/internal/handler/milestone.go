package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	goalrules "github.com/multica-ai/multica/server/internal/goal"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type MilestoneResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	GoalID      string `json:"goal_id"`
	Type        string `json:"type"`
	N           *int32 `json:"n"`
	Title       string `json:"title"`
	// ValueStatement is what this delivery is worth, in the language of the
	// people who will use it. A milestone nobody can state the value of is a
	// date, not a milestone.
	ValueStatement string `json:"value_statement"`
	// OriginalPlannedDate is the first date ever planned and never changes.
	// On-time attainment is measured against it, so a reschedule cannot turn a
	// slip into a hit. Sent so a client can show both without a second call.
	OriginalPlannedDate *string `json:"original_planned_date"`
	PlannedDate         *string `json:"planned_date"`
	ActualDate          *string `json:"actual_date"`
	Status              string  `json:"status"`
	IsDelayed           bool    `json:"is_delayed"`
	AdoptionCheck       *string `json:"adoption_check"`
	VerifierType        *string `json:"verifier_type"`
	VerifierID          *string `json:"verifier_id"`
	VerifierLabel       string  `json:"verifier_label"`
	AcceptedByType      *string `json:"accepted_by_type"`
	AcceptedByID        *string `json:"accepted_by_id"`
	AcceptNote          string  `json:"accept_note"`
	AcceptedAt          string  `json:"accepted_at"`
	IsRetro             bool    `json:"is_retro"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	// RequiresAcceptance tells a client whether reaching this milestone needs
	// someone other than the owner to say so, without it having to re-derive
	// the rule from the type.
	RequiresAcceptance bool `json:"requires_acceptance"`
	// DateChangeCount backs the "moved N times" counter on the card.
	DateChangeCount int64 `json:"date_change_count"`
}

// milestoneToResponse maps one row, deriving the overdue flag against today.
//
// is_delayed is derived rather than stored. The column exists and defaults to
// false, and nothing ever wrote it — so every overdue badge on the detail page
// and every late marker on the roadmap stayed dark, which is the one state
// this feature most needs to show. Deriving it on read also means it cannot
// drift: a milestone that becomes overdue at midnight is overdue on the next
// read, with no sweeper to schedule, miss, or fall behind.
func milestoneToResponse(m db.Milestone, today time.Time) MilestoneResponse {
	var n *int32
	if m.N.Valid {
		v := m.N.Int32
		n = &v
	}
	return MilestoneResponse{
		ID:                  uuidToString(m.ID),
		WorkspaceID:         uuidToString(m.WorkspaceID),
		GoalID:              uuidToString(m.GoalID),
		Type:                m.Type,
		N:                   n,
		Title:               m.Title,
		ValueStatement:      m.ValueStatement,
		OriginalPlannedDate: dateToPtr(m.OriginalPlannedDate),
		PlannedDate:         dateToPtr(m.PlannedDate),
		ActualDate:          dateToPtr(m.ActualDate),
		Status:              m.Status,
		IsDelayed:           isMilestoneDelayed(m, today),
		AdoptionCheck:       textToPtr(m.AdoptionCheck),
		VerifierType:        textToPtr(m.VerifierType),
		VerifierID:          uuidToPtr(m.VerifierID),
		VerifierLabel:       m.VerifierLabel,
		AcceptedByType:      textToPtr(m.AcceptedByType),
		AcceptedByID:        uuidToPtr(m.AcceptedByID),
		AcceptNote:          m.AcceptNote,
		AcceptedAt:          timestampToString(m.AcceptedAt),
		IsRetro:             m.IsRetro,
		CreatedAt:           timestampToString(m.CreatedAt),
		UpdatedAt:           timestampToString(m.UpdatedAt),
		RequiresAcceptance:  goalrules.RequiresAcceptance(goalrules.MilestoneType(m.Type)),
	}
}

type CreateMilestoneRequest struct {
	Type           string          `json:"type"`
	N              int             `json:"n"`
	Title          string          `json:"title"`
	ValueStatement string          `json:"value_statement"`
	PlannedDate    string          `json:"planned_date"`
	ActualDate     *string         `json:"actual_date"`
	Status         string          `json:"status"`
	AdoptionCheck  *string         `json:"adoption_check"`
	AdoptionConfig json.RawMessage `json:"adoption_config"`
	VerifierType   *string         `json:"verifier_type"`
	VerifierID     *string         `json:"verifier_id"`
	VerifierLabel  string          `json:"verifier_label"`
	IsRetro        bool            `json:"is_retro"`
}

func (h *Handler) ListGoalMilestones(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	milestones, err := h.Queries.ListMilestonesForGoal(r.Context(), db.ListMilestonesForGoalParams{
		GoalID: g.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list milestones")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"milestones": h.milestoneResponses(r, wsUUID, milestones),
		"total":      len(milestones),
	})
}

// milestoneResponses maps rows and fills the reschedule counters in one batch.
//
// A failure fetching the counters leaves them at zero rather than failing the
// request: the counter is a badge, and a list that renders without it beats a
// list that does not render.
func (h *Handler) milestoneResponses(r *http.Request, wsUUID pgtype.UUID, milestones []db.Milestone) []MilestoneResponse {
	today := time.Now().UTC()
	resp := make([]MilestoneResponse, len(milestones))
	ids := make([]pgtype.UUID, len(milestones))
	for i, m := range milestones {
		resp[i] = milestoneToResponse(m, today)
		ids[i] = m.ID
	}
	if len(ids) == 0 {
		return resp
	}
	rows, err := h.Queries.CountMilestoneDateChanges(r.Context(), db.CountMilestoneDateChangesParams{
		WorkspaceID: wsUUID, MilestoneIds: ids,
	})
	if err != nil {
		return resp
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[uuidToString(row.MilestoneID)] = row.ChangeCount
	}
	for i := range resp {
		resp[i].DateChangeCount = counts[resp[i].ID]
	}
	return resp
}

func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	g, wsUUID, ok := h.loadGoal(w, r)
	if !ok {
		return
	}
	var req CreateMilestoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	plannedDate, err := util.ParseCalendarDate(req.PlannedDate)
	hasPlanned := req.PlannedDate != "" && err == nil
	if req.PlannedDate != "" && err != nil {
		writeError(w, http.StatusBadRequest, "invalid planned_date format, expected YYYY-MM-DD")
		return
	}

	in := goalrules.MilestoneInput{
		Type:           goalrules.MilestoneType(req.Type),
		N:              req.N,
		Title:          req.Title,
		HasPlannedDate: hasPlanned,
		Adoption:       goalrules.AdoptionCheck(strFromPtr(req.AdoptionCheck)),
		HasVerifier:    req.VerifierType != nil && *req.VerifierType != "",
	}
	if hasPlanned {
		in.PlannedDate = plannedDate.Time
	}
	if writeGoalRuleError(w, goalrules.ValidateMilestone(in)) {
		return
	}

	status := req.Status
	if status == "" {
		status = string(goalrules.MilestonePlanned)
	}
	var actualDate pgtype.Date
	if req.ActualDate != nil && *req.ActualDate != "" {
		d, err := util.ParseCalendarDate(*req.ActualDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid actual_date format, expected YYYY-MM-DD")
			return
		}
		actualDate = d
	}
	// Recording a milestone that already happened is a first-class path, not a
	// workaround: work ships before anyone files a goal for it, and the choice
	// is between capturing that or pretending it did not happen. The row is
	// flagged so the planned-versus-unplanned mix stays honest.
	if status == string(goalrules.MilestoneAchieved) && !actualDate.Valid {
		writeError(w, http.StatusBadRequest, "recording a milestone as achieved requires the date it happened")
		return
	}

	creatorType, creatorID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))

	// Creation is a second door into `achieved`, and it has to be locked the
	// same way as the first. Without this an agent could POST a first_use
	// milestone that is already achieved and certify its own adoption in one
	// request — exactly what UpdateMilestoneStatus and DecideMilestoneProposal
	// refuse, reached by a route that never asked.
	//
	// The rule is not "agents may not backfill". An agent may record a launch
	// that already shipped, because a launch is settled by the engineering
	// record. It may not record that something was used.
	if status == string(goalrules.MilestoneAchieved) &&
		goalrules.RequiresAcceptance(in.Type) &&
		!goalrules.CanDecide(goalrules.ProposalAccepted, goalrules.ActorType(creatorType)) {
		writeError(w, http.StatusForbidden,
			"an agent cannot record an adoption milestone as already reached; propose it instead")
		return
	}

	var n pgtype.Int4
	if in.Type == goalrules.MilestoneNthUse {
		n = pgtype.Int4{Int32: int32(req.N), Valid: true}
	}
	var adoption pgtype.Text
	if req.AdoptionCheck != nil && *req.AdoptionCheck != "" {
		adoption = pgtype.Text{String: *req.AdoptionCheck, Valid: true}
	}
	adoptionConfig := []byte("{}")
	if len(req.AdoptionConfig) > 0 {
		adoptionConfig = req.AdoptionConfig
	}
	var verifierType pgtype.Text
	if req.VerifierType != nil && *req.VerifierType != "" {
		verifierType = pgtype.Text{String: *req.VerifierType, Valid: true}
	}
	var verifierID pgtype.UUID
	if req.VerifierID != nil && *req.VerifierID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.VerifierID, "verifier_id")
		if !ok {
			return
		}
		verifierID = id
	}
	// An external verifier has no workspace seat and therefore no id; the label
	// is the only thing identifying them, so it is required in that case.
	if verifierType.String == "external" && req.VerifierLabel == "" {
		writeError(w, http.StatusBadRequest, "an external verifier needs a name")
		return
	}

	creatorUUID, ok := parseUUIDOrBadRequest(w, creatorID, "creator_id")
	if !ok {
		return
	}

	m, err := h.Queries.CreateMilestone(r.Context(), db.CreateMilestoneParams{
		WorkspaceID:    wsUUID,
		GoalID:         g.ID,
		Type:           req.Type,
		N:              n,
		Title:          req.Title,
		ValueStatement: req.ValueStatement,
		Status:         status,
		AdoptionCheck:  adoption,
		AdoptionConfig: adoptionConfig,
		VerifierType:   verifierType,
		VerifierID:     verifierID,
		VerifierLabel:  req.VerifierLabel,
		ActualDate:     actualDate,
		IsRetro:        req.IsRetro,
		CreatedByType:  creatorType,
		CreatedByID:    creatorUUID,
		PlannedDate:    plannedDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create milestone")
		return
	}
	resp := milestoneToResponse(m, time.Now().UTC())
	h.publishGoalChange(r, protocol.EventMilestoneCreated, map[string]any{
		"goal_id":   uuidToString(g.ID),
		"milestone": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) loadMilestone(w http.ResponseWriter, r *http.Request) (db.Milestone, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.Milestone{}, wsUUID, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "milestoneId"), "milestone_id")
	if !ok {
		return db.Milestone{}, wsUUID, false
	}
	m, err := h.Queries.GetMilestoneInWorkspace(r.Context(), db.GetMilestoneInWorkspaceParams{
		ID: id, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "milestone not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load milestone")
		}
		return db.Milestone{}, wsUUID, false
	}
	return m, wsUUID, true
}

func (h *Handler) GetMilestone(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.milestoneResponses(r, wsUUID, []db.Milestone{m})[0])
}

// ListMilestonesTimeline serves the roadmap: one workspace, one date window,
// every goal at once. The window is on planned_date so an unfinished milestone
// keeps its slot on the chart instead of vanishing until it lands.
func (h *Handler) ListMilestonesTimeline(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	fromRaw := r.URL.Query().Get("from")
	toRaw := r.URL.Query().Get("to")
	if fromRaw == "" || toRaw == "" {
		writeError(w, http.StatusBadRequest, "from and to are required, as YYYY-MM-DD")
		return
	}
	from, err := util.ParseCalendarDate(fromRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from format, expected YYYY-MM-DD")
		return
	}
	to, err := util.ParseCalendarDate(toRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to format, expected YYYY-MM-DD")
		return
	}
	if to.Time.Before(from.Time) {
		writeError(w, http.StatusBadRequest, "to must not be before from")
		return
	}
	milestones, err := h.Queries.ListMilestonesForTimeline(r.Context(), db.ListMilestonesForTimelineParams{
		WorkspaceID: wsUUID, FromDate: from, ToDate: to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load timeline")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"milestones": h.milestoneResponses(r, wsUUID, milestones),
		"total":      len(milestones),
	})
}

type UpdateMilestoneStatusRequest struct {
	Status     string  `json:"status"`
	ActualDate *string `json:"actual_date"`
	AcceptNote string  `json:"accept_note"`
}

// UpdateMilestoneStatus moves a milestone through its state machine.
//
// Acceptance is recorded on the same write that reaches `achieved`, because
// the two are the same event: someone said this is genuinely done. Splitting them would
// let a milestone sit as achieved with nobody attached to the claim.
func (h *Handler) UpdateMilestoneStatus(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	var req UpdateMilestoneStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	from := goalrules.MilestoneStatus(m.Status)
	to := goalrules.MilestoneStatus(req.Status)
	if !goalrules.CanTransitionFor(goalrules.MilestoneType(m.Type), from, to) {
		writeError(w, http.StatusBadRequest, "a milestone at "+m.Status+" cannot move to "+req.Status)
		return
	}

	params := db.UpdateMilestoneStatusParams{
		ID: m.ID, WorkspaceID: wsUUID, Status: req.Status,
		// The status this transition was validated against. If someone else
		// moved the milestone in between, no row matches and the write is
		// refused rather than applied to a state nobody checked.
		ExpectedStatus: m.Status,
	}
	if req.ActualDate != nil && *req.ActualDate != "" {
		d, err := util.ParseCalendarDate(*req.ActualDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid actual_date format, expected YYYY-MM-DD")
			return
		}
		params.ActualDate = d
	}

	if to == goalrules.MilestoneAchieved {
		if !params.ActualDate.Valid && !m.ActualDate.Valid {
			writeError(w, http.StatusBadRequest, "marking a milestone achieved requires the date it happened")
			return
		}
		actorType, actorID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
		// An adoption milestone is a claim about the world outside the
		// repository. An agent may propose it (see CreateMilestoneProposal) but
		// may not be the one who settles it.
		if goalrules.RequiresAcceptance(goalrules.MilestoneType(m.Type)) &&
			!goalrules.CanDecide(goalrules.ProposalAccepted, goalrules.ActorType(actorType)) {
			writeError(w, http.StatusForbidden, "an agent cannot accept a milestone; propose it instead")
			return
		}
		actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor_id")
		if !ok {
			return
		}
		params.AcceptedByType = pgtype.Text{String: actorType, Valid: true}
		params.AcceptedByID = actorUUID
		params.AcceptNote = pgtype.Text{String: req.AcceptNote, Valid: true}
		params.AcceptedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}

	updated, err := h.Queries.UpdateMilestoneStatus(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "the milestone has moved since this was read")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update milestone")
		return
	}
	h.publishGoalChange(r, protocol.EventMilestoneUpdated, map[string]any{
		"goal_id":      uuidToString(m.GoalID),
		"milestone_id": uuidToString(m.ID),
	})
	// Through milestoneResponses, not the bare mapper: a client that renders
	// from the mutation response has to see the same milestone the next GET
	// would give it, counters included.
	writeJSON(w, http.StatusOK, h.milestoneResponses(r, wsUUID, []db.Milestone{updated})[0])
}

type RescheduleMilestoneRequest struct {
	PlannedDate string `json:"planned_date"`
	Reason      string `json:"reason"`
}

// RescheduleMilestone moves a date and records why, in one transaction.
//
// The reason is mandatory and the log is append-only, which together implement
// the rule that the system never silently swallows an overdue milestone. What
// it does NOT do is touch original_planned_date, so rescheduling stays free of
// blame and free of effect on the on-time statistic.
func (h *Handler) RescheduleMilestone(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	var req RescheduleMilestoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "moving a date requires a reason")
		return
	}
	newDate, err := util.ParseCalendarDate(req.PlannedDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid planned_date format, expected YYYY-MM-DD")
		return
	}
	if m.PlannedDate.Valid && newDate.Time.Equal(m.PlannedDate.Time) {
		writeError(w, http.StatusBadRequest, "the milestone is already planned for that date")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor_id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reschedule milestone")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Re-read the date under a row lock. The copy loaded before the
	// transaction is a snapshot: two concurrent reschedules both saw the
	// original date and each recorded a move from it, so the log claimed two
	// moves out of a state only one of them was ever in.
	locked, err := qtx.LockMilestoneForUpdate(r.Context(), db.LockMilestoneForUpdateParams{
		ID: m.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reschedule milestone")
		return
	}
	if locked.PlannedDate.Valid && newDate.Time.Equal(locked.PlannedDate.Time) {
		writeError(w, http.StatusConflict, "the milestone is already planned for that date")
		return
	}

	if _, err := qtx.CreateMilestoneDateChange(r.Context(), db.CreateMilestoneDateChangeParams{
		WorkspaceID:   wsUUID,
		MilestoneID:   m.ID,
		FromDate:      locked.PlannedDate,
		ToDate:        newDate,
		Reason:        req.Reason,
		ChangedByType: actorType,
		ChangedByID:   actorUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the reschedule")
		return
	}
	updated, err := qtx.RescheduleMilestone(r.Context(), db.RescheduleMilestoneParams{
		ID: m.ID, WorkspaceID: wsUUID, PlannedDate: newDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reschedule milestone")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reschedule milestone")
		return
	}
	h.publishGoalChange(r, protocol.EventMilestoneUpdated, map[string]any{
		"goal_id":      uuidToString(m.GoalID),
		"milestone_id": uuidToString(m.ID),
	})
	// Counted after the commit, so the "moved n times" badge the caller draws
	// from this response already includes the move it just made. Reading it
	// inside the transaction would return the pre-insert count.
	writeJSON(w, http.StatusOK, h.milestoneResponses(r, wsUUID, []db.Milestone{updated})[0])
}

func (h *Handler) ListMilestoneDateChanges(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	changes, err := h.Queries.ListMilestoneDateChanges(r.Context(), db.ListMilestoneDateChangesParams{
		MilestoneID: m.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reschedules")
		return
	}
	// Who may read the reasons.
	//
	// The dates and the count are the record and everyone sees them: an
	// overdue milestone must never disappear quietly. The reasons are not the
	// same thing. They are only worth collecting while the person writing one
	// is explaining a plan rather than defending a record, and a log the whole
	// workspace reads is a log that fills with "delay" within a quarter.
	//
	// So the reason goes to the two people who need it to act on it — the
	// goal's owner, and whoever runs the review — and to nobody else. This is
	// the same decision as the metrics carrying no per-person dimension, and
	// it is deliberately not configurable.
	reasonsVisible := h.canReadRescheduleReasons(r, wsUUID, m.GoalID)

	out := make([]map[string]any, len(changes))
	for i, c := range changes {
		row := map[string]any{
			"id":         uuidToString(c.ID),
			"from_date":  dateToPtr(c.FromDate),
			"to_date":    dateToPtr(c.ToDate),
			"created_at": timestampToString(c.CreatedAt),
		}
		if reasonsVisible {
			row["reason"] = c.Reason
			row["changed_by_type"] = c.ChangedByType
			row["changed_by_id"] = uuidToString(c.ChangedByID)
		}
		out[i] = row
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date_changes":    out,
		"total":           len(out),
		"reasons_visible": reasonsVisible,
	})
}

type CreateMilestoneReleaseRequest struct {
	Kind              string  `json:"kind"`
	RepoURL           string  `json:"repo_url"`
	Ref               string  `json:"ref"`
	Tag               string  `json:"tag"`
	ReleasedAt        *string `json:"released_at"`
	PullRequestID     *string `json:"pull_request_id"`
	PullRequestSource *string `json:"pull_request_source"`
	Summary           string  `json:"summary"`
}

// CreateMilestoneRelease records what shipped for a milestone.
//
// Either a typed-in release or a pointer at a pull request row this server
// already holds. The second is the path that closes the loop with agent work:
// the agent finished an issue, the issue already carries its PR, and the same
// row is attached here — nothing is retyped for the engineering fact and the
// delivery record to agree.
func (h *Handler) CreateMilestoneRelease(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	var req CreateMilestoneReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Kind {
	case "main", "hotfix", "pending":
	default:
		writeError(w, http.StatusBadRequest, "kind must be main, hotfix or pending")
		return
	}

	var prID pgtype.UUID
	var prSource pgtype.Text
	if req.PullRequestID != nil && *req.PullRequestID != "" {
		if req.PullRequestSource == nil {
			writeError(w, http.StatusBadRequest, "pull_request_source is required with pull_request_id")
			return
		}
		switch *req.PullRequestSource {
		case "github", "vcs":
		default:
			writeError(w, http.StatusBadRequest, "pull_request_source must be github or vcs")
			return
		}
		id, ok := parseUUIDOrBadRequest(w, *req.PullRequestID, "pull_request_id")
		if !ok {
			return
		}
		prID = id
		prSource = pgtype.Text{String: *req.PullRequestSource, Valid: true}
	}
	if !prID.Valid && req.RepoURL == "" && req.Tag == "" {
		writeError(w, http.StatusBadRequest, "a release must name a pull request, a repository or a tag")
		return
	}

	var releasedAt pgtype.Date
	if req.ReleasedAt != nil && *req.ReleasedAt != "" {
		d, err := util.ParseCalendarDate(*req.ReleasedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid released_at format, expected YYYY-MM-DD")
			return
		}
		releasedAt = d
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor_id")
	if !ok {
		return
	}

	params := db.CreateMilestoneReleaseParams{
		WorkspaceID:       wsUUID,
		MilestoneID:       m.ID,
		Kind:              req.Kind,
		RepoUrl:           req.RepoURL,
		Ref:               req.Ref,
		Tag:               req.Tag,
		ReleasedAt:        releasedAt,
		PullRequestID:     prID,
		PullRequestSource: prSource,
		Summary:           req.Summary,
		CreatedByType:     actorType,
		CreatedByID:       actorUUID,
	}
	// Authorship of the notes is recorded only when there are notes, so an
	// empty summary does not claim an author who wrote nothing.
	if req.Summary != "" {
		params.SummaryByType = pgtype.Text{String: actorType, Valid: true}
		params.SummaryByID = actorUUID
		params.SummaryAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}

	rel, err := h.Queries.CreateMilestoneRelease(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the release")
		return
	}
	h.publishGoalChange(r, protocol.EventMilestoneUpdated, map[string]any{
		"goal_id":      uuidToString(m.GoalID),
		"milestone_id": uuidToString(m.ID),
	})
	writeJSON(w, http.StatusCreated, releaseToResponse(rel))
}

func releaseToResponse(rel db.MilestoneRelease) map[string]any {
	return map[string]any{
		"id":                  uuidToString(rel.ID),
		"milestone_id":        uuidToString(rel.MilestoneID),
		"kind":                rel.Kind,
		"repo_url":            rel.RepoUrl,
		"ref":                 rel.Ref,
		"tag":                 rel.Tag,
		"released_at":         dateToPtr(rel.ReleasedAt),
		"pull_request_id":     uuidToPtr(rel.PullRequestID),
		"pull_request_source": textToPtr(rel.PullRequestSource),
		"summary":             rel.Summary,
		"summary_by_type":     textToPtr(rel.SummaryByType),
		"summary_by_id":       uuidToPtr(rel.SummaryByID),
		"created_at":          timestampToString(rel.CreatedAt),
	}
}

func (h *Handler) ListMilestoneReleases(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	releases, err := h.Queries.ListMilestoneReleases(r.Context(), db.ListMilestoneReleasesParams{
		MilestoneID: m.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list releases")
		return
	}
	out := make([]map[string]any, len(releases))
	for i, rel := range releases {
		out[i] = releaseToResponse(rel)
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out, "total": len(out)})
}

type CreateMilestoneProposalRequest struct {
	ProposedStatus     string          `json:"proposed_status"`
	ProposedActualDate *string         `json:"proposed_actual_date"`
	Evidence           string          `json:"evidence"`
	EvidenceRefs       json.RawMessage `json:"evidence_refs"`
	SourceTaskID       *string         `json:"source_task_id"`
	SourceIssueID      *string         `json:"source_issue_id"`
}

func proposalToResponse(p db.MilestoneProposal) map[string]any {
	return map[string]any{
		"id":                   uuidToString(p.ID),
		"milestone_id":         uuidToString(p.MilestoneID),
		"proposed_status":      p.ProposedStatus,
		"proposed_actual_date": dateToPtr(p.ProposedActualDate),
		"evidence":             p.Evidence,
		"evidence_refs":        json.RawMessage(p.EvidenceRefs),
		"proposed_by_type":     p.ProposedByType,
		"proposed_by_id":       uuidToString(p.ProposedByID),
		"source_task_id":       uuidToPtr(p.SourceTaskID),
		"source_issue_id":      uuidToPtr(p.SourceIssueID),
		"state":                p.State,
		"decided_by_type":      textToPtr(p.DecidedByType),
		"decided_by_id":        uuidToPtr(p.DecidedByID),
		"decided_at":           timestampToString(p.DecidedAt),
		"decide_note":          p.DecideNote,
		"created_at":           timestampToString(p.CreatedAt),
	}
}

// CreateMilestoneProposal records a claim that a milestone has been reached,
// with the evidence behind it, and changes nothing about the milestone.
//
// This is the endpoint an agent calls after finishing the work, and it is the
// mechanism that makes the goal layer affordable: every product-management
// tool before this one failed because the person paying the data-entry cost
// was not the person getting the benefit. Here the cost moves to the agent
// that already did the work and already holds the evidence.
//
// A newer proposal retires the pending ones for the same milestone, so the
// reviewer sees one current claim rather than a pile of stale ones.
func (h *Handler) CreateMilestoneProposal(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	var req CreateMilestoneProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	in := goalrules.ProposalInput{
		From:     goalrules.MilestoneStatus(m.Status),
		To:       goalrules.MilestoneStatus(req.ProposedStatus),
		Evidence: req.Evidence,
		HasDate:  req.ProposedActualDate != nil && *req.ProposedActualDate != "",
	}
	if writeGoalRuleError(w, goalrules.ValidateProposal(in)) {
		return
	}

	var actualDate pgtype.Date
	if in.HasDate {
		d, err := util.ParseCalendarDate(*req.ProposedActualDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid proposed_actual_date format, expected YYYY-MM-DD")
			return
		}
		actualDate = d
	}

	// Provenance back into execution. Both are optional because a proposal can
	// also come from a scheduled sweep rather than a single run, but when they
	// are present they are what lets a reviewer open the execution log and the
	// token cost sitting behind a one-line claim that a feature is live.
	var sourceTaskID, sourceIssueID pgtype.UUID
	if req.SourceTaskID != nil && *req.SourceTaskID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.SourceTaskID, "source_task_id")
		if !ok {
			return
		}
		sourceTaskID = id
	}
	if req.SourceIssueID != nil && *req.SourceIssueID != "" {
		issue, ok := h.loadIssueForUser(w, r, *req.SourceIssueID)
		if !ok {
			return
		}
		sourceIssueID = issue.ID
	}

	proposerType, proposerID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
	proposerUUID, ok := parseUUIDOrBadRequest(w, proposerID, "proposer_id")
	if !ok {
		return
	}
	refs := []byte("[]")
	if len(req.EvidenceRefs) > 0 {
		refs = req.EvidenceRefs
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the proposal")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Serialize on the milestone. Two proposals arriving together would
	// otherwise each insert and then retire only what was already committed,
	// leaving both pending — and the queue is supposed to hold one current
	// claim so two reviewers cannot accept competing ones.
	if _, err := qtx.LockMilestoneForUpdate(r.Context(), db.LockMilestoneForUpdateParams{
		ID: m.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the proposal")
		return
	}

	p, err := qtx.CreateMilestoneProposal(r.Context(), db.CreateMilestoneProposalParams{
		WorkspaceID:        wsUUID,
		MilestoneID:        m.ID,
		ProposedStatus:     req.ProposedStatus,
		ProposedActualDate: actualDate,
		Evidence:           req.Evidence,
		EvidenceRefs:       refs,
		ProposedByType:     proposerType,
		ProposedByID:       proposerUUID,
		SourceTaskID:       sourceTaskID,
		SourceIssueID:      sourceIssueID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the proposal")
		return
	}
	// Superseding is bookkeeping rather than a judgement, which is why the
	// proposer is recorded as having done it and no human is involved.
	if err := qtx.SupersedePendingProposals(r.Context(), db.SupersedePendingProposalsParams{
		WorkspaceID:   wsUUID,
		MilestoneID:   m.ID,
		KeepID:        p.ID,
		DecidedByType: pgtype.Text{String: proposerType, Valid: true},
		DecidedByID:   proposerUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retire earlier proposals")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the proposal")
		return
	}
	// The claim has to reach whoever can judge it, not only whoever happens to
	// reload the page. This is the event behind the inbox badge.
	h.publishGoalChange(r, protocol.EventMilestoneProposed, map[string]any{
		"goal_id":      uuidToString(m.GoalID),
		"milestone_id": uuidToString(m.ID),
		"proposal_id":  uuidToString(p.ID),
	})
	writeJSON(w, http.StatusCreated, proposalToResponse(p))
}

func (h *Handler) ListMilestoneProposals(w http.ResponseWriter, r *http.Request) {
	m, wsUUID, ok := h.loadMilestone(w, r)
	if !ok {
		return
	}
	proposals, err := h.Queries.ListMilestoneProposals(r.Context(), db.ListMilestoneProposalsParams{
		MilestoneID: m.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list proposals")
		return
	}
	out := make([]map[string]any, len(proposals))
	for i, p := range proposals {
		out[i] = proposalToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": out, "total": len(out)})
}

// ListPendingMilestoneProposals is the workspace-wide queue behind the inbox
// badge: every claim still waiting on a human.
func (h *Handler) ListPendingMilestoneProposals(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	proposals, err := h.Queries.ListPendingMilestoneProposals(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list proposals")
		return
	}
	out := make([]map[string]any, len(proposals))
	for i, p := range proposals {
		out[i] = proposalToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": out, "total": len(out)})
}

type DecideMilestoneProposalRequest struct {
	State      string `json:"state"`
	DecideNote string `json:"decide_note"`
}

// DecideMilestoneProposal settles a pending claim, and on acceptance applies it
// to the milestone in the same transaction.
//
// The rule this endpoint exists to enforce is that an agent may never accept,
// including its own proposal. A milestone is an assertion that something is
// genuinely done or genuinely used, and a system where the party doing the work
// also certifies the work has stopped measuring anything. Rejection is a human
// judgement for the same reason.
func (h *Handler) DecideMilestoneProposal(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	proposalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "proposalId"), "proposal_id")
	if !ok {
		return
	}
	var req DecideMilestoneProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	state := goalrules.ProposalState(req.State)
	if state != goalrules.ProposalAccepted && state != goalrules.ProposalRejected {
		writeError(w, http.StatusBadRequest, "state must be accepted or rejected")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	deciderType, deciderID := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
	if !goalrules.CanDecide(state, goalrules.ActorType(deciderType)) {
		writeError(w, http.StatusForbidden, "an agent cannot decide a milestone proposal; a person has to")
		return
	}
	deciderUUID, ok := parseUUIDOrBadRequest(w, deciderID, "decider_id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decide the proposal")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Guarded on state = 'pending' inside the query, so two reviewers racing
	// cannot both decide; the loser gets no row and is told it is already
	// settled rather than silently overwriting the winner.
	decided, err := qtx.DecideMilestoneProposal(r.Context(), db.DecideMilestoneProposalParams{
		ID:            proposalID,
		WorkspaceID:   wsUUID,
		State:         req.State,
		DecidedByType: pgtype.Text{String: deciderType, Valid: true},
		DecidedByID:   deciderUUID,
		DecideNote:    req.DecideNote,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "that proposal is not pending any more")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to decide the proposal")
		return
	}

	var milestone db.Milestone
	if state == goalrules.ProposalAccepted {
		m, err := qtx.GetMilestoneInWorkspace(r.Context(), db.GetMilestoneInWorkspaceParams{
			ID: decided.MilestoneID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load the milestone")
			return
		}
		// The milestone may have moved since the proposal was written. Refusing
		// here rather than forcing the transition keeps the human's decision
		// from being applied to a state they were not looking at.
		if !goalrules.CanTransitionFor(goalrules.MilestoneType(m.Type),
			goalrules.MilestoneStatus(m.Status), goalrules.MilestoneStatus(decided.ProposedStatus)) {
			writeError(w, http.StatusConflict,
				"the milestone has moved to "+m.Status+" since this was proposed")
			return
		}
		params := db.UpdateMilestoneStatusParams{
			ID:             m.ID,
			WorkspaceID:    wsUUID,
			Status:         decided.ProposedStatus,
			ExpectedStatus: m.Status,
			ActualDate:     decided.ProposedActualDate,
			AcceptedByType: pgtype.Text{String: deciderType, Valid: true},
			AcceptedByID:   deciderUUID,
			AcceptNote:     pgtype.Text{String: req.DecideNote, Valid: true},
			AcceptedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}
		milestone, err = qtx.UpdateMilestoneStatus(r.Context(), params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply the proposal")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decide the proposal")
		return
	}

	h.publishGoalChange(r, protocol.EventMilestoneDecided, map[string]any{
		"milestone_id": uuidToString(decided.MilestoneID),
		"proposal_id":  uuidToString(decided.ID),
		"state":        decided.State,
	})

	resp := map[string]any{"proposal": proposalToResponse(decided)}
	if state == goalrules.ProposalAccepted {
		resp["milestone"] = h.milestoneResponses(r, wsUUID, []db.Milestone{milestone})[0]
	}
	writeJSON(w, http.StatusOK, resp)
}

// isMilestoneDelayed reports whether an unfinished milestone has passed its
// planned date. The rule itself lives in internal/goal, which owns it for the
// client and the server alike; this only adapts the row's column types.
func isMilestoneDelayed(m db.Milestone, today time.Time) bool {
	if !m.PlannedDate.Valid {
		return false
	}
	return goalrules.IsDelayed(goalrules.MilestoneStatus(m.Status), m.PlannedDate.Time, today)
}

// canReadRescheduleReasons reports whether this caller may see why dates moved.
//
// True for the goal's owner and for a workspace owner or admin. Everyone else
// sees the dates and the count, which is all the record needs to be honest.
// See the comment at the call site for why this is not a setting.
func (h *Handler) canReadRescheduleReasons(r *http.Request, wsUUID, goalID pgtype.UUID) bool {
	workspaceID := h.resolveWorkspaceID(r)
	userID := requestUserID(r)
	if workspaceID == "" || userID == "" {
		return false
	}

	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}

	goal, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{
		ID: goalID, WorkspaceID: wsUUID,
	})
	if err != nil {
		return false
	}
	return goal.OwnerType.String == "member" && uuidToString(goal.OwnerID) == userID
}
