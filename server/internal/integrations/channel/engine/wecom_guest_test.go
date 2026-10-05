package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelaccess"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"testing"
)

func TestGuestCannotUseDirectIssueCommand(t *testing.T) {
	h := newHarness(t)
	h.ident.id.WecomGuest = &channelaccess.WecomGuest{SenderID: "visitor"}
	msg := p2pMessage(t)
	msg.Text = "/issue change production"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if h.binder.ensureCalls != 0 || h.binder.startCalls != 0 {
		t.Fatal("guest issue command reached session pipeline")
	}
	h.router.Drain(context.Background())
	if got := h.replier.calls(); len(got) != 1 || got[0].Outcome != OutcomeGuestCommandDenied {
		t.Fatalf("outcome: %+v", got)
	}
}

func TestGuestMetadataReachesSessionBinder(t *testing.T) {
	h := newHarness(t)
	guest := &channelaccess.WecomGuest{SenderID: "visitor"}
	h.ident.id.WecomGuest = guest
	msg := p2pMessage(t)
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	h.router.Drain(context.Background())
	if h.binder.lastEnsure.WecomGuest != guest {
		t.Fatal("external identity lost")
	}
}

// A separate sponsor may own the bot after another administrator installed it.
func TestGuestGroupUsesSponsorForNormalAndNewSessions(t *testing.T) {
	for _, text := range []string{"question", "/new question"} {
		t.Run(text, func(t *testing.T) {
			h := newHarness(t)
			guest := &channelaccess.WecomGuest{SenderID: "visitor"}
			h.ident.id.WecomGuest = guest
			msg := p2pMessage(t)
			msg.Source.ChatType = channel.ChatTypeGroup
			msg.AddressedToBot = true
			msg.Text = text
			probe := &guestContextBinder{fakeBinder: h.binder}
			h.router.Register(msg.Source.ChannelType, ResolverSet{Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: probe, Audit: h.audit, Replier: h.replier})
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.router.Drain(context.Background())
			if probe.guest != guest {
				t.Fatal("guest authorization context lost")
			}
			if text == "question" {
				if h.binder.lastEnsure.WecomGuest != guest || h.binder.lastEnsure.Sender != h.ident.id.UserID {
					t.Fatal("normal route lost guest or sponsor")
				}
			} else {
				if h.binder.lastStart.WecomGuest != guest || h.binder.lastStart.Creator != h.ident.id.UserID || h.binder.lastStart.Sender != h.ident.id.UserID {
					t.Fatal("new route lost guest or sponsor")
				}
			}
		})
	}
}

type guestContextBinder struct {
	*fakeBinder
	guest *channelaccess.WecomGuest
}

func (b *guestContextBinder) EnsureSession(ctx context.Context, p EnsureSessionParams) (pgtype.UUID, error) {
	b.guest = channelaccess.WecomGuestFromContext(ctx)
	return b.fakeBinder.EnsureSession(ctx, p)
}
func (b *guestContextBinder) StartSession(ctx context.Context, p StartSessionParams) (StartSessionResult, error) {
	b.guest = channelaccess.WecomGuestFromContext(ctx)
	return b.fakeBinder.StartSession(ctx, p)
}

// Access can be revoked at identity resolution or at a later transactional
// check. Neither is a transport failure; database errors must remain retryable.
func TestGuestAccessDeniedDoesNotFailConnector(t *testing.T) {
	for _, stage := range []string{"identity", "session", "new_prepare", "new_transaction"} {
		for _, denied := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/denied=%t", stage, denied), func(t *testing.T) {
				h := newHarness(t)
				cause := error(context.DeadlineExceeded)
				if denied {
					cause = channelaccess.ErrWecomAccessDenied
				}
				failure := fmt.Errorf("authorization lookup: %w", cause)
				msg := p2pMessage(t)
				switch stage {
				case "identity":
					h.ident.err = failure
				case "session":
					h.binder.ensureErr = failure
				case "new_prepare":
					msg.Text = "/new question"
					h.tasks.prepareErr = failure
				case "new_transaction":
					msg.Text = "/new question"
					h.binder.startErr = failure
				}
				err := h.router.Handle(context.Background(), msg)
				h.router.Drain(context.Background())
				if denied {
					if err != nil {
						t.Fatalf("authorization denial escaped to connector: %v", err)
					}
					if h.dedup.marks() != 1 || h.dedup.releases() != 0 {
						t.Fatal("denied callback was left retryable")
					}
					if reason, ok := h.audit.last(); !ok || reason != DropReason("guest_access_denied") {
						t.Fatalf("missing denial audit: %v %v", reason, ok)
					}
					replies := h.replier.calls()
					if len(replies) != 1 || replies[0].Outcome != OutcomeDropped {
						t.Fatalf("unexpected outcome: %+v", replies)
					}
				} else {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("database failure swallowed: %v", err)
					}
					if h.dedup.marks() != 0 || h.dedup.releases() != 1 {
						t.Fatal("database failure cannot retry")
					}
				}
			})
		}
	}
}
