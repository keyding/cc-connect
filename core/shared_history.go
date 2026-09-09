package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Shared history uses only completed turns admitted by this instance, never an
// account-wide Agent history list or the legacy entry-keyed default session.
func (e *Engine) replySharedHistory(p Platform, msg *Message, session sharedSession, args []string) {
	limit := 10
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}
	type entry struct{ turn, text string }
	var entries []entry
	e.sharedQueue.mu.Lock()
	for _, r := range e.sharedQueue.requests {
		if r.Session.ID != session.ID || r.Scope != msg.SharedScope || r.Platform != msg.Platform || r.Status != "completed" {
			continue
		}
		entries = append(entries, entry{r.ID, e.sharedHistoryUser(r)})
		if r.Result != "" {
			bot := r.BotDisplayName
			if bot == "" {
				bot = e.i18n.T(MsgHistoryBot)
			}
			entries = append(entries, entry{r.ID, "**🤖 " + sharedDisplayLabel(bot) + " · " + e.sharedHistoryTime(r.CompletedAtMs) + "**\n\n" + truncateHistoryEntry(r.Result, e.historyEntryMaxLen())})
		}
	}
	e.sharedQueue.mu.Unlock()
	if len(entries) == 0 {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgHistoryEmpty))
		return
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	var rendered []string
	for i, row := range entries {
		if i > 0 && row.turn != entries[i-1].turn {
			rendered = append(rendered, "────────────")
		}
		rendered = append(rendered, row.text)
	}
	e.reply(p, msg.ReplyCtx, fmt.Sprintf("%s\n%s\n\n%s", sharedSessionTitle(session.Name), e.i18n.Tf(MsgSharedHistoryHeader, len(entries)), strings.Join(rendered, "\n\n")))
}
