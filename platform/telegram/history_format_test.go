package telegram

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/chenhg5/cc-connect/core"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReceiptHTMLFallbackDoesNotExposeGeneratedTags(t *testing.T) {
	calls := 0
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		calls++
		if r.FormValue("parse_mode") == "HTML" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`)
			return
		}
		if got := r.FormValue("text"); strings.Contains(got, "<b>") || strings.Contains(got, "<blockquote>") || strings.Contains(got, "<a href=") {
			t.Errorf("generated HTML exposed to user: %s", got)
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":20,"chat":{"id":-1001,"type":"supergroup"}}}`)
	})
	err := p.ReplyWithReceipt(context.Background(), replyContext{chatID: -1001}, "**Moon · 2026-09-09**\n\n> quoted reply\n\n[查看原消息](https://t.me/c/123/4)", func(core.MessageReference) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestLongHistoryKeepsHTMLBalancedAcrossChunks(t *testing.T) {
	for _, method := range []string{"reply", "receipt", "send", "buttons"} {
		t.Run(method, func(t *testing.T) {
			accepted := 0
			p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseMultipartForm(1 << 20)
				text := r.FormValue("text")
				if len(text) > 6000 {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`)
					return
				}
				if r.FormValue("parse_mode") != "HTML" {
					t.Errorf("history unexpectedly downgraded: %.80s", text)
				}
				d := xml.NewDecoder(strings.NewReader("<root>" + text + "</root>"))
				for {
					_, err := d.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Errorf("broken HTML chunk: %v", err)
						break
					}
				}
				accepted++
				fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":-1001,"type":"supergroup"}}}`, accepted)
			})
			body := "**User**\n\n```text\n" + strings.Repeat("code sample & value\n", 500) + "```\n\n**Bot**\n\n> quotation\n\n[Original](https://t.me/c/123/4)"
			var err error
			switch method {
			case "receipt":
				err = p.ReplyWithReceipt(context.Background(), replyContext{chatID: -1001}, body, func(core.MessageReference) error { return nil })
			case "send":
				err = p.Send(context.Background(), replyContext{chatID: -1001}, body)
			case "buttons":
				err = p.SendWithButtons(context.Background(), replyContext{chatID: -1001}, body, nil)
			default:
				err = p.Reply(context.Background(), replyContext{chatID: -1001}, body)
			}
			if err != nil {
				t.Fatal(err)
			}
			if accepted < 2 {
				t.Fatal("not chunked")
			}
		})
	}
}
