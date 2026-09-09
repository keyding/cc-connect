package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// MessageReference identifies an actual platform message within a group. Text,
// forwarded copies and mutable session names are never identity evidence.
type MessageReference struct{ Scope, MessageID string }

// ReceiptReplySender reports each successfully delivered text message, including
// chunks. A receipt failure must never cause the sender to resend that message.
type ReceiptReplySender interface {
	ReplyWithReceipt(context.Context, any, string, func(MessageReference) error) error
}

func sharedScopeKey(project, platform, scope string) string {
	key, _ := json.Marshal([]string{project, platform, scope})
	return string(key)
}
func sharedMessageKey(project, platform string, ref MessageReference) string {
	key, _ := json.Marshal([]string{project, platform, ref.Scope, ref.MessageID})
	return string(key)
}

func (d *sharedDirectory) recordMessage(project, platform, scope, session string, ref MessageReference) error {
	return d.recordMessageKind(project, platform, scope, session, ref, false)
}
func (d *sharedDirectory) recordMessageKind(project, platform, scope, session string, ref MessageReference, interaction bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loadErr != nil {
		return fmt.Errorf("message link state unavailable: %w", d.loadErr)
	}
	exists := false
	for _, s := range d.state.Scopes[sharedScopeKey(project, platform, scope)].Sessions {
		if s.ID == session {
			exists = true
		}
	}
	if !exists {
		return fmt.Errorf("shared message target deleted")
	}
	if ref.Scope != scope || ref.MessageID == "" || ref.MessageID == "0" {
		return fmt.Errorf("invalid message receipt")
	}
	key := sharedMessageKey(project, platform, ref)
	if old := d.state.Links[key]; old != "" && old != session {
		return fmt.Errorf("conflicting message receipt")
	}
	next := d.state
	next.Links = make(map[string]string, len(d.state.Links)+1)
	for k, v := range d.state.Links {
		next.Links[k] = v
	}
	next.Links[key] = session
	next.InteractionLinks = make(map[string]bool, len(d.state.InteractionLinks)+1)
	for k, v := range d.state.InteractionLinks {
		next.InteractionLinks[k] = v
	}
	if interaction {
		next.InteractionLinks[key] = true
	}
	if err := d.save(next); err != nil {
		return fmt.Errorf("save message receipt: %w", err)
	}
	d.state = next
	return nil
}

func (d *sharedDirectory) resolveMessage(project, platform, scope string, ref MessageReference) (sharedSession, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loadErr != nil || ref.Scope != scope || ref.MessageID == "" || ref.MessageID == "0" {
		return sharedSession{}, false
	}
	if d.state.InteractionLinks[sharedMessageKey(project, platform, ref)] {
		return sharedSession{}, false
	}
	id := d.state.Links[sharedMessageKey(project, platform, ref)]
	for _, s := range d.state.Scopes[sharedScopeKey(project, platform, scope)].Sessions {
		if s.ID == id {
			return s, true
		}
	}
	return sharedSession{}, false
}

func (e *Engine) routeSharedReply(p Platform, msg *Message) {
	if e.sharedDirectory.isInteractionMessage(e.name, msg.Platform, msg.SharedScope, *msg.BotReply) {
		e.replySharedInteraction(p, msg)
		return
	}
	s, ok := e.sharedDirectory.resolveMessage(e.name, msg.Platform, msg.SharedScope, *msg.BotReply)
	if !ok {
		slog.Debug("shared message reference unavailable", "platform", msg.Platform)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedReplyUnavailable))
		return
	}
	e.acceptSharedRequest(p, msg, s)
}
