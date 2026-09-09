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
	var entries []string
	e.sharedQueue.mu.Lock()
	for _, r := range e.sharedQueue.requests {
		if r.Session.ID != session.ID || r.Scope != msg.SharedScope || r.Platform != msg.Platform || r.Status != "completed" {
			continue
		}
		entries = append(entries, "👤 "+truncateHistoryEntry(r.Content, e.historyEntryMaxLen()))
		if r.Result != "" {
			entries = append(entries, "🤖 "+truncateHistoryEntry(r.Result, e.historyEntryMaxLen()))
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
	e.reply(p, msg.ReplyCtx, fmt.Sprintf("%s\n\n%s", e.i18n.Tf(MsgSharedHistoryHeader, session.Name, len(entries)), strings.Join(entries, "\n\n")))
}
