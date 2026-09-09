package core

import (
	"fmt"
	"strings"
)

// SharedAuthorizationUpdater applies the platform's real inbound allowlist.
// Unsupported adapters still require restart; core never guesses their policy.
type SharedAuthorizationUpdater interface{ SetAllowFrom(string) }

func sharedUserAllowed(p Platform, scope, user string) bool {
	if auth, ok := p.(SharedControlAuthorizer); ok {
		return auth.AuthorizeSharedControl(scope, user)
	}
	return true
}

// SetPlatformAllowFrom applies supported platform policies and reconciles durable
// requests before another shared operation/dispatch can pass the same gate.
func (e *Engine) SetPlatformAllowFrom(updates map[string]string) (restartRequired bool, err error) {
	e.sharedMutationMu.Lock()
	defer e.sharedMutationMu.Unlock()
	for name, value := range updates {
		applied := false
		for _, p := range e.platforms {
			if !strings.EqualFold(strings.TrimSpace(name), p.Name()) {
				continue
			}
			if updater, ok := p.(SharedAuthorizationUpdater); ok {
				updater.SetAllowFrom(strings.TrimSpace(value))
				applied = true
			}
		}
		if !applied {
			restartRequired = true
		}
	}
	return restartRequired, e.reconcileSharedAuthorizationLocked()
}

func (e *Engine) requestUserAllowed(r sharedRequest) bool {
	for _, p := range e.platforms {
		if p.Name() == r.Platform {
			return sharedUserAllowed(p, r.Scope, r.UserID)
		}
	}
	return false
}

// Caller owns sharedMutationMu; q.mu serializes with executor/interaction events.
func (e *Engine) reconcileSharedAuthorizationLocked() error {
	q := e.sharedQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	var next []sharedRequest
	var revoked []string
	for i, r := range q.requests {
		if (r.Status != "queued" && r.Status != "running") || e.requestUserAllowed(r) {
			continue
		}
		if next == nil {
			next = append([]sharedRequest(nil), q.requests...)
		}
		revoked = append(revoked, r.ID)
		q.invalidateInteractionsLocked(r.ID)
		next[i].Waiting = false
		if r.Status == "queued" {
			next[i].Status = "cancelled"
		} else {
			next[i].Status = "stopping"
		}
	}
	if len(revoked) == 0 {
		return nil
	}
	err := q.save(next)
	// Even failed persistence cannot leave revoked interactions/executors active.
	// Dispatch stays fenced; restart rechecks the current policy before execution.
	if err != nil {
		q.paused = true
		for i, r := range next {
			if r.Status == "stopping" {
				q.requests[i].Status = "stopping"
				q.requests[i].Waiting = false
			}
		}
	} else {
		q.requests = next
	}
	for _, id := range revoked {
		if stop := q.cancels[id]; stop != nil {
			stop()
		}
	}
	if err != nil {
		return fmt.Errorf("persist shared revocation: %w", err)
	}
	return nil
}

func (e *Engine) takeAuthorizedSharedRequest() (int, sharedRequest, bool, error) {
	e.sharedMutationMu.Lock()
	defer e.sharedMutationMu.Unlock()
	if err := e.reconcileSharedAuthorizationLocked(); err != nil {
		return 0, sharedRequest{}, false, err
	}
	return e.sharedQueue.take()
}
