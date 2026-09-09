package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Receipt capabilities are optional; a platform never fabricates an identity
// for an output whose successful response is unavailable.
type ReceiptImageSender interface {
	SendImageWithReceipt(context.Context, any, ImageAttachment, func(MessageReference) error) error
}
type ReceiptFileSender interface {
	SendFileWithReceipt(context.Context, any, FileAttachment, func(MessageReference) error) error
}
type ReceiptPreviewSender interface {
	SendPreviewWithReceipt(context.Context, any, string, func(MessageReference) error) (any, error)
	UpdatePreviewWithReceipt(context.Context, any, string, func(MessageReference) error) error
}

// sharedOutputPlatform binds every side output to a request, independently of
// mutable selections. It deliberately does not expose preview deletion.
type sharedOutputPlatform struct {
	Platform
	record func(MessageReference) error
}

func (p *sharedOutputPlatform) Reply(ctx context.Context, target any, text string) error {
	if sender, ok := p.Platform.(ReceiptReplySender); ok {
		return sender.ReplyWithReceipt(ctx, target, text, p.record)
	}
	return p.Platform.Reply(ctx, target, text)
}
func (p *sharedOutputPlatform) Send(ctx context.Context, target any, text string) error {
	return p.Reply(ctx, target, text)
}
func (p *sharedOutputPlatform) SendImage(ctx context.Context, target any, img ImageAttachment) error {
	if sender, ok := p.Platform.(ReceiptImageSender); ok {
		return sender.SendImageWithReceipt(ctx, target, img, p.record)
	}
	return fmt.Errorf("shared image receipt: %w", ErrNotSupported)
}
func (p *sharedOutputPlatform) SendFile(ctx context.Context, target any, file FileAttachment) error {
	if sender, ok := p.Platform.(ReceiptFileSender); ok {
		return sender.SendFileWithReceipt(ctx, target, file, p.record)
	}
	return fmt.Errorf("shared file receipt: %w", ErrNotSupported)
}
func (p *sharedOutputPlatform) SendPreviewStart(ctx context.Context, target any, text string) (any, error) {
	if sender, ok := p.Platform.(ReceiptPreviewSender); ok {
		handle, err := sender.SendPreviewWithReceipt(ctx, target, text, p.record)
		if err != nil {
			slog.Warn("shared preview delivery failed", "error", err)
		}
		return handle, err
	}
	return nil, fmt.Errorf("shared preview receipt: %w", ErrNotSupported)
}
func (p *sharedOutputPlatform) UpdateMessage(ctx context.Context, handle any, text string) error {
	if sender, ok := p.Platform.(ReceiptPreviewSender); ok {
		err := sender.UpdatePreviewWithReceipt(ctx, handle, text, p.record)
		if err != nil {
			slog.Warn("shared preview edit failed", "error", err)
		}
		return err
	}
	return fmt.Errorf("shared preview receipt: %w", ErrNotSupported)
}

func (e *Engine) sharedOutputTarget(r sharedRequest) (*sharedOutputPlatform, any, error) {
	for _, p := range e.platforms {
		if p.Name() != r.Platform {
			continue
		}
		codec, ok := p.(DurableReplyContext)
		if !ok {
			break
		}
		target, err := codec.UnmarshalReplyContext(r.Reply)
		if err != nil {
			return nil, nil, err
		}
		return &sharedOutputPlatform{Platform: p, record: func(ref MessageReference) error {
			return e.sharedDirectory.recordMessage(e.name, r.Platform, r.Scope, r.Session.ID, ref)
		}}, target, nil
	}
	return nil, nil, fmt.Errorf("shared output destination unavailable")
}

func (e *Engine) resolveSharedOutput(sessionKey string) (Platform, any, error) {
	id := strings.TrimPrefix(sessionKey, "shared-request:")
	e.sharedQueue.mu.Lock()
	var request *sharedRequest
	for _, r := range e.sharedQueue.requests {
		if r.ID == id && (r.Status == "running" || r.Status == "waiting") {
			copy := r
			request = &copy
			break
		}
	}
	e.sharedQueue.mu.Unlock()
	if request == nil {
		return nil, nil, fmt.Errorf("shared output request is no longer active")
	}
	return e.sharedOutputTarget(*request)
}

// SessionEnvStarter atomically binds per-process output routing without mutating
// the agent's defaults shared by concurrent session creation.
type SessionEnvStarter interface {
	StartSessionWithEnv(context.Context, string, []string) (AgentSession, error)
}

func (e *Engine) startSharedAgent(ctx context.Context, r sharedRequest) (AgentSession, error) {
	if starter, ok := e.agent.(SessionEnvStarter); ok {
		env := []string{"CC_PROJECT=" + e.name, "CC_SESSION_KEY=shared-request:" + r.ID, "CC_DATA_DIR=" + e.dataDir}
		if exe, err := os.Executable(); err == nil {
			env = append(env, "PATH="+filepath.Dir(exe)+string(filepath.ListSeparator)+os.Getenv("PATH"))
		} else {
			slog.Warn("locate shared executor output helper", "error", err)
		}
		return starter.StartSessionWithEnv(ctx, r.HistoryID, env)
	}
	return e.agent.StartSession(ctx, r.HistoryID)
}

func (e *Engine) sharedPreview(r sharedRequest, ctx context.Context) *streamPreview {
	p, target, err := e.sharedOutputTarget(r)
	if err != nil {
		slog.Warn("shared preview destination unavailable", "error", err)
		return nil
	}
	if _, ok := p.Platform.(ReceiptPreviewSender); !ok {
		return nil
	}
	return newStreamPreview(e.streamPreview, p, target, ctx, func(text string) string { text, _ = stripTrailingSilent(text); return text })
}
