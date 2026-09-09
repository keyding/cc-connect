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
		var converted []models.InlineKeyboardButton
		for _, button := range row {
			converted = append(converted, models.InlineKeyboardButton{Text: button.Text, CallbackData: button.Data})
		}
		rows = append(rows, converted)
	}
	return p.replyWithReceipt(ctx, target, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows}, record)
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
