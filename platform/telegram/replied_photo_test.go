package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

func TestMentionReplyToPhotoIncludesImage(t *testing.T) {
	failDownload := false
	downloads := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			fmt.Fprint(w, `{"ok":true,"result":{"file_id":"photo","file_path":"photo.jpg"}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/photo.jpg") {
			downloads++
			if failDownload {
				http.Error(w, "unavailable", http.StatusNotFound)
				return
			}
			w.Write([]byte("image-bytes"))
			return
		}
		t.Errorf("unexpected request %s", r.URL.Path)
	})
	p.sharedSessionDirectory = true
	p.selfUser = &models.User{ID: 42, Username: "mybot", IsBot: true}
	var received *core.Message
	p.handler = func(_ core.Platform, m *core.Message) { received = m }
	chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup}
	msg := &models.Message{ID: 10, Date: int(time.Now().Unix()), From: &models.User{ID: 7}, Chat: chat, Text: "@mybot 图片里有什么", Entities: []models.MessageEntity{{Type: models.MessageEntityTypeMention, Offset: 0, Length: 6}}, ReplyToMessage: &models.Message{ID: 9, Chat: chat, From: &models.User{ID: 8}, Photo: []models.PhotoSize{{FileID: "photo", Width: 100, Height: 100}}}}
	// An ordinary reply must not dispatch the referenced image.
	text, entities := msg.Text, msg.Entities
	msg.Text, msg.Entities = "图片里有什么", nil
	p.handleMessage(context.Background(), msg)
	if received != nil || downloads != 0 {
		t.Fatal("ordinary reply was dispatched or downloaded")
	}
	msg.Text, msg.Entities = text, entities
	p.handleMessage(context.Background(), msg)
	if received == nil {
		t.Fatal("mentioned reply was not dispatched")
	}
	if !strings.Contains(received.ExtraContent, "explicitly addressed to the assistant") {
		t.Errorf("explicit bot mention lost: %s", received.ExtraContent)
	}
	if len(received.Images) != 1 {
		t.Fatalf("replied photo missing from agent input: images=%d want=1", len(received.Images))
	}
	if string(received.Images[0].Data) != "image-bytes" {
		t.Fatal("wrong referenced image bytes")
	}
	// Failed downloads must surface an attachment error once, never silently
	// submit the text alone or recursively retry the quoted image.
	failDownload = true
	received = nil
	p.handleMessage(context.Background(), msg)
	if received == nil || received.AttachmentError == nil {
		t.Fatal("missing attachment rejection")
	}
	if downloads != 2 {
		t.Fatalf("download attempts=%d want=2", downloads)
	}
}
