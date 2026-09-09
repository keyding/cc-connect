package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

func TestLiveAllowFrom_SharedDenialSkipsAttachmentsAndCallbackActions(t *testing.T) {
	callbackAnswers := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Errorf("denied event caused network action: %s", r.URL.Path)
		}
		callbackAnswers++
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	p.sharedSessionDirectory = true
	p.groupReplyAll = true
	p.SetAllowFrom("7")
	var received []*core.Message
	p.handler = func(_ core.Platform, msg *core.Message) { received = append(received, msg) }
	chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, IsForum: true}
	ordinary := &models.Message{ID: 1, Date: int(time.Now().Unix()), From: &models.User{ID: 7}, Chat: chat, MessageThreadID: 77, Text: "allowed content"}
	p.handleMessage(context.Background(), ordinary)
	if len(received) != 1 || received[0].Content != "allowed content" || !p.AuthorizeSharedControl("-100", "7") {
		t.Fatal("initial authorization failed")
	}
	p.SetAllowFrom("8")
	if p.AuthorizeSharedControl("-100", "7") || !p.AuthorizeSharedControl("-100", "8") {
		t.Fatal("live snapshot was not applied")
	}
	denied := &models.Message{ID: 2, Date: int(time.Now().Unix()), From: &models.User{ID: 7, Username: "private-name"}, Chat: chat, MessageThreadID: 77, Caption: "private body", Photo: []models.PhotoSize{{FileID: "must-not-download"}}}
	p.handleMessage(context.Background(), denied)
	p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: "denied-callback", Data: "cmd:/new private-name", From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 55, Chat: chat, MessageThreadID: 77, Text: "private callback text"}}})
	if len(received) != 3 || callbackAnswers != 1 {
		t.Fatalf("messages=%d callbackAnswers=%d", len(received), callbackAnswers)
	}
	for _, msg := range received[1:] {
		if msg.Platform != "telegram" || msg.UserID != "7" || msg.SharedScope != "-100" || msg.SessionKey != p.buildSessionKey(-100, 77, 7) {
			t.Fatalf("missing denial identity: %+v", msg)
		}
		if msg.Content != "" || msg.ExtraContent != "" || msg.UserName != "" || msg.MessageID != "" || len(msg.Images) != 0 || len(msg.Files) != 0 || msg.Audio != nil || msg.Interaction != nil {
			t.Fatalf("denial leaked executable content: %+v", msg)
		}
		target := msg.ReplyCtx.(replyContext)
		if target.chatID != -100 || target.threadID != 77 {
			t.Fatal("denial changed position")
		}
	}
	p.SetAllowFrom("7,8")
	p.handleMessage(context.Background(), ordinary)
	if len(received) != 4 || received[3].Content != "allowed content" {
		t.Fatal("live grant did not restore admission")
	}
}

func TestLiveAllowFrom_NonsharedDenialRetainsLegacySilence(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("legacy denial sent network action: %s", r.URL.Path)
			})
			p.sharedSessionDirectory = shared
			p.SetAllowFrom("8")
			p.handler = func(core.Platform, *core.Message) { t.Fatal("nonshared denial reached engine") }
			chat := models.Chat{ID: 77, Type: models.ChatTypePrivate}
			if !shared {
				chat = models.Chat{ID: -100, Type: models.ChatTypeSupergroup}
			}
			p.handleMessage(context.Background(), &models.Message{ID: 1, Date: int(time.Now().Unix()), From: &models.User{ID: 7}, Chat: chat, Text: "private denied content"})
			p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: "cb", Data: "perm:allow", From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 55, Chat: chat}}})
		})
	}
}

func TestLiveAllowFrom_InaccessibleDeniedCallbackUsesAlertOnly(t *testing.T) {
	answers := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Errorf("guessed inaccessible callback position: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("callback_query_id") != "cb" || r.FormValue("show_alert") != "true" {
			t.Errorf("wrong alert: %v", r.Form)
		}
		answers++
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	p.sharedSessionDirectory = true
	p.SetAllowFrom("8")
	p.handler = func(platform core.Platform, msg *core.Message) {
		if msg.UserID != "7" || msg.SharedScope != "-100" || msg.MessageID != "" || msg.Content != "" {
			t.Fatalf("wrong denial envelope: %+v", msg)
		}
		// The Engine supplies the localized denial; the adapter only retains the
		// callback destination, whose missing Topic must never be reconstructed.
		if err := platform.Reply(context.Background(), msg.ReplyCtx, "当前无权限"); err != nil {
			t.Error(err)
		}
	}
	p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: "cb", Data: "cmd:/new", From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{InaccessibleMessage: &models.InaccessibleMessage{MessageID: 55, Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}}}})
	if answers != 1 {
		t.Fatalf("alerts=%d", answers)
	}
}

func TestLiveAllowFrom_ConcurrentSnapshotsAndInboundEvents(t *testing.T) {
	p := &Platform{bot: newStubTelegramBot(), selfUser: &models.User{ID: 1}, sharedSessionDirectory: true, groupReplyAll: true}
	var received atomic.Int32
	p.handler = func(core.Platform, *core.Message) { received.Add(1) }
	p.SetAllowFrom("7")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				if i == 0 {
					if n%2 == 0 {
						p.SetAllowFrom("8")
					} else {
						p.SetAllowFrom("7")
					}
					continue
				}
				if i == 1 {
					_ = p.AuthorizeSharedControl("-100", "7")
					continue
				}
				chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, IsForum: true}
				if i == 2 {
					p.handleMessage(context.Background(), &models.Message{ID: n + 1, Date: int(time.Now().Unix()), From: &models.User{ID: 7}, Chat: chat, MessageThreadID: 77, Text: "message"})
				} else {
					p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: fmt.Sprint(n), Data: "shared:" + strings.Repeat("a", 32) + ":allow", From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 55, Chat: chat, MessageThreadID: 77}}})
				}
			}
		}(i)
	}
	wg.Wait()
	if received.Load() != 200 {
		t.Fatalf("shared events lost: %d", received.Load())
	}
}
