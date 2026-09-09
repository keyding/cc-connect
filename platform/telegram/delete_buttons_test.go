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

func TestDeleteCommandButtonRemovesKeyboardAndHidesToken(t *testing.T) {
	var edited struct {
		Text        string                      `json:"text"`
		ReplyMarkup models.InlineKeyboardMarkup `json:"reply_markup"`
	}
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			fmt.Fprint(w, `{"ok":true,"result":true}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			edited.Text = r.FormValue("text")
			if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &edited.ReplyMarkup); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":55}}`)
			return
		}
		t.Errorf("unexpected request: %s", r.URL.Path)
	})
	p.sharedSessionDirectory = true
	var received *core.Message
	p.handler = func(_ core.Platform, msg *core.Message) { received = msg }
	command := "/delete 0123456789abcdef0123456789abcdef confirm"
	data := "cmd:" + command
	p.handleCallbackQuery(context.Background(), &models.CallbackQuery{ID: "cb", Data: data, From: models.User{ID: 7}, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 55, Text: "Delete Alpha?", Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageThreadID: 77, ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "确认删除", CallbackData: data}}}}}}})
	if received == nil || received.Content != command || received.SharedScope != "-100" {
		t.Fatalf("callback not dispatched: %+v", received)
	}
	if len(edited.ReplyMarkup.InlineKeyboard) != 0 || !strings.Contains(edited.Text, "确认删除") || strings.Contains(edited.Text, "012345") {
		t.Fatalf("bad resolved button: %+v", edited)
	}
}
