package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestIncomingMessageReactionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		emoji   any
		enabled bool
		want    string
	}{
		{"default", nil, true, "👀"}, {"custom", "🫡", true, "🫡"}, {"blank", " ", true, "👀"}, {"disabled", "👍", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reactions := make(chan string, 2)
			wire := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				var values []struct {
					Emoji string `json:"emoji"`
				}
				if err := json.Unmarshal([]byte(r.FormValue("reaction")), &values); err != nil {
					t.Error(err)
				}
				if len(values) == 1 {
					reactions <- values[0].Emoji
				}
				fmt.Fprint(w, `{"ok":true,"result":true}`)
			})
			opts := map[string]any{"token": "TEST_TOKEN", "allow_from": "*", "enable_reactions": tc.enabled}
			if tc.emoji != nil {
				opts["reaction_emoji"] = tc.emoji
			}
			created, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			p := created.(*Platform)
			p.bot = wire.bot
			p.selfUser = wire.selfUser
			p.handleMessage(context.Background(), &models.Message{ID: 10, Date: int(time.Now().Unix()), Text: "hello", From: &models.User{ID: 7}, Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate}})
			if tc.want == "" {
				select {
				case got := <-reactions:
					t.Fatalf("reaction disabled, got %q", got)
				case <-time.After(50 * time.Millisecond):
				}
				return
			}
			select {
			case got := <-reactions:
				if got != tc.want {
					t.Fatalf("reaction=%q want %q", got, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("missing reaction")
			}
		})
	}
}

func TestReactionEmojiRejectsNonString(t *testing.T) {
	if _, err := New(map[string]any{"token": "TEST_TOKEN", "allow_from": "*", "reaction_emoji": true}); err == nil {
		t.Fatal("accepted non-string emoji")
	}
}
