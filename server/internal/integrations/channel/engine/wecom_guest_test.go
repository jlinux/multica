package engine

import (
	"context"
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
