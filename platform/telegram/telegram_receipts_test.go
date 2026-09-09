package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

func TestReplyWithReceipt_HTTPContracts(t *testing.T) {
	var refs []core.MessageReference
	calls := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		calls++
		if r.FormValue("chat_id") != "-100" || r.FormValue("message_thread_id") != "77" {
			t.Errorf("changed destination: %v", r.Form)
		}
		if calls == 1 {
			var reply models.ReplyParameters
			if err := json.Unmarshal([]byte(r.FormValue("reply_parameters")), &reply); err != nil {
				t.Error(err)
			}
			if reply.MessageID != 10 || !reply.AllowSendingWithoutReply {
				t.Errorf("missing vanished-reference fallback: %+v", reply)
			}
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":77,"text":"answer"}}`, 100+calls)
	})
	err := p.ReplyWithReceipt(context.Background(), replyContext{chatID: -100, threadID: 77, messageID: 10}, strings.Repeat("answer ", 1500), func(ref core.MessageReference) error { refs = append(refs, ref); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 || len(refs) != calls {
		t.Fatalf("calls=%d receipts=%v", calls, refs)
	}
	for i, ref := range refs {
		if ref.Scope != "-100" || ref.MessageID != fmt.Sprint(101+i) {
			t.Fatal(ref)
		}
	}
}

func TestReplyWithReceipt_DeliveryOrReceiptFailureNeverRetries(t *testing.T) {
	for _, deliveryFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(deliveryFailure), func(t *testing.T) {
			calls := 0
			p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if deliveryFailure {
					fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message thread not found"}`)
					return
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"chat":{"id":-100,"type":"supergroup"}}}`)
			})
			err := p.ReplyWithReceipt(context.Background(), replyContext{chatID: -100, threadID: 77}, "answer", func(core.MessageReference) error { return fmt.Errorf("disk unavailable") })
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestDispatchMessage_ExplicitBotReferenceAndDuplicate(t *testing.T) {
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL.Path) })
	var got []*core.Message
	p.handler = func(_ core.Platform, msg *core.Message) { got = append(got, msg) }
	reply := &models.Message{ID: 99, Chat: models.Chat{ID: -100}, From: &models.User{ID: 1, IsBot: true}, Text: "answer"}
	msg := &models.Message{ID: 10, Chat: models.Chat{ID: -100}, ReplyToMessage: reply}
	for i := 0; i < 2; i++ {
		p.dispatchMessage(&core.Message{MessageID: "10"}, msg)
	}
	if len(got) != 2 || got[0].BotReply == nil || *got[0].BotReply != *got[1].BotReply || got[0].BotReply.MessageID != "99" || got[0].BotReply.Scope != "-100" {
		t.Fatalf("got %+v", got)
	}
	// A bot message with no valid ID remains an explicit unresolved reference.
	reply.ID = 0
	p.dispatchMessage(&core.Message{}, msg)
	if got[2].BotReply == nil || got[2].BotReply.MessageID != "0" {
		t.Fatal("missing reference silently discarded")
	}
	reply.From = &models.User{ID: 2}
	p.dispatchMessage(&core.Message{}, msg)
	if got[3].BotReply != nil {
		t.Fatal("ordinary user quote became routing evidence")
	}
	reply.From = &models.User{ID: 1, IsBot: true}
	msg.ForwardOrigin = &models.MessageOrigin{}
	p.dispatchMessage(&core.Message{}, msg)
	if got[4].BotReply != nil {
		t.Fatal("forward became routing evidence")
	}
}
