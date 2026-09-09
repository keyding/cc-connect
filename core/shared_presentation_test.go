package core

import (
	"strings"
	"testing"
	"time"
)

func TestSharedHistoryPresentationLegacyAndQuote(t *testing.T) {
	e := &Engine{i18n: NewI18n(LangChinese)}
	legacy := "[Reply to @bot]: old quotation\nactual old message"
	got := e.sharedHistoryUser(sharedRequest{Content: legacy, UserID: "7"})
	if !strings.Contains(got, legacy) || !strings.Contains(got, "时间未知") || !strings.Contains(got, "旧记录") {
		t.Fatal(got)
	}
	body := "Will it rain?"
	got = e.sharedHistoryUser(sharedRequest{UserContent: &body, Content: "full private prompt", UserDisplayName: "B", SentAtMs: time.Date(2026, 9, 9, 2, 35, 24, 0, time.UTC).UnixMilli(), QuotedMessage: &QuotedMessage{Author: "A", Text: "Original answer\n\nSources:\nlong sources", URL: "https://t.me/c/123/4"}})
	if strings.Contains(got, "full private prompt") || strings.Contains(got, "Sources:") || !strings.Contains(got, "2026-09-09 10:35:24 (UTC+8)") || strings.Index(got, body) > strings.Index(got, "↪ 回复 A") {
		t.Fatal(got)
	}
	html := MarkdownToSimpleHTML(got)
	if !strings.Contains(html, "<blockquote>Original answer</blockquote>") || !strings.Contains(html, `href="https://t.me/c/123/4"`) {
		t.Fatal(html)
	}
	if len([]rune(historyQuoteExcerpt(strings.Repeat("长", 200)))) != 121 {
		t.Fatal("quote not bounded")
	}
}
