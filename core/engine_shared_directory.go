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
	if command == "" && msg.BotReply != nil {
		e.routeSharedReply(p, msg)
		return
	}
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
			entry := fmt.Sprintf("%d. %s\n`%s`", i+1, sharedSessionName(s.Name), s.ID)
			if s.ID == scope.Selections[msg.SessionKey] {
				entry = "> " + e.i18n.T(MsgSharedCurrentLabel) + "\n" + entry
			}
			lines = append(lines, entry)
		}
		if len(scope.Sessions) == 0 {
			lines = append(lines, e.i18n.T(MsgSharedChoose))
		}
		e.reply(p, msg.ReplyCtx, strings.Join(lines, "\n\n"))
		return
	}
	for _, s := range scope.Sessions {
		if s.ID == scope.Selections[msg.SessionKey] {
			if command == "history" {
				e.replySharedHistory(p, msg, s, args)
				return
			}
			text := e.i18n.Tf(MsgSharedCurrent, sharedSessionName(s.Name), s.AgentType, "`"+s.ID+"`")
			if command == "" {
				e.acceptSharedRequest(p, msg, s)
				return
			}
			e.reply(p, msg.ReplyCtx, text)
			return
		}
	}
	e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedChoose))
}
