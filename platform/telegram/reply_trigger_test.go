package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type captureReplyBot struct {
	*stubTelegramBot
	sent     []*tgbot.SendMessageParams
	failHTML bool
	failLong bool
}

func (b *captureReplyBot) SendMessage(_ context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	cp := *p
	b.sent = append(b.sent, &cp)
	if b.failLong && len(b.sent) == 1 {
		return nil, errors.New("message is too long")
	}
	if b.failHTML && len(b.sent) == 1 {
		return nil, errors.New("can't parse entities")
	}
	return &models.Message{ID: 99}, nil
}
func TestSendReplyToTrigger(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		id       int
		fallback bool
	}{
		{"enabled", true, 42, false}, {"disabled", false, 42, false}, {"no trigger", true, 0, false}, {"plain fallback", true, 42, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := New(map[string]any{"token": "test", "allow_from": "1", "reply_to_trigger": tc.enabled})
			if err != nil {
				t.Fatal(err)
			}
			p := raw.(*Platform)
			b := &captureReplyBot{stubTelegramBot: newStubTelegramBot(), failHTML: tc.fallback}
			p.bot = b
			if err := p.Send(context.Background(), replyContext{chatID: 7, threadID: 4, messageID: tc.id}, "answer"); err != nil {
				t.Fatal(err)
			}
			if tc.fallback && len(b.sent) != 2 {
				t.Fatal("fallback not exercised")
			}
			for _, m := range b.sent {
				if m.ChatID != int64(7) || m.MessageThreadID != 4 {
					t.Fatal("routing changed")
				}
				r := m.ReplyParameters
				if tc.enabled && tc.id > 0 {
					if r == nil || r.MessageID != tc.id || !r.AllowSendingWithoutReply {
						t.Fatal("missing reply association")
					}
				} else if r != nil {
					t.Fatal("unexpected reply")
				}
			}
		})
	}
}

func TestPendingSessionRouteMatches(t *testing.T) {
	p := &Platform{}
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"telegram:-100:1", "telegram:-100:4:1", true},
		{"telegram:-100:4:1", "telegram:-100:5:1", true},
		{"telegram:-100:1", "telegram:-100:2", false},
		{"telegram:-100:1", "telegram:-200:1", false},
		{"telegram:-100:1", "other:-100:1", false},
		{"telegram:-100", "telegram:-100:1", false},
	} {
		if got := p.PendingSessionRouteMatches(tc.a, tc.b); got != tc.want {
			t.Errorf("match %q %q = %v", tc.a, tc.b, got)
		}
	}
}

func TestSendChunkedPreservesReplyToTriggerOption(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		raw, err := New(map[string]any{"token": "test", "allow_from": "1", "reply_to_trigger": enabled})
		if err != nil {
			t.Fatal(err)
		}
		p := raw.(*Platform)
		b := &captureReplyBot{stubTelegramBot: newStubTelegramBot(), failLong: true}
		p.bot = b
		if err := p.Send(context.Background(), replyContext{chatID: 7, threadID: 4, messageID: 42}, strings.Repeat("a", 5000)); err != nil {
			t.Fatal(err)
		}
		if len(b.sent) != 3 {
			t.Fatalf("expected original plus two chunks: %d", len(b.sent))
		}
		first := b.sent[1].ReplyParameters
		if enabled {
			if first == nil || first.MessageID != 42 || !first.AllowSendingWithoutReply {
				t.Fatal("chunk lost reply options")
			}
		} else if first != nil {
			t.Fatal("disabled Send quoted the trigger")
		}
		if b.sent[2].ReplyParameters != nil {
			t.Fatal("later chunk quoted trigger")
		}
	}
}
