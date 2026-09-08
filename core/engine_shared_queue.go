package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

func (e *Engine) acceptSharedRequest(p Platform, msg *Message, s sharedSession) {
	codec, ok := p.(DurableReplyContext)
	if !ok || msg.MessageID == "" || msg.IsPermissionResponse {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedNotAccepted))
		return
	}
	reply, err := codec.MarshalReplyContext(msg.ReplyCtx)
	if err == nil {
		_, err = codec.UnmarshalReplyContext(reply)
	}
	dir, dirErr := e.sharedWorkDir()
	if err != nil || dirErr != nil {
		slog.Error("shared request target unavailable", "reply_error", err, "directory_error", dirErr)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedNotAccepted))
		return
	}
	r := sharedRequest{Session: s, Platform: p.Name(), Scope: msg.SharedScope, Entry: msg.SessionKey, UserID: msg.UserID, UserName: msg.UserName, MessageID: msg.MessageID, Content: strings.TrimSpace(msg.ExtraContent + "\n" + msg.Content), WorkDir: dir, Reply: reply, Images: msg.Images, Files: msg.Files}
	if msg.Audio != nil {
		format := filepath.Base(msg.Audio.Format)
		if format == "." || format == "" {
			format = "bin"
		}
		r.Files = append(append([]FileAttachment(nil), r.Files...), FileAttachment{MimeType: msg.Audio.MimeType, FileName: "audio." + format, Data: msg.Audio.Data})
	}
	ahead, paused, duplicate, err := e.sharedQueue.accept(r)
	if err != nil {
		slog.Error("persist shared request", "error", err)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedNotAccepted))
		return
	}
	if duplicate {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgSharedDuplicate))
		return
	}
	if msg.OnAccepted != nil {
		msg.OnAccepted()
	}
	text := e.i18n.Tf(MsgSharedQueued, s.Name, ahead)
	if paused {
		text += "\n" + e.i18n.T(MsgSharedPaused)
	}
	e.reply(p, msg.ReplyCtx, text)
	e.startSharedQueue()
}

func (e *Engine) sharedWorkDir() (string, error) {
	dir := e.baseWorkDir
	if provider, ok := e.agent.(interface{ GetWorkDir() string }); ok {
		dir = provider.GetWorkDir()
	}
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func (e *Engine) startSharedQueue() {
	q := e.sharedQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running || len(q.requests) == 0 || !e.beginMessageWork() {
		return
	}
	q.running = true
	go func() {
		defer e.messageWork.Done()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			if e.ctx.Err() != nil {
				return
			}
			for {
				index, r, ok, err := q.take()
				if err != nil {
					slog.Error("shared start checkpoint failed", "request", r.ID, "error", err)
					e.sharedReply(r, e.i18n.T(MsgSharedPaused))
				}
				if !ok {
					break
				}
				if !e.beginMessageWork() {
					return
				}
				go func() { defer e.messageWork.Done(); e.executeSharedRequest(index, r) }()
			}
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (e *Engine) sharedReply(r sharedRequest, text string) {
	for _, p := range e.platforms {
		if p.Name() != r.Platform {
			continue
		}
		codec, ok := p.(DurableReplyContext)
		if !ok {
			break
		}
		target, err := codec.UnmarshalReplyContext(r.Reply)
		if err == nil {
			if sender, ok := p.(ReceiptReplySender); ok {
				err = sender.ReplyWithReceipt(e.ctx, target, text, func(ref MessageReference) error {
					return e.sharedDirectory.recordMessage(e.name, r.Platform, r.Scope, r.Session.ID, ref)
				})
			} else {
				err = p.Reply(e.ctx, target, text)
			}
		}
		if err != nil {
			slog.Error("shared result delivery failed", "request", r.ID, "error", err)
		}
		return
	}
	slog.Error("shared reply platform unavailable", "request", r.ID)
}

func (e *Engine) executeSharedRequest(index int, r sharedRequest) {
	result, history, err := e.runSharedAgent(r)
	result, _ = stripTrailingSilent(result)
	success := err == nil
	if saveErr := e.sharedQueue.finish(index, history, result, success); saveErr != nil {
		slog.Error("shared completion not durable", "request", r.ID, "error", saveErr)
		success = false
	}
	if !success {
		slog.Warn("shared queue paused", "request", r.ID, "error", err)
		e.sharedReply(r, e.i18n.T(MsgSharedPaused))
		return
	}
	if result != "" {
		e.sharedReply(r, result)
	}
}

func (e *Engine) runSharedAgent(r sharedRequest) (result, history string, err error) {
	ctx := e.ctx
	if e.maxTurnTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.maxTurnTime)
		defer cancel()
	}
	dir, err := e.sharedWorkDir()
	if err != nil {
		return "", "", err
	}
	if r.Session.AgentType != e.agent.Name() || dir != r.WorkDir {
		return "", "", fmt.Errorf("shared agent or directory changed")
	}
	as, err := e.startSharedAgent(ctx, r)
	if err != nil {
		return "", "", err
	}
	defer func() { history = as.CurrentSessionID(); err = errors.Join(err, settleSharedAgent(as)) }()

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- as.Send(e.buildSenderPrompt(r.Content, r.UserID, r.UserName, r.Platform, r.Entry, ""), r.ID, r.Images, r.Files)
	}()
	var idle *time.Timer
	var idleCh <-chan time.Time
	if e.eventIdleTimeout > 0 {
		idle = time.NewTimer(e.eventIdleTimeout)
		idleCh = idle.C
		defer idle.Stop()
	}
	preview := e.sharedPreview(r, ctx)
	if preview != nil {
		defer preview.discard()
	}
	var texts strings.Builder
	previewed := 0
	for {
		select {
		case <-ctx.Done():
			return texts.String(), "", ctx.Err()
		case sendErr := <-sendDone:
			sendDone = nil
			if sendErr != nil {
				return texts.String(), "", sendErr
			}
		case <-idleCh:
			return texts.String(), "", fmt.Errorf("agent event idle timeout")
		case event, ok := <-as.Events():
			if idle != nil {
				idle.Reset(e.eventIdleTimeout)
			}
			if !ok {
				return texts.String(), "", fmt.Errorf("agent ended without reliable result")
			}
			switch event.Type {
			case EventText:
				texts.WriteString(event.Content)
				if preview != nil && !couldBeSilentPrefix(texts.String()) {
					preview.appendText(texts.String()[previewed:])
					previewed = texts.Len()
				}
			case EventToolUse:
				e.sharedReply(r, e.i18n.Tf(MsgSharedProgress, event.ToolName))
			case EventError:
				return texts.String(), "", fmt.Errorf("agent execution failed: %v", event.Error)
			case EventPermissionRequest:
				if idle != nil {
					idle.Stop()
					idleCh = nil
				}
				e.sharedReply(r, e.i18n.T(MsgSharedWaiting))
			case EventResult:
				if event.Error != nil {
					return texts.String(), "", event.Error
				}
				if !event.Done {
					continue
				}
				if sendDone != nil {
					select {
					case sendErr := <-sendDone:
						if sendErr != nil {
							return texts.String(), "", sendErr
						}
						sendDone = nil
					case <-ctx.Done():
						return texts.String(), "", ctx.Err()
					}
				}
				if event.Content != "" {
					return event.Content, "", nil
				}
				return texts.String(), "", nil
			}
		}
	}
}

// Settlement must preserve all failure causes and prove OS teardown before unlock.
func settleSharedAgent(as AgentSession) error {
	var errs []error
	if err := as.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close shared executor: %w", err))
	}
	if waiter, ok := as.(AgentSessionSettler); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := waiter.WaitForExit(ctx); err != nil {
			errs = append(errs, fmt.Errorf("settle shared executor: %w", err))
		}
	} else {
		errs = append(errs, fmt.Errorf("agent does not confirm executor exit"))
	}
	if as.CurrentSessionID() == "" {
		errs = append(errs, fmt.Errorf("agent did not supply history identity"))
	}
	return errors.Join(errs...)
}
