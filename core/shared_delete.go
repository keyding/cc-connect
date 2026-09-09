package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
)

type sharedDeleteConfirmation struct {
	Scope, Session string
	RequestCount   int
}

// Called under sharedMutationMu, which also covers every route into admission.
func (e *Engine) handleSharedDelete(p Platform, msg *Message, args []string) {
	key := sharedScopeKey(e.name, msg.Platform, msg.SharedScope)
	scope, _, err := e.sharedDirectory.apply(key, msg.SessionKey, e.agent.Name(), "", "")
	if err != nil {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedUnavailable))
		return
	}
	sessionID := scope.Selections[msg.SessionKey]
	var confirmation sharedDeleteConfirmation
	confirm := len(args) == 2 && args[1] == "confirm"
	if confirm {
		var ok bool
		confirmation, ok = e.sharedDeleteConfirmations[args[0]]
		if !ok || confirmation.Scope != key {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgQueueStale))
			return
		}
		sessionID = confirmation.Session
	} else if len(args) == 1 {
		sessionID = args[0]
	} else if len(args) > 1 {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgQueueStale))
		return
	}
	var session sharedSession
	for _, s := range scope.Sessions {
		if s.ID == sessionID {
			session = s
		}
	}
	if session.ID == "" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedChoose))
		return
	}
	count, canDelete := e.sharedQueue.deletionState(session.ID)
	if !canDelete {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedDeleteBusy))
		return
	}
	if confirm {
		delete(e.sharedDeleteConfirmations, args[0])
		if count != confirmation.RequestCount {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgQueueStale))
			return
		}
		if err := e.sharedDirectory.deleteSession(key, session.ID); err != nil {
			slog.Error("delete shared session", "error", err)
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedUnavailable))
			return
		}
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgSharedDeleted, session.Name, session.ID))
		return
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		slog.Error("create shared deletion confirmation", "error", err)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedUnavailable))
		return
	}
	token := hex.EncodeToString(nonce[:])
	if e.sharedDeleteConfirmations == nil {
		e.sharedDeleteConfirmations = map[string]sharedDeleteConfirmation{}
	}
	// Retain only the latest offered confirmation for this stable session.
	for old, c := range e.sharedDeleteConfirmations {
		if c.Scope == key && c.Session == session.ID {
			delete(e.sharedDeleteConfirmations, old)
		}
	}
	e.sharedDeleteConfirmations[token] = sharedDeleteConfirmation{Scope: key, Session: session.ID, RequestCount: count}
	e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgSharedDeleteConfirm, session.Name, session.ID, token))
}

func (q *sharedQueue) deletionState(session string) (count int, allowed bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil || q.paused || q.uncertainAdmission != nil {
		return 0, false
	}
	for _, r := range q.requests {
		if r.Session.ID == session {
			count++
			if !sharedTerminal(r.Status) {
				return count, false
			}
		}
	}
	return count, true
}

func (d *sharedDirectory) deleteSession(key, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loadErr != nil {
		return d.loadErr
	}
	original := d.state.Scopes[key]
	scope := sharedScope{Selections: map[string]string{}}
	found := false
	for _, s := range original.Sessions {
		if s.ID == id {
			found = true
		} else {
			scope.Sessions = append(scope.Sessions, s)
		}
	}
	if !found {
		return fmt.Errorf("shared session unavailable")
	}
	for entry, selected := range original.Selections {
		if selected != id {
			scope.Selections[entry] = selected
		}
	}
	next := sharedDirectoryState{Version: d.state.Version, Scopes: map[string]sharedScope{}, Links: map[string]string{}, InteractionLinks: map[string]bool{}}
	for k, s := range d.state.Scopes {
		next.Scopes[k] = s
	}
	next.Scopes[key] = scope
	for link, target := range d.state.Links {
		if target != id {
			next.Links[link] = target
			if d.state.InteractionLinks[link] {
				next.InteractionLinks[link] = true
			}
		}
	}
	if err := d.save(next); err != nil {
		d.loadErr = fmt.Errorf("shared deletion persistence uncertain: %w", err)
		return d.loadErr
	}
	d.state = next
	return nil
}
