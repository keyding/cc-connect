package core

import "testing"

func TestModelContextFooter(t *testing.T) {
	e := &Engine{i18n: NewI18n(LangChinese)}
	cases := []struct {
		name  string
		usage *ContextUsage
		want  string
	}{
		{"reported", &ContextUsage{UsedTokens: 32000, ContextWindow: 200000, OutputTokens: 800, InputTokens: 10}, "deepseek-v4-pro · 上下文 32.0k / 200.0k（16%）"},
		{"cache", &ContextUsage{InputTokens: 1000, CachedInputTokens: 30000, CacheCreationInputTokens: 1000, ContextWindow: 200000}, "deepseek-v4-pro · 上下文 32.0k / 200.0k（16%）"},
		{"estimated", &ContextUsage{UsedTokens: 32000, ContextWindow: 200000, ContextWindowEstimated: true}, "deepseek-v4-pro"},
		{"unknown", nil, "deepseek-v4-pro"},
		{"no capacity", &ContextUsage{UsedTokens: 32000}, "deepseek-v4-pro"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := e.modelContextFooter("deepseek-v4-pro", c.usage); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
