package telegram

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chenhg5/cc-connect/core"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func recordTelegramMessage(rc replyContext, sent *models.Message, record func(core.MessageReference) error) error {
	if sent == nil || sent.ID <= 0 || sent.Chat.ID != rc.chatID || sent.MessageThreadID != rc.threadID {
		return fmt.Errorf("telegram: invalid message receipt")
	}
	return record(core.MessageReference{Scope: strconv.FormatInt(sent.Chat.ID, 10), MessageID: strconv.Itoa(sent.ID)})
}
func (p *Platform) SendImageWithReceipt(ctx context.Context, target any, img core.ImageAttachment, record func(core.MessageReference) error) error {
	rc, ok := target.(replyContext)
	if !ok {
		return fmt.Errorf("telegram: invalid image reply context %T", target)
	}
	bot, err := p.connectedBot("send image with receipt")
	if err != nil {
		return err
	}
	name := img.FileName
	if name == "" {
		name = "image"
	}
	params := &tgbot.SendPhotoParams{ChatID: rc.chatID, MessageThreadID: rc.threadID, Photo: &models.InputFileUpload{Filename: name, Data: bytes.NewReader(img.Data)}}
	if rc.messageID > 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: rc.messageID, AllowSendingWithoutReply: true}
	}
	sent, err := bot.SendPhoto(ctx, params)
	if err != nil {
		return fmt.Errorf("telegram: send image with receipt: %w", err)
	}
	return recordTelegramMessage(rc, sent, record)
}
func (p *Platform) SendFileWithReceipt(ctx context.Context, target any, file core.FileAttachment, record func(core.MessageReference) error) error {
	rc, ok := target.(replyContext)
	if !ok {
		return fmt.Errorf("telegram: invalid file reply context %T", target)
	}
	bot, err := p.connectedBot("send file with receipt")
	if err != nil {
		return err
	}
	name := file.FileName
	if name == "" {
		name = "attachment"
	}
	params := &tgbot.SendDocumentParams{ChatID: rc.chatID, MessageThreadID: rc.threadID, Document: &models.InputFileUpload{Filename: name, Data: bytes.NewReader(file.Data)}}
	if rc.messageID > 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: rc.messageID, AllowSendingWithoutReply: true}
	}
	sent, err := bot.SendDocument(ctx, params)
	if err != nil {
		return fmt.Errorf("telegram: send file with receipt: %w", err)
	}
	return recordTelegramMessage(rc, sent, record)
}
func (p *Platform) SendPreviewWithReceipt(ctx context.Context, target any, text string, record func(core.MessageReference) error) (any, error) {
	rc, ok := target.(replyContext)
	if !ok {
		return nil, fmt.Errorf("telegram: invalid preview reply context %T", target)
	}
	if len(core.SplitMessageCodeFenceAware(core.MarkdownToSimpleHTML(text), telegramMaxMessageLen)) != 1 {
		return nil, fmt.Errorf("telegram: preview exceeds one message")
	}
	var id int
	err := p.ReplyWithReceipt(ctx, target, text, func(ref core.MessageReference) error {
		n, err := strconv.Atoi(ref.MessageID)
		if err != nil {
			return err
		}
		id = n
		return record(ref)
	})
	if err != nil {
		return nil, err
	}
	return &telegramPreviewHandle{chatID: rc.chatID, threadID: rc.threadID, messageID: id}, nil
}
func (p *Platform) UpdatePreviewWithReceipt(ctx context.Context, handle any, text string, record func(core.MessageReference) error) error {
	h, ok := handle.(*telegramPreviewHandle)
	if !ok || h == nil {
		return fmt.Errorf("telegram: invalid receipt preview handle %T", handle)
	}
	bot, err := p.connectedBot("edit preview with receipt")
	if err != nil {
		return err
	}
	params := &tgbot.EditMessageTextParams{ChatID: h.chatID, MessageID: h.messageID, Text: core.MarkdownToSimpleHTML(text), ParseMode: models.ParseModeHTML}
	sent, err := bot.EditMessageText(ctx, params)
	if err != nil && strings.Contains(err.Error(), "can't parse") {
		params.Text = text
		params.ParseMode = ""
		sent, err = bot.EditMessageText(ctx, params)
	}
	if err != nil {
		if strings.Contains(err.Error(), "not modified") {
			return nil
		}
		return fmt.Errorf("telegram: edit preview with receipt: %w", err)
	}
	if sent == nil || sent.ID != h.messageID {
		return fmt.Errorf("telegram: edit changed message identity")
	}
	return recordTelegramMessage(replyContext{chatID: h.chatID, threadID: h.threadID}, sent, record)
}
