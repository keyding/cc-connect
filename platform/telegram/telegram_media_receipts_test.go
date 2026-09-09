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

func TestMediaAndPreviewReceipts_HTTPPartialFailureAndEditIdentity(t *testing.T) {
	calls := 0
	failEdit := false
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		calls++
		if r.FormValue("chat_id") != "-100" {
			t.Errorf("wrong chat %v", r.Form)
		}
		isEdit := strings.HasSuffix(r.URL.Path, "/editMessageText")
		if !isEdit && r.FormValue("message_thread_id") != "77" {
			t.Errorf("wrong topic %v", r.Form)
		}
		if isEdit && r.FormValue("message_id") != "103" {
			t.Errorf("edit changed identity: %v", r.Form)
		}
		if isEdit && failEdit {
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`)
			return
		}
		id := 100 + calls
		if isEdit {
			id = 103
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":77}}`, id)
	})
	var refs []core.MessageReference
	record := func(ref core.MessageReference) error { refs = append(refs, ref); return nil }
	ctx := context.Background()
	target := replyContext{chatID: -100, threadID: 77, messageID: 9}
	if err := p.SendImageWithReceipt(ctx, target, core.ImageAttachment{FileName: "chart.png", Data: []byte("png")}, record); err != nil {
		t.Fatal(err)
	}
	if err := p.SendFileWithReceipt(ctx, target, core.FileAttachment{FileName: "report.txt", Data: []byte("text")}, record); err != nil {
		t.Fatal(err)
	}
	handle, err := p.SendPreviewWithReceipt(ctx, target, "first", record)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.UpdatePreviewWithReceipt(ctx, handle, "updated", record); err != nil {
		t.Fatal(err)
	}
	failEdit = true
	if err := p.UpdatePreviewWithReceipt(ctx, handle, "lost edit", record); err == nil {
		t.Fatal("expected failed edit")
	}
	if calls != 5 || len(refs) != 4 || refs[2] != refs[3] || refs[0].MessageID != "101" || refs[1].MessageID != "102" {
		t.Fatalf("calls %d receipts %+v", calls, refs)
	}
}

func TestReplyChunks_PartialSuccessOnlyRecordsVerifiedMessages(t *testing.T) {
	calls := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message thread not found"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":77}}`)
	})
	var refs []core.MessageReference
	err := p.ReplyWithReceipt(context.Background(), replyContext{chatID: -100, threadID: 77}, strings.Repeat("answer ", 2000), func(ref core.MessageReference) error { refs = append(refs, ref); return nil })
	if err == nil || calls != 2 || len(refs) != 1 || refs[0].MessageID != "101" {
		t.Fatalf("err=%v calls=%d refs=%v", err, calls, refs)
	}
}

func TestExternalReply_ProtocolIdentityAndGroupUpgradeFailClosed(t *testing.T) {
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected network call %s", r.URL.Path) })
	for _, tc := range []struct {
		name, payload string
		want          *core.MessageReference
		directed      bool
	}{
		{"same group other topic", `{"origin":{"type":"user","date":1,"sender_user":{"id":1,"is_bot":true,"first_name":"Bot"}},"chat":{"id":-100,"type":"supergroup"},"message_id":99}`, &core.MessageReference{Scope: "-100", MessageID: "99"}, true},
		{"missing optional ids", `{"origin":{"type":"user","date":1,"sender_user":{"id":1,"is_bot":true,"first_name":"Bot"}}}`, &core.MessageReference{}, true},
		{"other group retains original id", `{"origin":{"type":"user","date":1,"sender_user":{"id":1,"is_bot":true,"first_name":"Bot"}},"chat":{"id":-200,"type":"supergroup"},"message_id":99}`, &core.MessageReference{Scope: "-200", MessageID: "99"}, true},
		{"ordinary user", `{"origin":{"type":"user","date":1,"sender_user":{"id":2,"is_bot":false,"first_name":"User"}},"chat":{"id":-100,"type":"supergroup"},"message_id":99}`, nil, false},
		{"hidden author", `{"origin":{"type":"hidden_user","date":1,"sender_user_name":"Bot"},"chat":{"id":-100,"type":"supergroup"},"message_id":99}`, &core.MessageReference{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var external models.ExternalReplyInfo
			if err := json.Unmarshal([]byte(tc.payload), &external); err != nil {
				t.Fatal(err)
			}
			msg := &models.Message{ID: 10, Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageThreadID: 88, ExternalReply: &external, MigrateFromChatID: -200}
			got := p.replyReference(msg)
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			if p.isDirectedAtBot(msg) != tc.directed {
				t.Fatal("external reference dropped before routing")
			}
		})
	}
}

func TestReplyReceipt_GroupMigrationDoesNotRetarget(t *testing.T) {
	calls := 0
	receipts := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("chat_id") != "-100" {
			t.Errorf("changed group: %v", r.Form)
		}
		fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-200}}`)
	})
	err := p.ReplyWithReceipt(context.Background(), replyContext{chatID: -100, threadID: 77}, "answer", func(core.MessageReference) error { receipts++; return nil })
	if err == nil || calls != 1 || receipts != 0 {
		t.Fatalf("err=%v calls=%d receipts=%d", err, calls, receipts)
	}
}

func TestEditReceipt_MismatchedIdentityCannotCreateAssociation(t *testing.T) {
	receipts := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":999,"chat":{"id":-100,"type":"supergroup"},"message_thread_id":77}}`)
	})
	err := p.UpdatePreviewWithReceipt(context.Background(), &telegramPreviewHandle{chatID: -100, threadID: 77, messageID: 103}, "edit", func(core.MessageReference) error { receipts++; return nil })
	if err == nil || receipts != 0 {
		t.Fatalf("err=%v receipts=%d", err, receipts)
	}
}
