package wecom

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWecomSessionRouteIsolation(t *testing.T) {
	sources := []channel.Source{
		{ChatID: "group", SenderID: "alice", ChatType: channel.ChatTypeGroup},
		{ChatID: "group", SenderID: "bob", ChatType: channel.ChatTypeGroup},
		{ChatID: "other", SenderID: "alice", ChatType: channel.ChatTypeGroup},
		{ChatID: "a:b", SenderID: "c", ChatType: channel.ChatTypeGroup},
		{ChatID: "a", SenderID: "b:c", ChatType: channel.ChatTypeGroup},
	}
	seen := map[string]bool{}
	for _, source := range sources {
		key, config, err := wecomSessionRoute(source)
		if err != nil {
			t.Fatal(err)
		}
		if seen[key] || key == source.ChatID {
			t.Fatalf("shared routing key %q", key)
		}
		seen[key] = true
		again, _, _ := wecomSessionRoute(source)
		if again != key {
			t.Fatal("unstable key")
		}
		var metadata map[string]string
		if err := json.Unmarshal(config, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["chat_id"] != source.ChatID || metadata["sender_id"] != source.SenderID {
			t.Fatalf("lost source: %s", config)
		}
	}
	key, _, err := wecomSessionRoute(channel.Source{ChatID: "alice", SenderID: "alice", ChatType: channel.ChatTypeP2P})
	if err != nil || key != "alice" {
		t.Fatalf("p2p changed: %s %v", key, err)
	}
}

func TestSessionBinderRejectsMissingGroupSender(t *testing.T) {
	b := &sessionBinder{session: &fakeSessionBinder{}}
	msg := channel.InboundMessage{Source: channel.Source{ChatID: "group", ChatType: channel.ChatTypeGroup}}
	if _, err := b.EnsureSession(context.Background(), engine.EnsureSessionParams{Message: msg}); err == nil {
		t.Fatal("EnsureSession accepted missing sender")
	}
	if _, err := b.StartSession(context.Background(), engine.StartSessionParams{Message: msg}); err == nil {
		t.Fatal("StartSession accepted missing sender")
	}
}

func TestWecomBindingChatID(t *testing.T) {
	for _, tc := range []struct {
		name, key, config, want string
		bad                     bool
	}{
		{"legacy", "group", `{}`, "group", false},
		{"isolated", "wecom-group-v1:hash", `{"chat_id":"group","sender_id":"alice"}`, "group", false},
		{"missing target", "wecom-group-v1:hash", `{}`, "", true},
		{"corrupt target", "wecom-group-v1:hash", `{`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := wecomBindingChatID(db.ChannelChatSessionBinding{ChannelChatID: tc.key, Config: []byte(tc.config)})
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestIsolatedRouteDeliversTextAndFilesToRealGroup(t *testing.T) {
	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "result.txt", Url: "https://cdn.example/obj/abc", ContentType: "text/plain", SizeBytes: 3})
	key, config, err := wecomSessionRoute(channel.Source{ChatID: "real-group", SenderID: "alice", ChatType: channel.ChatTypeGroup})
	if err != nil {
		t.Fatal(err)
	}
	q.sessionBinding.ChannelChatID, q.sessionBinding.Config = key, config
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/abc", data: []byte("abc")})
	q.sessionBinding.InstallationID, q.installation.ID = instID, instID
	if err := o.processEvent(context.Background(), chatDoneEvent("answer")); err != nil {
		t.Fatal(err)
	}
	frames := conn.cmdFrames(cmdSendMsg)
	if len(frames) != 2 {
		t.Fatalf("want text and file, got %d", len(frames))
	}
	for _, frame := range frames {
		var body map[string]any
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body["chatid"] != "real-group" {
			t.Fatalf("synthetic route leaked: %v", body)
		}
	}
}

type routingRelayPublisher struct{ frame []byte }

func (p *routingRelayPublisher) PublishWithID(_, _, _ string, frame []byte, _ string) error {
	p.frame = frame
	return nil
}

func TestIsolatedRouteRelaysRealGroup(t *testing.T) {
	q := oneAttachmentQueries(t, db.Attachment{})
	key, config, _ := wecomSessionRoute(channel.Source{ChatID: "real-group", SenderID: "alice", ChatType: channel.ChatTypeGroup})
	q.sessionBinding.ChannelChatID, q.sessionBinding.Config = key, config
	o, instID, _ := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID, q.installation.ID = instID, instID
	o.senders = newSendersRegistry()
	publisher := &routingRelayPublisher{}
	o.relay = NewRelayOutbound(publisher, nil, RelayConfig{}, nil)
	if err := o.processEvent(context.Background(), chatDoneEvent("answer")); err != nil {
		t.Fatal(err)
	}
	var frame relayFrame
	if err := json.Unmarshal(publisher.frame, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.ChatID != "real-group" || frame.ChatType != chatTypeGroupInt {
		t.Fatalf("wrong relay destination: %+v", frame)
	}
}
