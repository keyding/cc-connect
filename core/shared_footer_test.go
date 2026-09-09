package core

import "context"

type sharedFooterPlatform struct{ linkTestPlatform }

func (*sharedFooterPlatform) CompactReplyFooter() bool { return true }

type sharedFooterAgent struct {
	queueTestAgent
	usage *ContextUsage
}

func (a *sharedFooterAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	s, err := a.queueTestAgent.StartSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &sharedFooterSession{queueTestSession: s.(*queueTestSession), usage: a.usage}, nil
}

type sharedFooterSession struct {
	*queueTestSession
	usage *ContextUsage
}

func (*sharedFooterSession) GetModel() string                 { return "reported-model" }
func (s *sharedFooterSession) GetContextUsage() *ContextUsage { return s.usage }
