package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// This independently verifiable directory slice must not fall through to the
// legacy entry-keyed executor or expose unregistered agent history.
func (e *Engine) handleSharedDirectory(p Platform, msg *Message, command string, args []string) {
	key, _ := json.Marshal([]string{e.name, msg.Platform, msg.SharedScope}) // strings always encode
	scope, hint, err := e.sharedDirectory.apply(string(key), msg.SessionKey, e.agent.Name(), command, strings.Join(args, " "))
	if err != nil {
		slog.Error("shared directory unavailable", "project", e.name, "error", err)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedUnavailable))
		return
	}
	if hint != "" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(hint))
		return
	}
	if command == "list" {
		lines := []string{e.i18n.T(MsgSharedList)}
		for i, s := range scope.Sessions {
			marker := ""
			if s.ID == scope.Selections[msg.SessionKey] {
				marker = " *"
			}
			lines = append(lines, fmt.Sprintf("%d. %s [%s] (%s)%s", i+1, s.Name, s.AgentType, s.ID, marker))
		}
		if len(scope.Sessions) == 0 {
			lines = append(lines, e.i18n.T(MsgSharedChoose))
		}
		e.reply(p, msg.ReplyCtx, strings.Join(lines, "\n"))
		return
	}
	for _, s := range scope.Sessions {
		if s.ID == scope.Selections[msg.SessionKey] {
			text := e.i18n.Tf(MsgSharedCurrent, s.Name, s.AgentType, s.ID)
			if command == "" {
				text += "\n" + e.i18n.T(MsgSharedExecutionPending)
			}
			e.reply(p, msg.ReplyCtx, text)
			return
		}
	}
	e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedChoose))
}
