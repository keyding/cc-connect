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

func (p *Platform) SendWithButtonsWithReceipt(ctx context.Context, target any, text string, buttons [][]core.ButtonOption, record func(core.MessageReference) error) error {
	var rows [][]models.InlineKeyboardButton
	for _, row := range buttons {
		if len(row) == 0 {
			continue
		}
		var converted []models.InlineKeyboardButton
		for _, button := range row {
			converted = append(converted, models.InlineKeyboardButton{Text: button.Text, CallbackData: button.Data})
		}
		rows = append(rows, converted)
	}
	var markup *models.InlineKeyboardMarkup
	if len(rows) > 0 {
		markup = &models.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	return p.replyWithReceipt(ctx, target, text, markup, record)
}

// Invalid encodings remain explicit invalid interactions rather than becoming
// user text or legacy permission decisions. Payload never contains answer text.
func parseSharedInteraction(data string) *core.InteractionResponse {
	response := &core.InteractionResponse{}
	parts := strings.Split(data, ":")
	if len(parts) < 3 || parts[0] != "shared" || len(parts[1]) != 32 {
		return response
	}
	response.Token = parts[1]
	if len(parts) == 3 && (parts[2] == "allow" || parts[2] == "deny") {
		response.Action = parts[2]
		return response
	}
	if len(parts) != 4 || !strings.HasPrefix(parts[2], "q") || !strings.HasPrefix(parts[3], "o") {
		return response
	}
	q, err := strconv.Atoi(strings.TrimPrefix(parts[2], "q"))
	if err != nil {
		return response
	}
	o, err := strconv.Atoi(strings.TrimPrefix(parts[3], "o"))
	if err != nil {
		return response
	}
	response.Action = "option"
	response.Question = q
	response.Option = o
	return response
}

// Inaccessible callback messages lack a reliable Topic. Report invalidity in a
// localized callback alert rather than guessing a chat/thread reply destination.
type interactionCallbackReply struct{ ID string }

func (p *Platform) handleInaccessibleSharedCallback(cb *models.CallbackQuery) bool {
	if !strings.HasPrefix(cb.Data, "shared:") || cb.Message.Message != nil || cb.Message.InaccessibleMessage == nil {
		return false
	}
	inaccessible := cb.Message.InaccessibleMessage
	if handler := p.messageHandler(); handler != nil {
		handler(p, &core.Message{Platform: "telegram", SharedScope: strconv.FormatInt(inaccessible.Chat.ID, 10), UserID: strconv.FormatInt(cb.From.ID, 10), MessageID: cb.ID, ReplyCtx: interactionCallbackReply{ID: cb.ID}, Interaction: &core.InteractionResponse{}})
	}
	return true
}
func (p *Platform) replyInteractionCallback(ctx context.Context, target interactionCallbackReply, text string) error {
	bot, err := p.connectedBot("reply interaction callback")
	if err != nil {
		return err
	}
	runes := []rune(text)
	if len(runes) > 200 {
		text = string(runes[:200])
	}
	_, err = bot.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: target.ID, Text: text, ShowAlert: true})
	if err != nil {
		return fmt.Errorf("telegram: reply interaction callback: %w", err)
	}
	return nil
}

// UpdateInteractionMessage replaces an accepted question with its answer and
// explicitly clears the old keyboard. Text is plain to preserve user input.
func (p *Platform) UpdateInteractionMessage(ctx context.Context, ref core.MessageReference, text string) error {
	chat, err := strconv.ParseInt(ref.Scope, 10, 64)
	if err != nil || chat == 0 {
		return fmt.Errorf("telegram: invalid interaction chat reference")
	}
	message, err := strconv.Atoi(ref.MessageID)
	if err != nil || message <= 0 {
		return fmt.Errorf("telegram: invalid interaction message reference")
	}
	bot, err := p.connectedBot("update interaction")
	if err != nil {
		return err
	}
	if runes := []rune(text); len(runes) > telegramMaxMessageLen {
		text = string(runes[:telegramMaxMessageLen-1]) + "…"
	}
	_, err = bot.EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID: chat, MessageID: message, Text: text,
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		return fmt.Errorf("telegram: update answered question: %w", err)
	}
	return nil
}
