package core

import "strings"

type mutableSharedPlatform struct {
	interactionTestPlatform
	allow string
}

func (p *mutableSharedPlatform) SetAllowFrom(value string) {
	p.authMu.Lock()
	p.allow = value
	p.authMu.Unlock()
}
func (p *mutableSharedPlatform) AuthorizeSharedControl(scope, user string) bool {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	return AllowList(p.allow, user)
}
func sharedControlFromOutput(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
