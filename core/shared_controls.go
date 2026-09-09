package core

import (
	"log/slog"
	"strings"
)

// SharedControlAuthorizer rechecks current membership at each control operation.
// Platforms without this capability retain their inbound authorization boundary.
type SharedControlAuthorizer interface {
	AuthorizeSharedControl(scope, userID string) bool
}

func (e *Engine) handleSharedControl(p Platform, msg *Message, command string, args []string) {
	if auth, ok := p.(SharedControlAuthorizer); ok && !auth.AuthorizeSharedControl(msg.SharedScope, msg.UserID) {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgQueueDenied))
		return
	}
	key := sharedScopeKey(e.name, msg.Platform, msg.SharedScope)
	scope, _, err := e.sharedDirectory.apply(key, msg.SessionKey, e.agent.Name(), "", "")
	if err != nil {
		slog.Error("load shared directory for control", "error", err)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedUnavailable))
		return
	}
	sessionID := scope.Selections[msg.SessionKey]
	target := ""
	if len(args) == 1 {
		target = args[0]
	} else if len(args) > 1 && (command != "continue" || len(args) != 2 || args[1] != "confirm") {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgQueueStale))
		return
	}
	q := e.sharedQueue
	q.mu.Lock()
	var text string
	if command == "resolve" || command == "continue" {
		text, err = e.sharedRecoveryLocked(q, scope, msg, command, args)
	} else {
		text, err = e.sharedControlLocked(q, scope, msg, command, sessionID, target)
	}
	q.mu.Unlock()
	if err != nil {
		slog.Error("persist shared control", "error", err)
		text = e.i18n.T(MsgSharedUnavailable)
	}
	e.reply(p, msg.ReplyCtx, text)
	if err == nil && command == "resume" {
		e.startSharedQueue()
	}
}

func (e *Engine) sharedControlLocked(q *sharedQueue, scope sharedScope, msg *Message, command, sessionID, target string) (string, error) {
	visible := map[string]sharedSession{}
	for _, s := range scope.Sessions {
		visible[s.ID] = s
	}
	if command == "queue" {
		if target != "" {
			sessionID = target
		}
	}
	if target != "" && (command == "stop" || command == "cancel" || command == "resume") {
		sessionID = ""
		for _, r := range q.requests {
			if r.ID == target && r.Scope == msg.SharedScope && r.Platform == msg.Platform {
				sessionID = r.Session.ID
			}
		}
	}
	s, ok := visible[sessionID]
	if !ok {
		return e.i18n.T(MsgQueueStale), nil
	}
	if command == "queue" {
		return e.sharedQueueView(q, s), nil
	}
	next := append([]sharedRequest(nil), q.requests...)
	if command == "resume" {
		return e.resumeSharedQueue(q, s, target, next)
	}
	for i, r := range next {
		if r.Session.ID != sessionID || (target != "" && r.ID != target) {
			continue
		}
		if command == "cancel" {
			if r.Status != "queued" {
				if target != "" {
					break
				}
				continue
			}
			if r.UserID != msg.UserID {
				if target != "" {
					return e.i18n.T(MsgQueueDenied), nil
				}
				continue
			}
			next[i].Status = "cancelled"
		} else {
			if r.Status != "running" {
				if target != "" {
					break
				}
				continue
			}
			next[i].Status = "stopping"
		}
		if err := q.save(next); err != nil {
			q.paused = true
			return "", err
		}
		q.requests = next
		if command == "stop" {
			q.invalidateInteractionsLocked(r.ID)
			if cancel := q.cancels[r.ID]; cancel != nil {
				cancel()
			}
			return e.i18n.Tf(MsgQueueStopping, s.Name, r.UserID, r.ID), nil
		}
		return e.i18n.Tf(MsgQueueCancelled, s.Name, r.UserID, r.ID), nil
	}
	return e.i18n.T(MsgQueueStale), nil
}

func queueStatusKey(status string) MsgKey {
	switch status {
	case "queued":
		return MsgQueueQueued
	case "running":
		return MsgQueueRunning
	case "stopping":
		return MsgQueueStoppingStatus
	case "stopped":
		return MsgQueueStopped
	case "interrupted":
		return MsgQueueInterrupted
	case "cancelled":
		return MsgQueueCancelledStatus
	default:
		return MsgQueueCompleted
	}
}

func (e *Engine) sharedQueueView(q *sharedQueue, s sharedSession) string {
	lines := []string{e.i18n.Tf(MsgQueueTitle, sharedSessionTitle(s.Name)+"\n", "`"+s.ID+"`")}
	if r := q.uncertainAdmission; r != nil && r.Session.ID == s.ID {
		lines = append(lines, e.i18n.Tf(MsgQueueEntry, r.ID, sharedUserLabel(*r), e.i18n.T(MsgRecoveryAdmissionStatus)))
	}
	if q.paused {
		lines = append(lines, e.i18n.T(MsgSharedPaused))
	}
	for _, r := range q.requests {
		if r.Session.ID != s.ID {
			continue
		}
		status := e.i18n.T(queueStatusKey(r.Status))
		if r.Waiting && r.Status == "running" {
			status = e.i18n.T(MsgInteractionWaiting)
		}
		line := e.i18n.Tf(MsgQueueEntry, r.ID, sharedUserLabel(r), status)
		if r.Status == "queued" {
			line += "\n/cancel " + r.ID
		}
		if r.Status == "running" {
			line += "\n/stop " + r.ID
		}
		if r.Status == "interrupted" {
			line += "\n/resolve " + r.ID + "\n/continue " + r.ID
		}
		if r.Status == "stopped" {
			line += "\n/resume " + r.ID
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (e *Engine) resumeSharedQueue(q *sharedQueue, s sharedSession, target string, next []sharedRequest) (string, error) {
	if q.paused || q.err != nil {
		return e.i18n.T(MsgQueueBlocked), nil
	}
	changed := false
	for i, r := range next {
		if r.Session.ID != s.ID {
			continue
		}
		switch r.Status {
		case "running", "stopping", "interrupted":
			return e.i18n.T(MsgQueueBlocked), nil
		case "stopped":
			if target != "" && r.ID != target {
				return e.i18n.T(MsgQueueStale), nil
			}
			if !r.ExitConfirmed {
				return e.i18n.T(MsgQueueBlocked), nil
			}
			next[i].Status = "completed"
			changed = true
		}
	}
	if !changed {
		return e.i18n.T(MsgQueueStale), nil
	}
	if err := q.save(next); err != nil {
		q.paused = true
		return "", err
	}
	q.requests = next
	return e.i18n.Tf(MsgQueueResumed, s.Name), nil
}
