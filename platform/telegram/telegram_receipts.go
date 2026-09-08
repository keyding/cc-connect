package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chenhg5/cc-connect/core"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ReplyWithReceipt keeps the original chat/topic even when the quoted message
// disappeared. Never retry on an uncertain network result or receipt failure.
func (p *Platform) ReplyWithReceipt(ctx context.Context, target any, text string, record func(core.MessageReference) error) error {
	rc, ok := target.(replyContext)
	if !ok {
		return fmt.Errorf("telegram: invalid receipt reply context %T", target)
	}
	bot, err := p.connectedBot("reply with receipt")
	if err != nil {
		return err
	}
	chunks := core.SplitMessageCodeFenceAware(core.MarkdownToSimpleHTML(text), telegramMaxMessageLen)
	for i, chunk := range chunks {
		params := &tgbot.SendMessageParams{ChatID: rc.chatID, MessageThreadID: rc.threadID, Text: chunk, ParseMode: models.ParseModeHTML}
		if i == 0 && rc.messageID != 0 {
			params.ReplyParameters = &models.ReplyParameters{MessageID: rc.messageID, AllowSendingWithoutReply: true}
		}
		sent, sendErr := bot.SendMessage(ctx, params)
		if sendErr != nil && strings.Contains(sendErr.Error(), "can't parse") {
			params.ParseMode = ""
			sent, sendErr = bot.SendMessage(ctx, params)
		}
		if sendErr != nil {
			return fmt.Errorf("telegram: reply chunk %d: %w", i, sendErr)
		}
		if err := recordTelegramMessage(rc, sent, record); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) replyReference(msg *models.Message) *core.MessageReference {
	p.mu.RLock()
	self := p.selfUser
	p.mu.RUnlock()
	if self == nil || msg.ForwardOrigin != nil {
		return nil
	}
	if external := msg.ExternalReply; external != nil {
		origin := external.Origin.MessageOriginUser
		// Hidden/anonymous origins cannot prove this bot authored the message.
		if origin != nil && origin.SenderUser.ID != self.ID {
			return nil
		}
		ref := &core.MessageReference{}
		if origin == nil {
			return ref
		}
		if external.Chat != nil {
			ref.Scope = strconv.FormatInt(external.Chat.ID, 10)
		}
		if external.MessageID > 0 {
			ref.MessageID = strconv.Itoa(external.MessageID)
		}
		return ref
	}
	reply := msg.ReplyToMessage
	if reply == nil || reply.From == nil || reply.From.ID != self.ID || reply.ForwardOrigin != nil {
		return nil
	}
	return &core.MessageReference{Scope: strconv.FormatInt(reply.Chat.ID, 10), MessageID: strconv.Itoa(reply.ID)}
}
