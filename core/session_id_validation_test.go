package core

import (
	"context"
	"errors"
	"testing"
)

// validatingAgent wraps a controllableAgent and adds an opt-in
// SessionIDValidator so we can pin the engine's behavior for issue #599:
// when the stored session ID is rejected by the agent, the engine must
// start a fresh session instead of resuming the wrong one.
type validatingAgent struct {
	controllableAgent
	validateFunc func(ctx context.Context, sessionID string) bool
}

func (a *validatingAgent) ValidateSessionID(ctx context.Context, sessionID string) bool {
	if a.validateFunc == nil {
		return true // default: trust whatever ID the Session has
	}
	return a.validateFunc(ctx, sessionID)
}

var _ SessionIDValidator = (*validatingAgent)(nil)

// TestIssue599_InvalidSessionIDClearedBeforeResume pins the regression for
// cross-project session leakage: when the agent rejects the stored
// session ID, the engine must clear the ID and call StartSession with
// "" (fresh start) rather than passing the bad ID through.
func TestIssue599_InvalidSessionIDClearedBeforeResume(t *testing.T) {
	var startedWith string
	sess := newControllableSession("fresh-id")
	agent := &validatingAgent{
		controllableAgent: controllableAgent{nextSession: sess},
		validateFunc: func(_ context.Context, sessionID string) bool {
			// Reject whatever ID the Session carries — simulate a
			// cross-project leak.
			return false
		},
	}
	agent.startSessionFn = func(_ context.Context, sessionID string) (AgentSession, error) {
		startedWith = sessionID
		return sess, nil
	}

	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	key := "test:user1"

	// Simulate a stored cross-project session ID.
	s := &Session{AgentSessionID: "leaked-id-from-other-project"}

	e.getOrCreateInteractiveStateWith(key, p, "ctx", s, e.sessions, nil, "")

	if startedWith != "" {
		t.Errorf("StartSession called with %q, want \"\" (fresh start; leaked id must NOT be passed through)", startedWith)
	}
}

// TestIssue599_ValidSessionIDPreserved is the negative case: when the
// agent says the ID is valid, the engine must pass it through to
// StartSession so the resume actually resumes.
func TestIssue599_ValidSessionIDPreserved(t *testing.T) {
	var startedWith string
	sess := newControllableSession("resumed-id")
	agent := &validatingAgent{
		controllableAgent: controllableAgent{nextSession: sess},
		validateFunc: func(_ context.Context, _ string) bool {
			return true
		},
	}
	agent.startSessionFn = func(_ context.Context, sessionID string) (AgentSession, error) {
		startedWith = sessionID
		return sess, nil
	}

	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	key := "test:user1"

	s := &Session{AgentSessionID: "valid-id-abc"}

	e.getOrCreateInteractiveStateWith(key, p, "ctx", s, e.sessions, nil, "")

	if startedWith != "valid-id-abc" {
		t.Errorf("StartSession called with %q, want %q (resume path)", startedWith, "valid-id-abc")
	}
}

// TestIssue599_AgentWithoutValidatorNotBlocked ensures the validation
// gate is opt-in: agents that do not implement SessionIDValidator
// continue to work as before (the existing assumption is that engine
// callers only persist valid IDs).
func TestIssue599_AgentWithoutValidatorNotBlocked(t *testing.T) {
	var startedWith string
	sess := newControllableSession("resumed-id")
	agent := &controllableAgent{nextSession: sess}
	agent.startSessionFn = func(_ context.Context, sessionID string) (AgentSession, error) {
		startedWith = sessionID
		return sess, nil
	}

	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	key := "test:user1"

	s := &Session{AgentSessionID: "any-id"}

	e.getOrCreateInteractiveStateWith(key, p, "ctx", s, e.sessions, nil, "")

	if startedWith != "any-id" {
		t.Errorf("StartSession called with %q, want %q (no validator = pass through)", startedWith, "any-id")
	}
}

type unavailableValidationAgent struct{ controllableAgent }

func (a *unavailableValidationAgent) CheckSessionID(context.Context, string) (bool, error) {
	return false, errors.New("reader unavailable")
}
func TestSessionValidationFailurePreservesHistoryBinding(t *testing.T) {
	agent := &unavailableValidationAgent{}
	agent.startSessionFn = func(context.Context, string) (AgentSession, error) {
		t.Fatal("must not start while session validation is unavailable")
		return nil, nil
	}
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	s := e.sessions.GetOrCreateActive("test:user")
	s.SetAgentSessionID("saved-history", "test")
	e.getOrCreateInteractiveStateWith("test:user", p, "ctx", s, e.sessions, nil, "")
	if got := s.GetAgentSessionID(); got != "saved-history" {
		t.Fatalf("binding lost: %q", got)
	}
}
