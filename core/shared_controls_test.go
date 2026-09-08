package core

import (
	"errors"
	"testing"
)

type exitedWithoutHistorySession struct{ *queueTestSession }

func (s *exitedWithoutHistorySession) Close() error             { return errors.New("close failed") }
func (s *exitedWithoutHistorySession) CurrentSessionID() string { return "" }

func TestSharedSettlement_ExitProofSurvivesResultAndCloseErrors(t *testing.T) {
	s := &exitedWithoutHistorySession{&queueTestSession{cujAgentSession: newCUJAgentSession()}}
	exited, err := settleSharedAgent(s)
	if !exited || err == nil {
		t.Fatalf("exit proof=%v error=%v", exited, err)
	}
}
