package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type guestLookupFailureDB struct{ db.DBTX }
type guestLookupFailureRow struct{}

func (guestLookupFailureRow) Scan(...any) error { return errors.New("temporary lookup failure") }
func (d guestLookupFailureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "GetChannelChatSessionBindingBySessionAny") {
		return guestLookupFailureRow{}
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

func TestGuestClaimFailureClassification(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	for _, infra := range []bool{false, true} {
		name := "revoked"
		if infra {
			name = "database failure"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "guest claim runtime")
			agentID := dbfx.Agent(t, "guest claim agent", runtimeID)
			sessionID := dbfx.ChatSession(t, agentID)
			cols := testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "dispatched", "dispatched_at": testutil.Raw("now()")}
			if !infra {
				cols["originator_source"] = channelaccess.WecomGuestSource
			}
			taskID := dbfx.Task(t, agentID, cols)
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			svc := service.NewTaskService(testHandler.Queries, testPool, testHandler.TaskService.Hub, testHandler.TaskService.Bus)
			h.TaskService = svc
			if infra {
				svc.Queries = db.New(guestLookupFailureDB{testPool})
			}
			req := newDaemonTokenRequest(http.MethodPost, "/claim", nil, testWorkspaceID, "guest-claim")
			_, _, _, _, failure := h.buildClaimedTaskResponse(req, &task, db.AgentRuntime{}, runtimeID, testWorkspaceID)
			if failure == nil {
				t.Fatal("claim unexpectedly dispatched")
			}
			wantStatus := http.StatusForbidden
			wantTaskStatus := "failed"
			if infra {
				wantStatus = http.StatusInternalServerError
				wantTaskStatus = "queued"
			}
			if failure.status != wantStatus {
				t.Fatalf("status %d want %d", failure.status, wantStatus)
			}
			stored, err := testHandler.Queries.GetAgentTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != wantTaskStatus {
				t.Fatalf("task status %q want %q", stored.Status, wantTaskStatus)
			}
		})
	}
}
