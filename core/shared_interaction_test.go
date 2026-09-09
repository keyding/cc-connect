package core

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type interactionTestPlatform struct {
	linkTestPlatform
	authMu sync.Mutex
	denied map[string]bool
}

func (p *interactionTestPlatform) AuthorizeSharedControl(scope, user string) bool {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	return !p.denied[user]
}
func (p *interactionTestPlatform) deny(user string) {
	p.authMu.Lock()
	if p.denied == nil {
		p.denied = map[string]bool{}
	}
	p.denied[user] = true
	p.authMu.Unlock()
}
func (p *interactionTestPlatform) SendWithButtonsWithReceipt(ctx context.Context, target any, text string, _ [][]ButtonOption, record func(MessageReference) error) error {
	return p.ReplyWithReceipt(ctx, target, text, record)
}

type interactionDecision struct {
	id     string
	result PermissionResult
}
type interactionTestAgent struct {
	queueTestAgent
	decisions chan interactionDecision
}

func (a *interactionTestAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	s, err := a.queueTestAgent.StartSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &interactionTestSession{queueTestSession: s.(*queueTestSession), decisions: a.decisions}, nil
}

type interactionTestSession struct {
	*queueTestSession
	decisions chan interactionDecision
}

func (s *interactionTestSession) RespondPermission(id string, result PermissionResult) error {
	s.decisions <- interactionDecision{id, result}
	return nil
}
func interactionTokens(p interface{ getSent() []string }) []string {
	matches := regexp.MustCompile(`/(?:approve|answer) ([a-f0-9]{32})`).FindAllStringSubmatch(strings.Join(p.getSent(), "\n"), -1)
	var result []string
	for _, match := range matches {
		if len(result) == 0 || result[len(result)-1] != match[1] {
			result = append(result, match[1])
		}
	}
	return result
}
func waitInteraction(t *testing.T, p interface{ getSent() []string }, n int) string {
	t.Helper()
	waitQueue(t, func() bool { return len(interactionTokens(p)) >= n })
	return interactionTokens(p)[n-1]
}
func nextInteractionDecision(t *testing.T, a *interactionTestAgent) interactionDecision {
	t.Helper()
	select {
	case d := <-a.decisions:
		return d
	case <-time.After(3 * time.Second):
		t.Fatal("no Agent interaction response")
		return interactionDecision{}
	}
}
func noInteractionDecision(t *testing.T, a *interactionTestAgent) {
	t.Helper()
	select {
	case d := <-a.decisions:
		t.Fatalf("unexpected Agent decision %+v", d)
	case <-time.After(50 * time.Millisecond):
	}
}

func interactionResumeCommand(t *testing.T, p *interactionTestPlatform) string {
	t.Helper()
	matches := regexp.MustCompile(`/resume [^\s]+`).FindAllString(strings.Join(p.getSent(), "\n"), -1)
	if len(matches) == 0 {
		t.Fatal("missing explicit resume entry")
	}
	return matches[len(matches)-1]
}
