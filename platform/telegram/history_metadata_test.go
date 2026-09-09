package telegram

import (
	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
	"strings"
	"testing"
)

func TestDispatchPreservesHistoryBodyAuthorTimeAndQuote(t *testing.T) {
	p := &Platform{selfUser: &models.User{ID: 1, FirstName: "NubeClaude"}}
	var got *core.Message
	p.handler = func(_ core.Platform, m *core.Message) { got = m }
	original := &models.Message{ID: 20, Text: "Long original response", From: &models.User{ID: 1, FirstName: "NubeClaude"}, Chat: models.Chat{ID: -10012345, Type: models.ChatTypeSupergroup}}
	p.dispatchMessage(&core.Message{Content: "My reply"}, &models.Message{Date: 1788911724, From: &models.User{ID: 7, FirstName: "B", LastName: "User"}, ReplyToMessage: original})
	if got == nil || got.Content != "My reply" || got.UserDisplayName != "B User" || got.BotDisplayName != "NubeClaude" || got.UserMessageTimeMs != 1788911724000 || !strings.Contains(got.ExtraContent, original.Text) {
		t.Fatalf("bad metadata: %+v", got)
	}
	if got.QuotedMessage == nil || got.QuotedMessage.Author != "NubeClaude" || got.QuotedMessage.URL != "https://t.me/c/12345/20" {
		t.Fatalf("bad quote: %+v", got.QuotedMessage)
	}
	original.Chat.Username = "public_group"
	if q := telegramHistoryQuote(&models.Message{ReplyToMessage: original}); q.URL != "https://t.me/public_group/20" {
		t.Fatal(q)
	}
	original.Chat.Type = models.ChatTypePrivate
	if q := telegramHistoryQuote(&models.Message{ReplyToMessage: original}); q.URL != "" {
		t.Fatal(q)
	}
	original.ForumTopicCreated = &models.ForumTopicCreated{Name: "Topic"}
	if telegramHistoryQuote(&models.Message{ReplyToMessage: original}) != nil {
		t.Fatal("topic root treated as quote")
	}
}
