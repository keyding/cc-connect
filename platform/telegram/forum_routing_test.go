package telegram

import (
	"context"
	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
	"testing"
	"time"
)

func TestForumTopicRootDoesNotAddressBot(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		mention, reply, command, all bool
		want                         int
	}{
		{name: "ordinary topic message"},
		{name: "explicit mention", mention: true, want: 1},
		{name: "explicit bot reply", reply: true, want: 1},
		{name: "command", command: true, want: 1},
		{name: "reply all opt in", all: true, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created, err := New(map[string]any{"token": "TEST_TOKEN", "allow_from": "*", "shared_session_directory": true, "group_reply_all": tc.all})
			if err != nil {
				t.Fatal(err)
			}
			p := created.(*Platform)
			p.selfUser = &models.User{ID: 42, Username: "mybot", IsBot: true}
			calls := 0
			p.handler = func(_ core.Platform, m *core.Message) { calls++ }
			chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, IsForum: true}
			msg := &models.Message{ID: 809, Date: int(time.Now().Unix()), Text: "hey", From: &models.User{ID: 7}, Chat: chat, IsTopicMessage: true, MessageThreadID: 4,
				ReplyToMessage: &models.Message{ID: 4, Chat: chat, From: p.selfUser, ForumTopicCreated: &models.ForumTopicCreated{Name: "Topic"}},
			}
			if tc.mention {
				msg.Text = "@mybot hey"
				msg.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeMention, Offset: 0, Length: 6}}
			}
			if tc.reply {
				msg.ReplyToMessage = &models.Message{ID: 800, Chat: chat, From: p.selfUser, Text: "question"}
			}
			if tc.command {
				msg.Text = "/list"
				msg.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 5}}
			}
			p.handleMessage(context.Background(), msg)
			if calls != tc.want {
				t.Fatalf("ordinary topic context was treated as a reply: dispatched=%d want=%d", calls, tc.want)
			}
		})
	}
}
