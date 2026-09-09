package telegram

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

type topicRoutingAgent struct {
	attachmentFailureAgent
	dir string
}

func (a *topicRoutingAgent) GetWorkDir() string { return a.dir }

func TestTopicRootReplyAfterSwitchUsesSelectedSession(t *testing.T) {
	replies := make(chan string, 20)
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		replies <- r.FormValue("text")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":100,"chat":{"id":-100,"type":"supergroup"}}}`)
	})
	p.sharedSessionDirectory = true
	p.allowFrom = "*"
	a := &topicRoutingAgent{dir: t.TempDir()}
	e := core.NewEngine("project", a, []core.Platform{p}, filepath.Join(t.TempDir(), "sessions"), core.LangChinese)
	t.Cleanup(func() { _ = e.Stop() })
	p.handler = e.ReceiveMessage
	chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, IsForum: true}
	root := &models.Message{ID: 4, Chat: chat, From: &models.User{ID: 1, IsBot: true}, ForumTopicCreated: &models.ForumTopicCreated{Name: "test"}}
	for i, content := range []string{"/new test", "/switch 1", "/current", "hey", "@testbot 今天的天气"} {
		msg := &models.Message{ID: 10 + i, Date: int(time.Now().Unix()), From: &models.User{ID: 7}, Text: content, Chat: chat, IsTopicMessage: true, MessageThreadID: 4, ReplyToMessage: root}
		if strings.HasPrefix(content, "/") {
			msg.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len(strings.Fields(content)[0])}}
		} else if strings.HasPrefix(content, "@testbot") {
			msg.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeMention, Offset: 0, Length: 8}}
		}
		p.handleMessage(context.Background(), msg)
		if content == "hey" {
			select {
			case reply := <-replies:
				t.Fatalf("ordinary topic message got reply: %s", reply)
			case <-time.After(50 * time.Millisecond):
			}
			if a.starts.Load() != 0 {
				t.Fatal("ordinary topic message started Agent")
			}
			continue
		}
		select {
		case reply := <-replies:
			if strings.Contains(reply, "无法确认") {
				t.Fatalf("selected session rejected as unknown reply: %s", reply)
			}
		case <-time.After(time.Second):
			t.Fatal("no reply")
		}
	}
	deadline := time.Now().Add(time.Second)
	for a.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.starts.Load() != 1 {
		t.Fatalf("selected Agent starts=%d, want 1", a.starts.Load())
	}
}

func TestReplyReference_TopicContextDoesNotHideExplicitBotReplies(t *testing.T) {
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL.Path) })
	for _, tc := range []struct {
		name          string
		replyID       int
		creation      bool
		wantReference bool
	}{
		{"implicit topic root", 4, true, false},
		{"explicit answer", 99, false, true},
		{"root ID without service marker", 4, false, true},
		{"other topic service message", 99, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup}
			reply := &models.Message{ID: tc.replyID, Chat: chat, From: &models.User{ID: 1, IsBot: true}}
			if tc.creation {
				reply.ForumTopicCreated = &models.ForumTopicCreated{Name: "test"}
			}
			msg := &models.Message{ID: 10, Chat: chat, IsTopicMessage: true, MessageThreadID: 4, ReplyToMessage: reply}
			if got := p.replyReference(msg); (got != nil) != tc.wantReference {
				t.Fatalf("reference=%+v, want reference %v", got, tc.wantReference)
			}
		})
	}
}
