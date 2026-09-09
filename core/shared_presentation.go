package core

import (
	"strings"
	"time"
)

func sharedSessionTitle(name string) string {
	return "💬 **「" + sharedDisplayLabel(name) + "」**"
}

func sharedDisplayLabel(name string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "*", "∗", "`", "ˋ").Replace(name)
}

func sharedUserLabel(r sharedRequest) string {
	for _, name := range []string{r.UserDisplayName, r.UserName, r.UserID} {
		if strings.TrimSpace(name) != "" {
			return sharedDisplayLabel(name)
		}
	}
	return "?"
}

func (e *Engine) sharedHistoryTime(ms int64) string {
	if ms <= 0 {
		return e.i18n.T(MsgHistoryTimeUnknown)
	}
	return time.UnixMilli(ms).In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05") + " (UTC+8)"
}

func historyQuoteExcerpt(text string) string {
	var lines []string
	for _, line := range strings.Split(stripAgentFooterLines(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Sources:") || strings.Contains(line, " · 上下文 ") || strings.Contains(line, " · Context ") {
			break
		}
		lines = append(lines, line)
		if len(lines) == 2 {
			break
		}
	}
	runes := []rune(strings.Join(lines, " "))
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return string(runes)
}

func (e *Engine) sharedHistoryUser(r sharedRequest) string {
	body := r.Content
	note := ""
	if r.UserContent != nil {
		body = *r.UserContent
	} else if strings.HasPrefix(body, "[Reply to ") {
		note = "\n" + e.i18n.T(MsgHistoryLegacyQuote)
	}
	text := "**👤 " + sharedUserLabel(r) + " · " + e.sharedHistoryTime(r.SentAtMs) + "**\n\n" + truncateHistoryEntry(body, e.historyEntryMaxLen()) + note
	if q := r.QuotedMessage; q != nil {
		author := q.Author
		if author == "" {
			author = e.i18n.T(MsgHistoryAuthorUnknown)
		}
		text += "\n\n" + e.i18n.Tf(MsgHistoryQuoteLabel, sharedDisplayLabel(author)) + "\n> " + historyQuoteExcerpt(q.Text)
		if q.URL != "" {
			text += "\n[" + e.i18n.T(MsgHistoryViewOriginal) + "](" + q.URL + ")"
		}
	}
	return text
}
