package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

func TestSharedInteraction_CallbackBindingAndDuplicateProtocol(t *testing.T) {
	calls := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Errorf("callback edited before authorization: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	p.sharedSessionDirectory = true
	p.allowFrom = "7,8"
	var got []*core.Message
	p.handler = func(_ core.Platform, msg *core.Message) { got = append(got, msg) }
	token := strings.Repeat("a", 32)
	for _, data := range []string{"shared:" + token + ":allow", "shared:" + token + ":allow", "shared:" + token + ":q0:o1", "shared:invalid", "perm:allow", "askq:1:1"} {
		p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: fmt.Sprint(len(got)), Data: data, From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 55, Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageThreadID: 77}}})
	}
	if len(got) != 6 || calls != 6 {
		t.Fatalf("got=%d calls=%d", len(got), calls)
	}
	for _, msg := range got {
		if msg.SharedScope != "-100" || msg.UserID != "7" || msg.Interaction == nil || msg.Content != "" || msg.ReplyCtx.(replyContext).threadID != 77 {
			t.Fatalf("wrong callback message: %+v", msg)
		}
	}
	if got[0].Interaction.Token != token || got[1].Interaction.Token != token || got[2].Interaction.Question != 0 || got[2].Interaction.Option != 1 {
		t.Fatal("lost question generation or choice")
	}
	for _, msg := range got[3:] {
		if msg.Interaction.Action != "" {
			t.Fatal("legacy or malformed callback became a decision")
		}
	}
}

func TestSharedInteraction_ButtonAndChunkReceiptHTTP(t *testing.T) {
	calls := 0
	token := strings.Repeat("b", 32)
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		calls++
		if r.FormValue("chat_id") != "-100" || r.FormValue("message_thread_id") != "77" {
			t.Error("changed interaction target")
		}
		if calls == 1 {
			var markup models.InlineKeyboardMarkup
			if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &markup); err != nil {
				t.Error(err)
			}
			if len(markup.InlineKeyboard) != 1 || markup.InlineKeyboard[0][0].CallbackData != "shared:"+token+":allow" {
				t.Fatal(markup)
			}
		} else if r.FormValue("reply_markup") != "" {
			t.Error("duplicated button controls on later chunks")
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":77}}`, 100+calls)
	})
	var refs []core.MessageReference
	err := p.SendWithButtonsWithReceipt(context.Background(), replyContext{chatID: -100, threadID: 77, messageID: 8}, strings.Repeat("operation details ", 700), [][]core.ButtonOption{{{Text: "Allow", Data: "shared:" + token + ":allow"}}}, func(ref core.MessageReference) error { refs = append(refs, ref); return nil })
	if err != nil || len(refs) != calls || calls < 2 {
		t.Fatalf("err=%v refs=%d calls=%d", err, len(refs), calls)
	}
}

func TestSharedInteraction_InaccessibleCallbackShowsLocalizedStaleAlert(t *testing.T) {
	var alert string
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Errorf("guessed message destination: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		alert = r.FormValue("text")
		if r.FormValue("show_alert") != "true" || r.FormValue("callback_query_id") != "callback" {
			t.Errorf("wrong callback reply: %v", r.Form)
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	p.sharedSessionDirectory = true
	p.allowFrom = "*"
	a := &attachmentFailureAgent{}
	e := core.NewEngine("project", a, []core.Platform{p}, filepath.Join(t.TempDir(), "sessions"), core.LangChinese)
	t.Cleanup(func() { _ = e.Stop() })
	p.handler = e.ReceiveMessage
	p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: "callback", Data: "shared:" + strings.Repeat("a", 32) + ":allow", From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{InaccessibleMessage: &models.InaccessibleMessage{MessageID: 9, Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}}}})
	if !strings.Contains(alert, "已失效") || a.starts.Load() != 0 {
		t.Fatalf("alert=%q starts=%d", alert, a.starts.Load())
	}
}

func TestSharedInteraction_TextQuestionOmitsEmptyKeyboardHTTP(t *testing.T) {
	for _, buttons := range [][][]core.ButtonOption{nil, {}, {{}}} {
		t.Run(fmt.Sprintf("rows-%d", len(buttons)), func(t *testing.T) {
			p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if r.FormValue("reply_markup") != "" {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: field inline_keyboard must be of type Array"}`)
					return
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"chat":{"id":-100,"type":"supergroup"}}}`)
			})
			recorded := false
			err := p.SendWithButtonsWithReceipt(context.Background(), replyContext{chatID: -100}, "Choose multiple options or reply with notes", buttons, func(ref core.MessageReference) error { recorded = true; return nil })
			if err != nil || !recorded {
				t.Fatalf("text question failed: err=%v recorded=%v", err, recorded)
			}
		})
	}
}
