package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// InteractionResponse is an explicit action, never inferred from ordinary text.
// The token binds a request and one current Agent question generation.
type InteractionResponse struct {
	Token, Action, Answer string
	Question, Option      int
}
type ReceiptButtonSender interface {
	SendWithButtonsWithReceipt(context.Context, any, string, [][]ButtonOption, func(MessageReference) error) error
}

type sharedInteraction struct {
	token     string
	request   sharedRequest
	event     Event
	ctx       context.Context
	answers   map[int]string
	decisions chan PermissionResult // one accepted response; receiver owns lifecycle, never closed
}

func (q *sharedQueue) setWaiting(id string, waiting bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := indexOfSharedRequest(q.requests, id)
	if q.requests[i].Status != "running" {
		return fmt.Errorf("interaction request is no longer running")
	}
	next := append([]sharedRequest(nil), q.requests...)
	next[i].Waiting = waiting
	if err := q.save(next); err != nil {
		return err
	}
	q.requests = next
	return nil
}
func (e *Engine) beginSharedInteraction(ctx context.Context, r sharedRequest, event Event) (*sharedInteraction, error) {
	if event.RequestID == "" {
		return nil, fmt.Errorf("agent interaction lacks request identity")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	pending := &sharedInteraction{token: hex.EncodeToString(nonce[:]), request: r, event: event, ctx: ctx, answers: map[int]string{}, decisions: make(chan PermissionResult, 1)}
	if err := e.sharedQueue.setWaiting(r.ID, true); err != nil {
		return nil, err
	}
	q := e.sharedQueue
	q.mu.Lock()
	if q.interactions == nil {
		q.interactions = map[string]*sharedInteraction{}
	}
	q.interactions[pending.token] = pending
	q.mu.Unlock()
	if err := e.sendSharedInteraction(pending); err != nil {
		e.endSharedInteraction(pending)
		return nil, err
	}
	return pending, nil
}
func (e *Engine) endSharedInteraction(pending *sharedInteraction) {
	if pending == nil {
		return
	}
	e.sharedQueue.mu.Lock()
	delete(e.sharedQueue.interactions, pending.token)
	e.sharedQueue.mu.Unlock()
}

func (e *Engine) sendSharedInteraction(pending *sharedInteraction) error {
	r := pending.request
	p, target, err := e.sharedOutputTarget(r)
	if err != nil {
		return err
	}
	text := e.i18n.Tf(MsgInteractionPrompt, r.Session.Name, r.UserID, pending.event.ToolName)
	detail := pending.event.ToolInput
	if detail == "" && len(pending.event.ToolInputRaw) > 0 {
		data, err := json.Marshal(pending.event.ToolInputRaw)
		if err != nil {
			return err
		}
		detail = string(data)
	}
	limit := e.display.ToolMaxLen
	if limit > 0 {
		limit = limit * 8 / 5
	} else {
		limit = 4000
	}
	if detail != "" {
		text += "\n" + truncateIf(detail, limit)
	}
	var buttons [][]ButtonOption
	if len(pending.event.Questions) == 0 {
		text += "\n/approve " + pending.token + "\n/deny " + pending.token
		buttons = [][]ButtonOption{{{Text: e.i18n.T(MsgInteractionAllow), Data: "shared:" + pending.token + ":allow"}, {Text: e.i18n.T(MsgInteractionDeny), Data: "shared:" + pending.token + ":deny"}}}
	} else {
		for i, question := range pending.event.Questions {
			text += fmt.Sprintf("\n%d. %s", i+1, question.Question)
			for j, opt := range question.Options {
				text += fmt.Sprintf("\n  %d. %s", j+1, opt.Label)
				if !question.MultiSelect {
					buttons = append(buttons, []ButtonOption{{Text: fmt.Sprintf("%d.%d %s", i+1, j+1, opt.Label), Data: fmt.Sprintf("shared:%s:q%d:o%d", pending.token, i, j)}})
				}
			}
			text += "\n" + e.i18n.Tf(MsgInteractionAnswerUsage, pending.token, i+1)
		}
	}
	text += "\n" + e.i18n.T(MsgInteractionHint)
	record := func(ref MessageReference) error {
		return e.sharedDirectory.recordMessageKind(e.name, r.Platform, r.Scope, r.Session.ID, ref, true)
	}
	if sender, ok := p.Platform.(ReceiptButtonSender); ok {
		return sender.SendWithButtonsWithReceipt(pending.ctx, target, text, buttons, record)
	}
	if sender, ok := p.Platform.(ReceiptReplySender); ok {
		return sender.ReplyWithReceipt(pending.ctx, target, text, record)
	}
	return p.Platform.Reply(pending.ctx, target, text)
}

func (d *sharedDirectory) isInteractionMessage(project, platform, scope string, ref MessageReference) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadErr == nil && ref.Scope == scope && d.state.InteractionLinks[sharedMessageKey(project, platform, ref)]
}

func (e *Engine) handleSharedInteraction(p Platform, msg *Message, response InteractionResponse) {
	if auth, ok := p.(SharedControlAuthorizer); ok && !auth.AuthorizeSharedControl(msg.SharedScope, msg.UserID) {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgInteractionDenied))
		return
	}
	q := e.sharedQueue
	q.mu.Lock()
	hint := e.acceptSharedInteractionLocked(q, msg, response)
	q.mu.Unlock()
	e.reply(p, msg.ReplyCtx, e.i18n.T(hint))
}
func (e *Engine) acceptSharedInteractionLocked(q *sharedQueue, msg *Message, response InteractionResponse) MsgKey {
	pending := q.interactions[response.Token]
	if pending == nil || pending.ctx.Err() != nil {
		return MsgInteractionStale
	}
	r := pending.request
	if r.Platform != msg.Platform || r.Scope != msg.SharedScope {
		return MsgInteractionStale
	}
	current := q.requests[indexOfSharedRequest(q.requests, r.ID)]
	if current.Status != "running" || !current.Waiting {
		return MsgInteractionStale
	}
	if r.UserID != msg.UserID {
		return MsgInteractionDenied
	}
	result := PermissionResult{Behavior: "allow", UpdatedInput: pending.event.ToolInputRaw}
	if len(pending.event.Questions) == 0 {
		if response.Action != "allow" && response.Action != "deny" {
			return MsgInteractionStale
		}
		result.Behavior = response.Action
	} else {
		if response.Action != "answer" && response.Action != "option" {
			return MsgInteractionStale
		}
		i := response.Question
		if i < 0 || i >= len(pending.event.Questions) {
			return MsgInteractionStale
		}
		if _, answered := pending.answers[i]; answered {
			return MsgInteractionStale
		}
		question := pending.event.Questions[i]
		answer := response.Answer
		if response.Action == "option" {
			if question.MultiSelect || response.Option < 0 || response.Option >= len(question.Options) {
				return MsgInteractionStale
			}
			answer = question.Options[response.Option].Label
		} else {
			answer = e.resolveAskQuestionAnswer(question, answer)
		}
		if strings.TrimSpace(answer) == "" {
			return MsgInteractionHint
		}
		pending.answers[i] = answer
		if len(pending.answers) != len(pending.event.Questions) {
			return MsgInteractionAnswerSaved
		}
		result.UpdatedInput = buildAskQuestionResponse(pending.event.ToolInputRaw, pending.event.Questions, pending.answers)
	}
	delete(q.interactions, response.Token)
	pending.decisions <- result
	return MsgInteractionReceived
}

func (e *Engine) sharedInteractionCommand(p Platform, msg *Message, command string, args []string) {
	response := InteractionResponse{}
	if len(args) > 0 {
		response.Token = args[0]
	}
	switch command {
	case "approve", "deny":
		if len(args) != 1 {
			break
		}
		response.Action = "allow"
		if command == "deny" {
			response.Action = "deny"
		}
	case "answer":
		if len(args) < 3 {
			break
		}
		i, err := strconv.Atoi(args[1])
		if err != nil {
			break
		}
		response.Action = "answer"
		response.Question = i - 1
		response.Answer = strings.Join(args[2:], " ")
	}
	e.handleSharedInteraction(p, msg, response)
}

func (e *Engine) sharedInteractionAuthorized(r sharedRequest) bool {
	for _, p := range e.platforms {
		if p.Name() == r.Platform {
			if auth, ok := p.(SharedControlAuthorizer); ok {
				return auth.AuthorizeSharedControl(r.Scope, r.UserID)
			}
			return true
		}
	}
	slog.Warn("interaction platform unavailable", "request", r.ID)
	return false
}

// invalidateInteractionsLocked is called with q.mu held after a stop/revocation
// decision. This revokes entry tokens; process cancellation and settlement remain
// the control operation's responsibility.
func (q *sharedQueue) invalidateInteractionsLocked(requestID string) {
	for token, pending := range q.interactions {
		if pending.request.ID == requestID {
			delete(q.interactions, token)
		}
	}
}
