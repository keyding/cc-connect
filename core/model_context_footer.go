package core

import (
	"math"
	"strings"
)

// modelContextFooter shows only reported context usage and the model. Unknown
// capacity is not estimated from a model name or cumulative billing tokens.
func (e *Engine) modelContextFooter(model string, usage *ContextUsage) string {
	parts := []string{}
	if model = strings.TrimSpace(model); model != "" {
		parts = append(parts, model)
	}
	if usage != nil && usage.ContextWindow > 0 && !usage.ContextWindowEstimated {
		used := usage.UsedTokens
		if used <= 0 {
			used = usage.InputTokens + usage.CachedInputTokens + usage.CacheCreationInputTokens
		}
		if used < 0 {
			used = 0
		}
		pct := math.Round(float64(used) * 100 / float64(usage.ContextWindow))
		parts = append(parts, e.i18n.Tf(MsgModelContextUsage, formatStatusTokenCount(used), formatStatusTokenCount(usage.ContextWindow), pct))
	}
	return strings.Join(parts, " · ")
}

// Keep the final footer in the durable result so receipt-based delivery uses
// the same reported model and context snapshot as the completed Agent turn.
func (e *Engine) sharedResultWithFooter(r sharedRequest, session AgentSession, result string) string {
	result, _ = stripTrailingSilent(result)
	if result == "" || !e.replyFooterEnabled || !e.showContextIndicator {
		return result
	}
	for _, p := range e.platforms {
		if p.Name() != r.Platform {
			continue
		}
		if compact, ok := p.(CompactReplyFooter); ok && compact.CompactReplyFooter() {
			return appendReplyFooter(result, e.modelContextFooter(replyFooterModel(session, e.agent), replyFooterSessionContextUsage(session)))
		}
		break
	}
	return result
}
